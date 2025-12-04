package jsonrpc

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/ledgerwatch/erigon-lib/common"
	libcommon "github.com/ledgerwatch/erigon-lib/common"
	"github.com/ledgerwatch/erigon-lib/common/hexutility"
	"github.com/ledgerwatch/erigon-lib/metrics"
	"github.com/ledgerwatch/erigon/consensus"
	"github.com/ledgerwatch/erigon/core"
	"github.com/ledgerwatch/erigon/core/state"
	"github.com/ledgerwatch/erigon/core/tracing"
	"github.com/ledgerwatch/erigon/core/types"
	"github.com/ledgerwatch/erigon/core/vm"
	"github.com/ledgerwatch/erigon/core/vm/evmtypes"
	"github.com/ledgerwatch/erigon/crypto"
	dtracer "github.com/ledgerwatch/erigon/debank/tracer"
	dtypes "github.com/ledgerwatch/erigon/debank/types"
	"github.com/ledgerwatch/erigon/eth/stagedsync"
	"github.com/ledgerwatch/erigon/rlp"
	"github.com/ledgerwatch/erigon/rpc"
	"github.com/ledgerwatch/erigon/turbo/rpchelper"
	"github.com/ledgerwatch/erigon/turbo/transactions"
	"github.com/ledgerwatch/erigon/zk/hermez_db"
	"github.com/ledgerwatch/log/v3"
)

var (
	LatestBlockNumber = metrics.GetOrCreateGauge("pipeline_block_num")

	ChainHeadNumber = metrics.GetOrCreateGauge("chain_head_block")

	LatestBlockTime = metrics.GetOrCreateGauge("pipeline_block_time")

	NodeInfo = metrics.GetOrCreateGauge(`pipeline_node_info{role="writer"}`)

	BlockProcessTimer = metrics.GetOrCreateSummary("chain_inserts")
)

