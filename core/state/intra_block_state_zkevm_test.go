package state

import (
	"testing"

	libcommon "github.com/ledgerwatch/erigon-lib/common"
	"github.com/ledgerwatch/erigon/core/tracing"
	"github.com/ledgerwatch/erigon/core/types"
	"github.com/stretchr/testify/require"
)

func TestAddLogZkEvmCallsOnLogHook(t *testing.T) {
	t.Parallel()

	ibs := New(nil)
	var got []*types.Log
	ibs.SetHooks(&tracing.Hooks{
		OnLog: func(l *types.Log) {
			got = append(got, l)
		},
	})

	txHash := libcommon.HexToHash("0x1111")
	blockHash := libcommon.HexToHash("0x2222")
	ibs.SetTxContext(txHash, blockHash, 3)

	logEntry := &types.Log{
		Address: libcommon.HexToAddress("0x3333"),
		Data:    []byte{0x01, 0x02},
	}
	ibs.AddLog_zkEvm(logEntry)

	require.Len(t, got, 1)
	require.Equal(t, txHash, got[0].TxHash)
	require.Equal(t, blockHash, got[0].BlockHash)
	require.Equal(t, uint(3), got[0].TxIndex)
	require.Equal(t, uint(0), got[0].Index)
	require.Len(t, ibs.GetLogs(txHash), 1)
}
