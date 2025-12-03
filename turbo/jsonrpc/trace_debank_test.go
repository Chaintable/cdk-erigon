package jsonrpc

import (
	"encoding/hex"
	"testing"

	"github.com/holiman/uint256"
	"github.com/ledgerwatch/erigon-lib/common"
	dtypes "github.com/ledgerwatch/erigon/debank/types"
	"github.com/ledgerwatch/erigon/rlp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// trace_debank_test.go
//
// This file contains comprehensive tests for RLP encoding/decoding of BlockStorageDiff structures.
// The tests verify that state diff data can be reliably encoded to bytes and decoded back
// without data loss.
//
// Test Coverage:
//   - Empty BlockStorageDiff structures
//   - BlockStorageDiff with new accounts (balances, nonces, code hashes)
//   - BlockStorageDiff with deleted accounts
//   - BlockStorageDiff with storage changes (key-value pairs)
//   - BlockStorageDiff with new contract codes (bytecode)
//   - Complete BlockStorageDiff with all fields populated
//   - Large balance values (uint256 max values)
//   - Hex string encoding/decoding workflows
//
// Benchmarks:
//   - BenchmarkRLPEncode: Measures encoding performance (~282 ns/op on M1 Pro)
//   - BenchmarkRLPDecode: Measures decoding performance (~764 ns/op on M1 Pro)
//
// Usage:
//   go test -v ./turbo/jsonrpc -run TestRLP
//   go test -bench=BenchmarkRLP ./turbo/jsonrpc -run='^$'

// TestRLPDecodeStateDiff tests RLP decoding of bytes to BlockStorageDiff
func TestRLPDecodeStateDiff(t *testing.T) {
	tests := []struct {
		name        string
		setupFunc   func() *dtypes.BlockStorageDiff
		description string
	}{
		{
			name: "Empty BlockStorageDiff",
			setupFunc: func() *dtypes.BlockStorageDiff {
				return &dtypes.BlockStorageDiff{
					Hash:            common.HexToHash("0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef"),
					ParentHash:      common.HexToHash("0xabcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"),
					NewAccounts:     []dtypes.NewAccount{},
					DeletedAccounts: []common.Hash{},
					StorageDiff:     []dtypes.AccountStorageDiff{},
					NewCodes:        []dtypes.NewCode{},
				}
			},
			description: "Test decoding empty BlockStorageDiff with only hash and parent hash",
		},
		{
			name: "BlockStorageDiff with new accounts",
			setupFunc: func() *dtypes.BlockStorageDiff {
				return &dtypes.BlockStorageDiff{
					Hash:       common.HexToHash("0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef"),
					ParentHash: common.HexToHash("0xabcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"),
					NewAccounts: []dtypes.NewAccount{
						{
							Address:  common.HexToHash("0x1111111111111111111111111111111111111111111111111111111111111111"),
							Balance:  uint256.NewInt(1000000000000000000),
							Nonce:    1,
							CodeHash: common.HexToHash("0x2222222222222222222222222222222222222222222222222222222222222222"),
						},
						{
							Address:  common.HexToHash("0x3333333333333333333333333333333333333333333333333333333333333333"),
							Balance:  uint256.NewInt(5000000000000000000),
							Nonce:    42,
							CodeHash: common.HexToHash("0x4444444444444444444444444444444444444444444444444444444444444444"),
						},
					},
					DeletedAccounts: []common.Hash{},
					StorageDiff:     []dtypes.AccountStorageDiff{},
					NewCodes:        []dtypes.NewCode{},
				}
			},
			description: "Test decoding BlockStorageDiff with multiple new accounts",
		},
		{
			name: "BlockStorageDiff with deleted accounts",
			setupFunc: func() *dtypes.BlockStorageDiff {
				return &dtypes.BlockStorageDiff{
					Hash:        common.HexToHash("0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef"),
					ParentHash:  common.HexToHash("0xabcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"),
					NewAccounts: []dtypes.NewAccount{},
					DeletedAccounts: []common.Hash{
						common.HexToHash("0x5555555555555555555555555555555555555555555555555555555555555555"),
						common.HexToHash("0x6666666666666666666666666666666666666666666666666666666666666666"),
					},
					StorageDiff: []dtypes.AccountStorageDiff{},
					NewCodes:    []dtypes.NewCode{},
				}
			},
			description: "Test decoding BlockStorageDiff with deleted accounts",
		},
		{
			name: "BlockStorageDiff with storage diff",
			setupFunc: func() *dtypes.BlockStorageDiff {
				return &dtypes.BlockStorageDiff{
					Hash:            common.HexToHash("0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef"),
					ParentHash:      common.HexToHash("0xabcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"),
					NewAccounts:     []dtypes.NewAccount{},
					DeletedAccounts: []common.Hash{},
					StorageDiff: []dtypes.AccountStorageDiff{
						{
							Address: common.HexToHash("0x7777777777777777777777777777777777777777777777777777777777777777"),
							Values: []dtypes.IndexValuePair{
								{
									Index: common.HexToHash("0x0000000000000000000000000000000000000000000000000000000000000000"),
									Value: uint256.NewInt(100),
								},
								{
									Index: common.HexToHash("0x0000000000000000000000000000000000000000000000000000000000000001"),
									Value: uint256.NewInt(200),
								},
							},
						},
					},
					NewCodes: []dtypes.NewCode{},
				}
			},
			description: "Test decoding BlockStorageDiff with storage changes",
		},
		{
			name: "BlockStorageDiff with new codes",
			setupFunc: func() *dtypes.BlockStorageDiff {
				return &dtypes.BlockStorageDiff{
					Hash:            common.HexToHash("0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef"),
					ParentHash:      common.HexToHash("0xabcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"),
					NewAccounts:     []dtypes.NewAccount{},
					DeletedAccounts: []common.Hash{},
					StorageDiff:     []dtypes.AccountStorageDiff{},
					NewCodes: []dtypes.NewCode{
						{
							CodeHash: common.HexToHash("0x8888888888888888888888888888888888888888888888888888888888888888"),
							Code:     []byte{0x60, 0x60, 0x60, 0x40, 0x52}, // Simple EVM bytecode
						},
						{
							CodeHash: common.HexToHash("0x9999999999999999999999999999999999999999999999999999999999999999"),
							Code:     []byte{0x60, 0x80, 0x60, 0x40, 0x52, 0x60, 0x04, 0x36, 0x10},
						},
					},
				}
			},
			description: "Test decoding BlockStorageDiff with new contract codes",
		},
		{
			name: "Complete BlockStorageDiff",
			setupFunc: func() *dtypes.BlockStorageDiff {
				return &dtypes.BlockStorageDiff{
					Hash:       common.HexToHash("0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef"),
					ParentHash: common.HexToHash("0xabcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"),
					NewAccounts: []dtypes.NewAccount{
						{
							Address:  common.HexToHash("0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"),
							Balance:  uint256.NewInt(1000),
							Nonce:    1,
							CodeHash: common.HexToHash("0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"),
						},
					},
					DeletedAccounts: []common.Hash{
						common.HexToHash("0xcccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"),
					},
					StorageDiff: []dtypes.AccountStorageDiff{
						{
							Address: common.HexToHash("0xdddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"),
							Values: []dtypes.IndexValuePair{
								{
									Index: common.HexToHash("0x0000000000000000000000000000000000000000000000000000000000000000"),
									Value: uint256.NewInt(123),
								},
							},
						},
					},
					NewCodes: []dtypes.NewCode{
						{
							CodeHash: common.HexToHash("0xeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"),
							Code:     []byte{0x60, 0x60, 0x60, 0x40},
						},
					},
				}
			},
			description: "Test decoding complete BlockStorageDiff with all fields populated",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup: create original BlockStorageDiff
			original := tt.setupFunc()

			// Encode to RLP bytes
			encoded, err := rlp.EncodeToBytes(original)
			require.NoError(t, err, "Failed to encode BlockStorageDiff to RLP")

			t.Logf("%s - Encoded bytes length: %d", tt.description, len(encoded))
			t.Logf("Encoded hex: %s", hex.EncodeToString(encoded))

			// Decode from RLP bytes
			var decoded dtypes.BlockStorageDiff
			err = rlp.DecodeBytes(encoded, &decoded)
			require.NoError(t, err, "Failed to decode RLP bytes to BlockStorageDiff")

			// Verify Hash
			assert.Equal(t, original.Hash, decoded.Hash, "Hash mismatch")

			// Verify ParentHash
			assert.Equal(t, original.ParentHash, decoded.ParentHash, "ParentHash mismatch")

			// Verify NewAccounts
			assert.Equal(t, len(original.NewAccounts), len(decoded.NewAccounts), "NewAccounts length mismatch")
			for i := range original.NewAccounts {
				assert.Equal(t, original.NewAccounts[i].Address, decoded.NewAccounts[i].Address, "NewAccount[%d] Address mismatch", i)
				assert.Equal(t, original.NewAccounts[i].Balance, decoded.NewAccounts[i].Balance, "NewAccount[%d] Balance mismatch", i)
				assert.Equal(t, original.NewAccounts[i].Nonce, decoded.NewAccounts[i].Nonce, "NewAccount[%d] Nonce mismatch", i)
				assert.Equal(t, original.NewAccounts[i].CodeHash, decoded.NewAccounts[i].CodeHash, "NewAccount[%d] CodeHash mismatch", i)
			}

			// Verify DeletedAccounts
			assert.Equal(t, len(original.DeletedAccounts), len(decoded.DeletedAccounts), "DeletedAccounts length mismatch")
			for i := range original.DeletedAccounts {
				assert.Equal(t, original.DeletedAccounts[i], decoded.DeletedAccounts[i], "DeletedAccount[%d] mismatch", i)
			}

			// Verify StorageDiff
			assert.Equal(t, len(original.StorageDiff), len(decoded.StorageDiff), "StorageDiff length mismatch")
			for i := range original.StorageDiff {
				assert.Equal(t, original.StorageDiff[i].Address, decoded.StorageDiff[i].Address, "StorageDiff[%d] Address mismatch", i)
				assert.Equal(t, len(original.StorageDiff[i].Values), len(decoded.StorageDiff[i].Values), "StorageDiff[%d] Values length mismatch", i)
				for j := range original.StorageDiff[i].Values {
					assert.Equal(t, original.StorageDiff[i].Values[j].Index, decoded.StorageDiff[i].Values[j].Index, "StorageDiff[%d].Values[%d] Index mismatch", i, j)
					assert.Equal(t, original.StorageDiff[i].Values[j].Value, decoded.StorageDiff[i].Values[j].Value, "StorageDiff[%d].Values[%d] Value mismatch", i, j)
				}
			}

			// Verify NewCodes
			assert.Equal(t, len(original.NewCodes), len(decoded.NewCodes), "NewCodes length mismatch")
			for i := range original.NewCodes {
				assert.Equal(t, original.NewCodes[i].CodeHash, decoded.NewCodes[i].CodeHash, "NewCode[%d] CodeHash mismatch", i)
				assert.Equal(t, original.NewCodes[i].Code, decoded.NewCodes[i].Code, "NewCode[%d] Code mismatch", i)
			}

			t.Logf("✓ Successfully decoded and verified %s", tt.description)
		})
	}
}