func (api *TraceAPIImpl) DebankBlockRaw2(ctx context.Context, blockNrOrHash rpc.BlockNumberOrHash) (*dtypes.DebankOutPut, error) {
	logger := log.New("trace_debankBlock")

	dbtx, err := api.kv.BeginRo(ctx)
	if err != nil {
		return nil, err
	}
	defer dbtx.Rollback()

	chainConfig, err := api.chainConfig(ctx, dbtx)
	if err != nil {
		return nil, err
	}

	blockNumber, blockHash, _, err := rpchelper.GetBlockNumber_zkevm(blockNrOrHash, dbtx, api.filters)
	if err != nil {
		return nil, err
	}

	// Extract transactions from block
	block, bErr := api.blockWithSenders(ctx, dbtx, blockHash, blockNumber)
	if bErr != nil {
		return nil, bErr
	}

	if block == nil {
		return nil, fmt.Errorf("could not find block  %d", blockNumber)
	}

	header := block.Header()

	if block.NumberU64() == 0 {
		genesis := core.GenesisBlockByChainName(chainConfig.ChainName)
		return dtracer.OnGenesisBlock(block, genesis.Alloc)
	}

	LatestBlockNumber.SetUint64(block.NumberU64())
	ChainHeadNumber.SetUint64(block.NumberU64())
	LatestBlockTime.SetUint64(block.Time())

	engine := api.engine()
	txEnv, err := transactions.ComputeTxEnv_ZkEvm(ctx, engine, block, chainConfig, api._blockReader, dbtx, 0, api.historyV3(dbtx))
	if err != nil {
		return nil, err
	}
	blockCtx := txEnv.BlockContext
	ibs := txEnv.Ibs

	rules := chainConfig.Rules(block.NumberU64(), block.Time())

	parentHash := block.ParentHash()
	parentHeader, err := api._blockReader.Header(ctx, dbtx, parentHash, block.NumberU64()-1)
	if err != nil {
		return nil, err
	}

	writer := dtracer.NewBlockStorageDiff()
	usedGas := new(uint64)
	usedBlobGas := new(uint64)
	//gp := new(core.GasPool).AddGas(header.GasLimit).AddBlobGas(chainConfig.GetMaxBlobGasPerBlock())

	engine, ok := api.engine().(consensus.Engine)
	if !ok {
		return nil, errors.New("engine is not consensus.Engine")
	}

	includedTxs := make(types.Transactions, 0, block.Transactions().Len())
	receipts := make(types.Receipts, 0, block.Transactions().Len())
	vmConfig := vm.Config{}

	blockFile := &dtypes.BlockFile{
		Block:            dtracer.BuildPipelineBlock(block),
		Events:           make([]dtypes.Event, 0),
		Txs:              make([]dtypes.Transaction, 0),
		Traces:           make([]dtypes.Trace, 0),
		ErrorEvents:      make([]dtypes.Event, 0),
		ErrorTraces:      make([]dtypes.Trace, 0),
		StorageContracts: make([]string, 0),
	}
	stateHeader := dtracer.BuildPilelineBlockHeader(block)

	cumulativeGas := uint64(0)
	hermezReader := hermez_db.NewHermezDbReader(dbtx)
	// Collect ChangeContracts from all tracers to deduplicate them
	changeContractsMap := make(map[common.Address]struct{})
	for i, txn := range block.Transactions() {
		ibs.SetTxContext(txn.Hash(), block.Hash(), i)
		tracer := dtracer.NewCallTracer(blockFile, txn.Hash().Hex())
		vmConfig.Debug = true
		vmConfig.Tracer = tracer
		ibs.SetHooks(&tracing.Hooks{
			OnLog: tracer.OnLog,
		})

		effectiveGasPricePercentage, err := api._blockReader.TxnEffectiveGasPricePercentage(ctx, dbtx, txn.Hash())
		if err != nil {
			return nil, err
		}
		// Log details of the transaction for debugging
		txType := txn.Type()
		txPrice := txn.GetPrice()
		txTip := txn.GetTip()
		txFeeCap := txn.GetFeeCap()
		txGas := txn.GetGas()
		txBlobGas := txn.GetBlobGas()
		txChainID := txn.GetChainID()

		logger.Info("txn details",
			"hash", txn.Hash(),
			"type", txType,
			"price", txPrice,
			"tip", txTip,
			"feeCap", txFeeCap,
			"gas", txGas,
			"blobGas", txBlobGas,
			"chainID", txChainID,
			"gasPrice", txPrice,
			"to", txn.GetTo(),
			"nonce", txn.GetNonce(),
			"effectiveGasPricePercentage", effectiveGasPricePercentage,
		)

		txHash := txn.Hash()
		evm, effectiveGasPricePercentage, err := core.PrepareForTxExecution(chainConfig, &vm.Config{}, &blockCtx, hermezReader, ibs, block, &txHash, i)
		if err != nil {
			return nil, err
		}
		msg, _, err := core.GetTxContext(chainConfig, engine, ibs, block.Header(), txn, evm, effectiveGasPricePercentage)
		if err != nil {
			return nil, err
		}
		txCtx := evmtypes.TxContext{
			TxHash:            txn.Hash(),
			Origin:            msg.From(),
			GasPrice:          msg.GasPrice(),
			Txn:               txn,
			CumulativeGasUsed: &cumulativeGas,
			BlockNum:          block.NumberU64(),
		}

		var executionCounters *vm.CounterCollector
		zkConfig := vm.NewZkConfig(vm.Config{Debug: true, Tracer: tracer}, executionCounters)
		vmenv := vm.NewZkEVM(blockCtx, txCtx, ibs, chainConfig, zkConfig)

		result, err := core.ApplyMessage(vmenv, msg, new(core.GasPool).AddGas(msg.Gas()), true, false /* gasBailout */)
		if err != nil {
			return nil, err
		}
		err = ibs.FinalizeTx(rules, writer)
		if err != nil {
			return nil, err
		}

		*usedGas += result.UsedGas
		*usedBlobGas += txn.GetBlobGas()

		// Set the receipt logs and create the bloom filter.
		// based on the eip phase, we're passing whether the root touch-delete accounts.
		// by the tx.
		receipt := &types.Receipt{Type: txn.Type(), CumulativeGasUsed: *usedGas}
		if result.Failed() {
			receipt.Status = types.ReceiptStatusFailed
		} else {
			receipt.Status = types.ReceiptStatusSuccessful
		}
		receipt.TxHash = txn.Hash()
		receipt.GasUsed = result.UsedGas
		// if the transaction created a contract, store the creation address in the receipt.
		if msg.To() == nil {
			receipt.ContractAddress = crypto.CreateAddress(evm.Origin, txn.GetNonce())
		}
		// Set the receipt logs and create a bloom for filtering
		receipt.Logs = ibs.GetLogs(txn.Hash())

		// [zkevm] - ignore the bloom at this point due to a bug in zknode where the bloom is not included
		// in the block during execution
		//receipt.Bloom = types.CreateBloom(types.Receipts{receipt})

		receipt.BlockNumber = header.Number
		receipt.TransactionIndex = uint(ibs.TxIndex())

		log.Info("[DebankBlockRaw] Receipt created",
			"txHash", txn.Hash().Hex(),
			"status", receipt.Status,
			"gasUsed", receipt.GasUsed,
			"cumulativeGasUsed", receipt.CumulativeGasUsed,
			"totalGasUsed", *usedGas,
			"contractAddress", func() string {
				if receipt.ContractAddress != (libcommon.Address{}) {
					return receipt.ContractAddress.Hex()
				}
				return "none"
			}(),
			"logs", len(receipt.Logs))

		balance := ibs.GetBalance(libcommon.HexToAddress("0x687BEdBC8176e5E0A2d3A625ecf37a52f860968D"))
		fmt.Printf("[DebankBlockRaw] Balance in DebankBlockRaw, from: %v, balance: %v, balance Hex: %v\n", "0x687BEdBC8176e5E0A2d3A625ecf37a52f860968D", balance.String(), balance.Hex())
		// Collect ChangeContracts from this tracer
		for addr := range tracer.ChangeContracts {
			changeContractsMap[addr] = struct{}{}
		}
		includedTxs = append(includedTxs, txn)
		receipts = append(receipts, receipt)
		var from common.Address
		if tracer.Evm != nil {
			from = tracer.Evm.Origin
		} else {
			from = getFrom(txn)
		}
		tx := dtracer.BuildPipelineTransaction(txn, receipt, from, chainConfig, header)
		blockFile.Txs = append(blockFile.Txs, tx)
	}

	//chainReader := consensuschain.NewReader(chainConfig, dbtx, api._blockReader, logger)
	//chainReader := stagedsync.NewChainReaderImpl(chainConfig, dbtx, nil, logger)

	// newBlock, _, _, err := core.FinalizeBlockExecution(engine, stateReader, block.Header(), block.Transactions(), block.Uncles(), writer, chainConfig, ibs, receipts, block.Withdrawals(), chainReader, true, logger)
	// if err != nil {
	// 	return nil, err
	// }

	// if newBlock.Root() != block.Root() {
	// 	return nil, fmt.Errorf("state root mismatch")
	// }

	// merlin 代码中有些分叉逻辑没有按照区块高度来, 所以这个判断对早期区块不适用
	// receiptSha := types.DeriveSha(receipts)
	// if chainConfig.IsByzantium(header.Number.Uint64()) && receiptSha != block.ReceiptHash() {
	// 	return nil, fmt.Errorf("receipt hash mismatch")
	// }

	txSha := types.DeriveSha(includedTxs)
	if txSha != block.TxHash() {
		return nil, fmt.Errorf("tx hash mismatch")
	}

	// // 如果 usedGas 不为 nil，值必须等于 headerGasUsed
	// if *usedGas != header.GasUsed {
	// 	return nil, fmt.Errorf("usedGas mismatch: got %v, want %v", *usedGas, header.GasUsed)
	// }

	// // usedBlobGas 不为 nil
	// if header.BlobGasUsed == nil {
	// 	// 将 headerBlobGasUsed 视为 0
	// 	if *usedBlobGas != 0 {
	// 		return nil, fmt.Errorf("usedBlobGas is %v, but headerBlobGasUsed is nil (0 expected)", *usedBlobGas)
	// 	}
	// } else {
	// 	// headerBlobGasUsed 不为 nil，二者必须相等
	// 	if *usedBlobGas != *header.BlobGasUsed {
	// 		return nil, fmt.Errorf("usedBlobGas mismatch: got %v, want %v", *usedBlobGas, *header.BlobGasUsed)
	// 	}
	// }

	// bloom := types.CreateBloom(receipts)
	// if bloom != header.Bloom {
	// 	return nil, fmt.Errorf("bloom mismatch")
	// }

	stateDiff := writer.ToStateDiff(parentHeader.Root, block.Root())
	for addr := range changeContractsMap {
		blockFile.StorageContracts = append(blockFile.StorageContracts, strings.ToLower(addr.Hex()))
	}

	out := &dtypes.DebankOutPut{
		BlockFile:      blockFile,
		Header:         stateHeader,
		StateDiff:      stateDiff,
		ValidationHash: blockFile.Validation().ValidationHash,
	}

	return out, nil
}

func (api *TraceAPIImpl) DebankBlockRaw(ctx context.Context, blockNrOrHash rpc.BlockNumberOrHash) (*dtypes.DebankOutPut, error) {
	dbtx, err := api.kv.BeginRo(ctx)
	if err != nil {
		return nil, err
	}
	defer dbtx.Rollback()

	chainConfig, err := api.chainConfig(ctx, dbtx)
	if err != nil {
		return nil, err
	}

	blockNumber, blockHash, _, err := rpchelper.GetBlockNumber(blockNrOrHash, dbtx, api.filters)
	if err != nil {
		return nil, err
	}

	// Extract transactions from block
	block, bErr := api.blockWithSenders(ctx, dbtx, blockHash, blockNumber)
	if bErr != nil {
		return nil, bErr
	}

	if block == nil {
		return nil, fmt.Errorf("could not find block  %d", blockNumber)
	}

	header := block.Header()

	if block.NumberU64() == 0 {
		genesis := core.GenesisBlockByChainName(chainConfig.ChainName)
		return dtracer.OnGenesisBlock(block, genesis.Alloc)
	}

	LatestBlockNumber.SetUint64(block.NumberU64())
	ChainHeadNumber.SetUint64(block.NumberU64())
	LatestBlockTime.SetUint64(block.Time())

	parentHash := block.ParentHash()
	parentHeader, err := api._blockReader.Header(ctx, dbtx, parentHash, block.NumberU64()-1)
	if err != nil {
		return nil, err
	}

	stateReader, err := rpchelper.CreateHistoryStateReader(dbtx, header.Number.Uint64(), 0, api.historyV3(dbtx), chainConfig.ChainName)
	if err != nil {
		return nil, err
	}

	writer := dtracer.NewBlockStorageDiff()
	ibs := state.New(stateReader)
	usedGas := new(uint64)
	usedBlobGas := new(uint64)
	gp := new(core.GasPool).AddGas(header.GasLimit).AddBlobGas(chainConfig.GetMaxBlobGasPerBlock())

	engine, ok := api.engine().(consensus.Engine)
	if !ok {
		return nil, errors.New("engine is not consensus.Engine")
	}

	//consensusHeaderReader := consensuschain.NewReader(chainConfig, dbtx, api._blockReader, nil)
	logger := log.New("trace_debankBlock")

	consensusHeaderReader := stagedsync.NewChainReaderImpl(chainConfig, dbtx, nil, logger)
	err = core.InitializeBlockExecution2(engine, consensusHeaderReader, block.HeaderNoCopy(), chainConfig, ibs, writer, logger)
	if err != nil {
		return nil, err
	}

	includedTxs := make(types.Transactions, 0, block.Transactions().Len())
	receipts := make(types.Receipts, 0, block.Transactions().Len())
	vmConfig := vm.Config{}

	getHeader := func(hash common.Hash, number uint64) *types.Header {
		h, e := api._blockReader.Header(ctx, dbtx, hash, number)
		if e != nil {
			log.Error("getHeader error", "number", number, "hash", hash, "err", e)
		}
		return h
	}
	blockFile := &dtypes.BlockFile{
		Block:            dtracer.BuildPipelineBlock(block),
		Events:           make([]dtypes.Event, 0),
		Txs:              make([]dtypes.Transaction, 0),
		Traces:           make([]dtypes.Trace, 0),
		ErrorEvents:      make([]dtypes.Event, 0),
		ErrorTraces:      make([]dtypes.Trace, 0),
		StorageContracts: make([]string, 0),
	}
	stateHeader := dtracer.BuildPilelineBlockHeader(block)

	// Collect ChangeContracts from all tracers to deduplicate them
	changeContractsMap := make(map[common.Address]struct{})
	for i, txn := range block.Transactions() {
		ibs.SetTxContext(txn.Hash(), block.Hash(), i)
		tracer := dtracer.NewCallTracer(blockFile, txn.Hash().Hex())
		vmConfig.Debug = true
		vmConfig.Tracer = tracer
		ibs.SetHooks(&tracing.Hooks{
			OnLog: tracer.OnLog,
		})
		effectiveGasPricePercentage, err := api._blockReader.TxnEffectiveGasPricePercentage(ctx, dbtx, txn.Hash())
		if err != nil {
			return nil, err
		}
		// Log details of the transaction for debugging
		txType := txn.Type()
		txPrice := txn.GetPrice()
		txTip := txn.GetTip()
		txFeeCap := txn.GetFeeCap()
		txGas := txn.GetGas()
		txBlobGas := txn.GetBlobGas()
		txChainID := txn.GetChainID()

		log.Info("txn details",
			"hash", txn.Hash(),
			"type", txType,
			"price", txPrice,
			"tip", txTip,
			"feeCap", txFeeCap,
			"gas", txGas,
			"blobGas", txBlobGas,
			"chainID", txChainID,
			"gasPrice", txPrice,
			"to", txn.GetTo(),
			"nonce", txn.GetNonce(),
		)
		log.Info("effectiveGasPricePercentage", "value", effectiveGasPricePercentage, "txnPrice", txn.GetPrice())
		receipt, _, err := core.ApplyTransaction(chainConfig, core.GetHashFn(header, getHeader), engine, nil, gp, ibs, writer, header, txn, usedGas, usedBlobGas, vmConfig, effectiveGasPricePercentage)
		if err != nil {
			return nil, fmt.Errorf("trace_debankBlock: bn=%d, txnIdx=%d, %w", header.Number.Uint64(), i, err)
		}
		balance := ibs.GetBalance(libcommon.HexToAddress("0x687BEdBC8176e5E0A2d3A625ecf37a52f860968D"))
		fmt.Printf("[DebankBlockRaw] Balance in DebankBlockRaw, from: %v, balance: %v, balance Hex: %v\n", "0x687BEdBC8176e5E0A2d3A625ecf37a52f860968D", balance.String(), balance.Hex())
		// Collect ChangeContracts from this tracer
		for addr := range tracer.ChangeContracts {
			changeContractsMap[addr] = struct{}{}
		}
		includedTxs = append(includedTxs, txn)
		receipts = append(receipts, receipt)
		var from common.Address
		if tracer.Evm != nil {
			from = tracer.Evm.Origin
		} else {
			from = getFrom(txn)
		}
		tx := dtracer.BuildPipelineTransaction(txn, receipt, from, chainConfig, header)
		blockFile.Txs = append(blockFile.Txs, tx)
	}

	//chainReader := consensuschain.NewReader(chainConfig, dbtx, api._blockReader, logger)
	chainReader := stagedsync.NewChainReaderImpl(chainConfig, dbtx, nil, logger)

	newBlock, _, _, err := core.FinalizeBlockExecution(engine, stateReader, block.Header(), block.Transactions(), block.Uncles(), writer, chainConfig, ibs, receipts, block.Withdrawals(), chainReader, true, logger)
	if err != nil {
		return nil, err
	}

	if newBlock.Root() != block.Root() {
		return nil, fmt.Errorf("state root mismatch")
	}

	// merlin 代码中有些分叉逻辑没有按照区块高度来, 所以这个判断对早期区块不适用
	// receiptSha := types.DeriveSha(receipts)
	// if chainConfig.IsByzantium(header.Number.Uint64()) && receiptSha != block.ReceiptHash() {
	// 	return nil, fmt.Errorf("receipt hash mismatch")
	// }

	txSha := types.DeriveSha(includedTxs)
	if txSha != block.TxHash() {
		return nil, fmt.Errorf("tx hash mismatch")
	}

	// // 如果 usedGas 不为 nil，值必须等于 headerGasUsed
	// if *usedGas != header.GasUsed {
	// 	return nil, fmt.Errorf("usedGas mismatch: got %v, want %v", *usedGas, header.GasUsed)
	// }

	// // usedBlobGas 不为 nil
	// if header.BlobGasUsed == nil {
	// 	// 将 headerBlobGasUsed 视为 0
	// 	if *usedBlobGas != 0 {
	// 		return nil, fmt.Errorf("usedBlobGas is %v, but headerBlobGasUsed is nil (0 expected)", *usedBlobGas)
	// 	}
	// } else {
	// 	// headerBlobGasUsed 不为 nil，二者必须相等
	// 	if *usedBlobGas != *header.BlobGasUsed {
	// 		return nil, fmt.Errorf("usedBlobGas mismatch: got %v, want %v", *usedBlobGas, *header.BlobGasUsed)
	// 	}
	// }

	// bloom := types.CreateBloom(receipts)
	// if bloom != header.Bloom {
	// 	return nil, fmt.Errorf("bloom mismatch")
	// }

	stateDiff := writer.ToStateDiff(parentHeader.Root, newBlock.Root())
	for addr := range changeContractsMap {
		blockFile.StorageContracts = append(blockFile.StorageContracts, strings.ToLower(addr.Hex()))
	}

	out := &dtypes.DebankOutPut{
		BlockFile:      blockFile,
		Header:         stateHeader,
		StateDiff:      stateDiff,
		ValidationHash: blockFile.Validation().ValidationHash,
	}

	return out, nil
}