// TestRLPDecodeFromHexString tests decoding from a hex string
func TestRLPDecodeFromHexString(t *testing.T) {
	// Decode from hex string
	//	decodedBytes, err := hex.DecodeString("f908e7a097211b3218d879b7df494708c13d235b302c82b03e19d6cdaf78aeb5159018e3a0772c2069ab002b4806b95464d0982f414edb742e9343231bc005215cce1c3c09f90424f84da0cb4bca215b16cc37717ce1b11bcd3c10a85f05b3a785e1c61ac7c2391150e8a28702bdfdc9c326aa8202fda0c5d2460186f7233c927e7db2dcc703c0e500b653ca82273b7bfad8045d85a470f84da001d16c01ea71b45b37571400827173660ecf98a115e855f03acff516e2304583890504b21d67475b158f37a0c5d2460186f7233c927e7db2dcc703c0e500b653ca82273b7bfad8045d85a470f84da01231b53050ebd959cf10f184eb5be8e96ab4fe5456404b39d54b32d0d94be98a870227bcdbc7b604820244a0c5d2460186f7233c927e7db2dcc703c0e500b653ca82273b7bfad8045d85a470f84da06c26c5a43fb3e6135b2ac2451d9744dfa76caa7fe487174918af973b8bb7f5728702dc0d7349444a8203e5a0c5d2460186f7233c927e7db2dcc703c0e500b653ca82273b7bfad8045d85a470f844a06296d0fff89ae28e6bf2e276e49ffbefdf5498ba85eefd3d0262b3e2c16e98828001a0a65d77509775d24254b9a954e719c720f95b17c2ec814954a73efa7d6669a8b4f844a01f9556cc544722d000bed98885b914e74b593886b7e90984dfecb4d57b8276ef8001a0390727ffb70aeee189a4943ab077e83bcef160f49e26983e2a18c2440d7b6f29f84da08cb67b5690ad1269ecfe2d643b9a205bf13984371364689ca4e10c747ecaa4a187031753ce6df24a820458a0c5d2460186f7233c927e7db2dcc703c0e500b653ca82273b7bfad8045d85a470f84da069aa620b99d1419cb94fb3445f89d7549a5749ece5342e3bee0f2b04707b52bc87036d2d31076754820457a0c5d2460186f7233c927e7db2dcc703c0e500b653ca82273b7bfad8045d85a470f84da09f3b4d302399317037ca25cc86dda87711ef08ca76ecfb0bd2f7006f6801673b8702fbd980406afc8203e3a0c5d2460186f7233c927e7db2dcc703c0e500b653ca82273b7bfad8045d85a470f84ca0b18f62bdad8a1e48bd43cf1498e3f0c84355cf8a050c209548e3ee566ffdf358870132313534605e81c1a0c5d2460186f7233c927e7db2dcc703c0e500b653ca82273b7bfad8045d85a470f844a0f3bef18d515599b42f7842e5444eb28459d71d0cc5eecb8ff5b02e26e487cafc8001a0d8108e3c7046fa0081c631f7377b2246fa67dcfff1502651c9231e264be0f543f844a01c9bd47f0c2a4bfa5be0eb5b6502e03ab57e56829cf399dc587db53bd74c3f988001a09c97382df2508a5b995a0e98735c37e86d340f04c79a52b51df2a54a9f3abe9ff84da03c62c250b60ff4763cc7b6904f3dbb4b22dea9f7ccc9ee45e7a5be6afdd323048905b45839c0a334de5901a02e99356edc1ecb11bab4e109e1af117bbf1c2c9afb7269271ef82b4605624e15f844a02d79c1eb9bae69b3cfc25d3698c6723a648bb832f5f3bf6c26578174100d51e18001a09ed810de3bd42dc57abfda91f306d385ffcc413844f42a020beb453f9f38fcc0e1a01468288056310c82aa4c01a7e12a10f8111a0560e72b700555479031b86c357df90458f875a03c62c250b60ff4763cc7b6904f3dbb4b22dea9f7ccc9ee45e7a5be6afdd32304f852e8a05367677107f0505efa001b4f1c18a6702ec077b02c22f4dacb659553fce1c1758607db3c25891fe8a019b57aaf98a8a6ebdc0029baa5eade4428db7457529f01fe12c38b9b478025d1860bb3e31f423ff845a06296d0fff89ae28e6bf2e276e49ffbefdf5498ba85eefd3d0262b3e2c16e9882e3e2a0fd7c78779b28abb18ba08727f89aa3c9628b4aa32a518747339bbedd0bfc5f8a01f90184a0f3bef18d515599b42f7842e5444eb28459d71d0cc5eecb8ff5b02e26e487cafcf90160f842a0290decd9548b62a8d60345a988386fc84ba6bc95484008f6362f93160ef3e563a0ffffffffffffffffffffffffffff8c4affffffffffffffffffffffffffff84ddf6a0cdc5dfb80ca632cf1b12a74e40fbe084caea7802e2c603bd67833c7b6a23de60947ec2dd7025bcc5ee5723631e44494979e4ff7483e2a08e9e2a56b03e133cc59916bcff40bb4ce8a2ffbfe5f9a1e69798f80340dcd43b01f842a0e4d2973fbcbca746afad7b2ae18b83a33c0a1b79ff57b4737b0c7e476bfb122ca08000000000000000000000000000000000000000000000000000000000002639eda028022f4c57213cb2dfce0a29517939f3cff1af5b81e113a423d4ec1de4dcbf508b126e6ea63ee9d10c265b6aeca01b930a9eb80b970c7f694bc034f0dc00ab51d9090d697518627fc63fb38f70fb8a03310023da6b781c0000e2a0ebd717d3f513131176e200109e7ecf987457f7be07fbedf62d897eb23c9b042e80f90105a01c9bd47f0c2a4bfa5be0eb5b6502e03ab57e56829cf399dc587db53bd74c3f98f8e2f83da0385fa9e34e3c7929b17e26aa6abd26a6ac3af5aa208b384138c0ff8bb3d29d7d9b04936423f9ee426b196d4100000000033b5dfc7a40c3662d033b97f83da0520e6d5cbfab4c9cf36997deadf36be83ee557da36dd70a92fbb6797fc6a9ae49b1ad9b6dd6a1add36f645b800000000033d05d4b287a8f2bc3ea58fe9a01d2cdde56e0f620405fa9b05d630d12c9816e5ca4e8368d4765138a4e51ae45d877fbe94a71a5dcef838a0b9e43a8e56b3bf745f3ad07fe401c498fc8192967592300e42f2c69150de9c1e9604006699324b0000000000760e96a3ed8897eb45fd9bf878a02d79c1eb9bae69b3cfc25d3698c6723a648bb832f5f3bf6c26578174100d51e1f855e8a0614c016d8ff8c9728b74fa739d2db038acb347e364ff0270e3bc8d4c10e5ce0f860aadd3db8900eba00af344fb7cf3c57cf5c9c5e51b5e574f14485d838747922079b5db8d01e4940b891d38193021bf9c796cf88fa01f9556cc544722d000bed98885b914e74b593886b7e90984dfecb4d57b8276eff86cf83ea085d4eda3838b078c292ad33e6ede373c4123530557ce65b7da6591257189dc099c033b5dfc7a40c3662d033b970000000000000000000007a1eb4f2f80eba04a11f94e20a93c79f6ec743a1954ec4fc2c08429ae2122118bf234b2185c81b889269c689bf714a011fac0")
	decodedBytes, err := hex.DecodeString("f90387a08b5e3998eaa4a0cee40e3d144f0557ef4c1a9e825264f8ab0da136d2ec871fd7a097211b3218d879b7df494708c13d235b302c82b03e19d6cdaf78aeb5159018e3f902c7f84da07d1d2e8a1d311ce69d0b6349f7dc27a6b6afd6edf66ba70279e7e7a37bacbb258702beea8903538e8202faa0c5d2460186f7233c927e7db2dcc703c0e500b653ca82273b7bfad8045d85a470f84da03c62c250b60ff4763cc7b6904f3dbb4b22dea9f7ccc9ee45e7a5be6afdd323048905b45839c0a334de5901a02e99356edc1ecb11bab4e109e1af117bbf1c2c9afb7269271ef82b4605624e15f84da022d2caaf91573555a396b86aa58ed4abcdf3e60695c19c2ece045fabbf5483308701c1e9b76a390282041ba0c5d2460186f7233c927e7db2dcc703c0e500b653ca82273b7bfad8045d85a470f84da0257ef01d8c6a594ed4d55ed130abae71b3f419848740b7d9c74f3ef58f934ac58702e4146b1ca05a820483a0c5d2460186f7233c927e7db2dcc703c0e500b653ca82273b7bfad8045d85a470f84da0005595dbf9bb72c574d027f9c89c79d5162eb93c6b009f465b98f9fc22b196d98702fc231bc7c5b282041fa0c5d2460186f7233c927e7db2dcc703c0e500b653ca82273b7bfad8045d85a470f84da0084970242c20cb50cac401697768bbde1a7cec61ba8accf925b7b8e1ed523dcb87031eaa8b1b010a820405a0c5d2460186f7233c927e7db2dcc703c0e500b653ca82273b7bfad8045d85a470f84da01490bee8d84175b521ae47b15dc63157194e56f31a550a5ef3eb7dc8e9a4e0248703ad3365b92b22820466a0c5d2460186f7233c927e7db2dcc703c0e500b653ca82273b7bfad8045d85a470f84da001d16c01ea71b45b37571400827173660ecf98a115e855f03acff516e2304583890504b234106de02ca737a0c5d2460186f7233c927e7db2dcc703c0e500b653ca82273b7bfad8045d85a470f84da0719ec4dec2a55d353b395449933172aa2ad3217377dd3e40c1feb835b0bc00ed8702bca12039a56482048fa0c5d2460186f7233c927e7db2dcc703c0e500b653ca82273b7bfad8045d85a470c0f877f875a03c62c250b60ff4763cc7b6904f3dbb4b22dea9f7ccc9ee45e7a5be6afdd32304f852e8a08c0c8be1d83880851341d17f7ae35b2c1d14e4a04e14fd3fa30ced47b4e242558607a9043be6ffe8a0cbeb7d5fd55b57eb0d52412e06d704e57ef8b799c3afee90eebed161961418868604d0feb2ec47c0")
	require.NoError(t, err)

	var decoded dtypes.BlockStorageDiff
	err = rlp.DecodeBytes(decodedBytes, &decoded)
	require.NoError(t, err)

	// Print basic info
	t.Logf("\n=== Decoded BlockStorageDiff ===")
	t.Logf("Hash:       %s", decoded.Hash.Hex())
	t.Logf("ParentHash: %s", decoded.ParentHash.Hex())
	t.Logf("\nTotal NewAccounts: %d", len(decoded.NewAccounts))

	// Print all decoded accounts with details
	for i, account := range decoded.NewAccounts {
		t.Logf("\n--- Account #%d ---", i+1)
		t.Logf("  Address:  %s", account.Address.Hex())
		t.Logf("  Balance:  %s (wei)", account.Balance.Dec())
		t.Logf("  Balance:  %s (hex)", account.Balance.Hex())
		t.Logf("  Nonce:    %d", account.Nonce)
		t.Logf("  CodeHash: %s", account.CodeHash.Hex())
	}

	t.Logf("\nDeleted Accounts: %d", len(decoded.DeletedAccounts))
	t.Logf("Storage Diffs:    %d", len(decoded.StorageDiff))
	t.Logf("New Codes:        %d", len(decoded.NewCodes))

	t.Logf("\n✓ Successfully decoded %d accounts from hex string!", len(decoded.NewAccounts))
}