type DebankOutPutJs struct {
	BlockFile      *dtypes.BlockFile `json:"block_file"`
	Header         *dtypes.Header    `json:"header"`
	StateDiff      hexutility.Bytes  `json:"state_diff"`
	ValidationHash int64             `json:"validation_hash"`
}

func (api *TraceAPIImpl) DebankBlock(ctx context.Context, blockNrOrHash rpc.BlockNumberOrHash) (*DebankOutPutJs, error) {
	start := time.Now()
	output, err := api.DebankBlockRaw(ctx, blockNrOrHash)
	if err != nil {
		return nil, err
	}
	data, err := rlp.EncodeToBytes(output.StateDiff)
	if err != nil {
		return nil, err
	}

	BlockProcessTimer.Observe(float64(time.Since(start)))
	return &DebankOutPutJs{
		BlockFile:      output.BlockFile,
		Header:         output.Header,
		StateDiff:      data,
		ValidationHash: output.ValidationHash,
	}, nil
}

func (api *TraceAPIImpl) DebankBGTraceStart(ctx context.Context, region string, nodeXBucket string, chainTableBucket string, broker string, topic string, chainID string, startBlock, endBlock, maxTask uint64) (*BGTraceStatus, error) {
	if region == "" || nodeXBucket == "" || chainTableBucket == "" || broker == "" || topic == "" || chainID == "" || endBlock == 0 || startBlock > endBlock || maxTask == 0 {
		return nil, errors.New("missing required parameters")
	}

	err := dtracer.DebankTraceBackGroundMangeInstance.Start(api, region, nodeXBucket, chainTableBucket, broker, topic, chainID, startBlock, endBlock, maxTask)
	if err != nil {
		return nil, err
	}
	stat, err := api.DebankBGTraceStatus(ctx)
	if err != nil {
		return nil, err
	}

	return stat, nil
}

func (api *TraceAPIImpl) DebankBGTraceStop(ctx context.Context) (*BGTraceStatus, error) {
	stat, err := api.DebankBGTraceStatus(ctx)
	if err != nil {
		return nil, err
	}
	dtracer.DebankTraceBackGroundMangeInstance.Stop()
	return stat, nil
}

type BGTraceStatus struct {
	Start     uint64  `json:"start"`
	End       uint64  `json:"end"`
	Latest    uint64  `json:"latest"`
	Blocks    uint64  `json:"blocks"`
	StartTime uint64  `json:"start_time"`
	Duration  uint64  `json:"duration"`
	Rate      float64 `json:"rate"`
}

func (api *TraceAPIImpl) DebankBGTraceStatus(ctx context.Context) (*BGTraceStatus, error) {
	start, end, latest, startTime := dtracer.DebankTraceBackGroundMangeInstance.Status()
	return &BGTraceStatus{
		Start:     start,
		End:       end,
		Latest:    latest,
		Blocks:    latest - start + 1,
		StartTime: uint64(startTime.Unix()),
		Duration:  uint64(time.Now().Unix() - startTime.Unix()),
		Rate:      float64(latest-start+1) / float64(time.Now().Unix()-startTime.Unix()),
	}, nil

}

func getFrom(txn types.Transaction) common.Address {
	var chainId *big.Int
	switch t := txn.(type) {
	case *types.LegacyTx:
		if t.Protected() {
			chainId = types.DeriveChainId(&t.V).ToBig()
		}
	default:
		chainId = txn.GetChainID().ToBig()
	}

	var from common.Address
	signer := types.LatestSignerForChainID(chainId)
	from, _ = txn.Sender(*signer)
	return from
}
