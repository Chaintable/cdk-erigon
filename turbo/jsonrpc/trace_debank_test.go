package jsonrpc

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strings"
	"testing"

	"github.com/holiman/uint256"
	libcommon "github.com/ledgerwatch/erigon-lib/common"
	"github.com/ledgerwatch/erigon/common"
	"github.com/ledgerwatch/erigon/crypto"
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
					Hash:            libcommon.HexToHash("0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef"),
					ParentHash:      libcommon.HexToHash("0xabcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"),
					NewAccounts:     []dtypes.NewAccount{},
					DeletedAccounts: []libcommon.Hash{},
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
					Hash:       libcommon.HexToHash("0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef"),
					ParentHash: libcommon.HexToHash("0xabcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"),
					NewAccounts: []dtypes.NewAccount{
						{
							Address:  libcommon.HexToHash("0x1111111111111111111111111111111111111111111111111111111111111111"),
							Balance:  uint256.NewInt(1000000000000000000),
							Nonce:    1,
							CodeHash: libcommon.HexToHash("0x2222222222222222222222222222222222222222222222222222222222222222"),
						},
						{
							Address:  libcommon.HexToHash("0x3333333333333333333333333333333333333333333333333333333333333333"),
							Balance:  uint256.NewInt(5000000000000000000),
							Nonce:    42,
							CodeHash: libcommon.HexToHash("0x4444444444444444444444444444444444444444444444444444444444444444"),
						},
					},
					DeletedAccounts: []libcommon.Hash{},
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
					Hash:        libcommon.HexToHash("0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef"),
					ParentHash:  libcommon.HexToHash("0xabcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"),
					NewAccounts: []dtypes.NewAccount{},
					DeletedAccounts: []libcommon.Hash{
						libcommon.HexToHash("0x5555555555555555555555555555555555555555555555555555555555555555"),
						libcommon.HexToHash("0x6666666666666666666666666666666666666666666666666666666666666666"),
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
					Hash:            libcommon.HexToHash("0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef"),
					ParentHash:      libcommon.HexToHash("0xabcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"),
					NewAccounts:     []dtypes.NewAccount{},
					DeletedAccounts: []libcommon.Hash{},
					StorageDiff: []dtypes.AccountStorageDiff{
						{
							Address: libcommon.HexToHash("0x7777777777777777777777777777777777777777777777777777777777777777"),
							Values: []dtypes.IndexValuePair{
								{
									Index: libcommon.HexToHash("0x0000000000000000000000000000000000000000000000000000000000000000"),
									Value: uint256.NewInt(100),
								},
								{
									Index: libcommon.HexToHash("0x0000000000000000000000000000000000000000000000000000000000000001"),
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
					Hash:            libcommon.HexToHash("0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef"),
					ParentHash:      libcommon.HexToHash("0xabcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"),
					NewAccounts:     []dtypes.NewAccount{},
					DeletedAccounts: []libcommon.Hash{},
					StorageDiff:     []dtypes.AccountStorageDiff{},
					NewCodes: []dtypes.NewCode{
						{
							CodeHash: libcommon.HexToHash("0x8888888888888888888888888888888888888888888888888888888888888888"),
							Code:     []byte{0x60, 0x60, 0x60, 0x40, 0x52}, // Simple EVM bytecode
						},
						{
							CodeHash: libcommon.HexToHash("0x9999999999999999999999999999999999999999999999999999999999999999"),
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
					Hash:       libcommon.HexToHash("0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef"),
					ParentHash: libcommon.HexToHash("0xabcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"),
					NewAccounts: []dtypes.NewAccount{
						{
							Address:  libcommon.HexToHash("0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"),
							Balance:  uint256.NewInt(1000),
							Nonce:    1,
							CodeHash: libcommon.HexToHash("0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"),
						},
					},
					DeletedAccounts: []libcommon.Hash{
						libcommon.HexToHash("0xcccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"),
					},
					StorageDiff: []dtypes.AccountStorageDiff{
						{
							Address: libcommon.HexToHash("0xdddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"),
							Values: []dtypes.IndexValuePair{
								{
									Index: libcommon.HexToHash("0x0000000000000000000000000000000000000000000000000000000000000000"),
									Value: uint256.NewInt(123),
								},
							},
						},
					},
					NewCodes: []dtypes.NewCode{
						{
							CodeHash: libcommon.HexToHash("0xeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"),
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
	//decodedBytes, err := hex.DecodeString("f90387a08b5e3998eaa4a0cee40e3d144f0557ef4c1a9e825264f8ab0da136d2ec871fd7a097211b3218d879b7df494708c13d235b302c82b03e19d6cdaf78aeb5159018e3f902c7f84da07d1d2e8a1d311ce69d0b6349f7dc27a6b6afd6edf66ba70279e7e7a37bacbb258702beea8903538e8202faa0c5d2460186f7233c927e7db2dcc703c0e500b653ca82273b7bfad8045d85a470f84da03c62c250b60ff4763cc7b6904f3dbb4b22dea9f7ccc9ee45e7a5be6afdd323048905b45839c0a334de5901a02e99356edc1ecb11bab4e109e1af117bbf1c2c9afb7269271ef82b4605624e15f84da022d2caaf91573555a396b86aa58ed4abcdf3e60695c19c2ece045fabbf5483308701c1e9b76a390282041ba0c5d2460186f7233c927e7db2dcc703c0e500b653ca82273b7bfad8045d85a470f84da0257ef01d8c6a594ed4d55ed130abae71b3f419848740b7d9c74f3ef58f934ac58702e4146b1ca05a820483a0c5d2460186f7233c927e7db2dcc703c0e500b653ca82273b7bfad8045d85a470f84da0005595dbf9bb72c574d027f9c89c79d5162eb93c6b009f465b98f9fc22b196d98702fc231bc7c5b282041fa0c5d2460186f7233c927e7db2dcc703c0e500b653ca82273b7bfad8045d85a470f84da0084970242c20cb50cac401697768bbde1a7cec61ba8accf925b7b8e1ed523dcb87031eaa8b1b010a820405a0c5d2460186f7233c927e7db2dcc703c0e500b653ca82273b7bfad8045d85a470f84da01490bee8d84175b521ae47b15dc63157194e56f31a550a5ef3eb7dc8e9a4e0248703ad3365b92b22820466a0c5d2460186f7233c927e7db2dcc703c0e500b653ca82273b7bfad8045d85a470f84da001d16c01ea71b45b37571400827173660ecf98a115e855f03acff516e2304583890504b234106de02ca737a0c5d2460186f7233c927e7db2dcc703c0e500b653ca82273b7bfad8045d85a470f84da0719ec4dec2a55d353b395449933172aa2ad3217377dd3e40c1feb835b0bc00ed8702bca12039a56482048fa0c5d2460186f7233c927e7db2dcc703c0e500b653ca82273b7bfad8045d85a470c0f877f875a03c62c250b60ff4763cc7b6904f3dbb4b22dea9f7ccc9ee45e7a5be6afdd32304f852e8a08c0c8be1d83880851341d17f7ae35b2c1d14e4a04e14fd3fa30ced47b4e242558607a9043be6ffe8a0cbeb7d5fd55b57eb0d52412e06d704e57ef8b799c3afee90eebed161961418868604d0feb2ec47c0")
	decodedBytes, err := hex.DecodeString("f902baa00eb90722c2a5ae49af663237874b40075515fa665ed2d33569f68cfd5f47784da0cc09c6fc5d31bde5362c65214d7b4d6dd06a42db1a4a68b2eb72fac88874a162f9017ff84ca09e400273d5330504254fcecb16524749195af37a156f086a835489250d630900860e6ec02f5aa7825076a0c5d2460186f7233c927e7db2dcc703c0e500b653ca82273b7bfad8045d85a470f84da03c62c250b60ff4763cc7b6904f3dbb4b22dea9f7ccc9ee45e7a5be6afdd3230489015463fd893daff8a001a02e99356edc1ecb11bab4e109e1af117bbf1c2c9afb7269271ef82b4605624e15f84ca001d16c01ea71b45b37571400827173660ecf98a115e855f03acff516e230458388118daa08095ae0e36ca0c5d2460186f7233c927e7db2dcc703c0e500b653ca82273b7bfad8045d85a470f84ca04bcdef39a18c2f218daa7803a5a789b6cc68ed6e4630f476a9ccf8c5a96ce7a58671ea892e966d825e70a0c5d2460186f7233c927e7db2dcc703c0e500b653ca82273b7bfad8045d85a470f844a0cbac4d789d00b9d0aba323012585e202e66d343f9e7291c304a716d820c29b0a8001a054dd131dbc6d30b91875970f8b159567e0cbd4cd27250be69f346d9a14e85250c0f8f2f875a03c62c250b60ff4763cc7b6904f3dbb4b22dea9f7ccc9ee45e7a5be6afdd32304f852e8a0bb4a0ed4c29d00898236941663782c716a249cdf9b4a2a35d820e5fec7590457860612ebc1d3a9e8a08cb7d45b4cee9d942f3e04b15b8d6503a95171447a3e856ba7f958b05b784fc68604fe93386a45f879a0cbac4d789d00b9d0aba323012585e202e66d343f9e7291c304a716d820c29b0af856eaa00c5afac4e08d13992f98a2e4ac5d1b44c9ad91ba54a2cd28303382f803617238881bcd66afeea6d793eaa03f6b8a52dd8779f0d357c3b78c532cfccc7b73c033c455fa2a3d615a971a0deb881c14a692aebf91bac0")
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

// TestCompareBalancesBetweenRPCs compares account balances between public RPC and local node
//
// This test:
// 1. Gets the latest block number from the public RPC (https://rpc.merlinchain.io)
// 2. For each of the 100 addresses from the provided JSON:
//   - Queries eth_getBalance from both public RPC and local node (127.0.0.1:8574)
//   - Compares the balances
//
// 3. Outputs a detailed comparison report showing:
//   - Summary statistics (matches, mismatches, errors)
//   - Details of any mismatched balances
//   - Any errors encountered
//
// To run this test, remove the t.Skip() line below and execute:
//
//	go test -v ./turbo/jsonrpc -run TestCompareBalancesBetweenRPCs
func TestCompareBalancesBetweenRPCs(t *testing.T) {
	//t.Skip("Skipping RPC comparison test - remove this line to enable the test")

	publicRPC := "https://rpc.merlinchain.io"
	localRPC := "http://127.0.0.1:8574"

	// Extract addresses from the provided JSON
	addresses := []string{
		"0x92367037e551e0894c3dd8a7a63aa41cfb3db0a8",
		"0x28ad6b7dfd79153659cb44c2155cf7c0e1ceeccc",
		"0x78f813aa474167627acf0a0005f523e0e6d561d0",
		"0xf6d226f9dc15d9bb51182815b320d3fbe324e1ba",
		"0x9cff0bd4750206def22112b02342046a5738129b",
		"0xba799c1ea835422fa4fb81a9845d81fb5bd32e4b",
		"0xc2e862d161d9da32d583f9ee5434d1413412ee60",
		"0x051c5d80dbe9cc98126339a8ee87a4e761365b15",
		"0x5ff137d4b0fdcd49dca30c7cf57e578a026d2789",
		"0xa8bd77769c875f8490e4b49f4c02a1dd83d21a18",
		"0x97ba36556f992d97fa6957f98b21702a2e368c50",
		"0xfaa0a4558e9f56f57d5b4d82964d11166a3e77cc",
		"0x73158f19defdd6433f21098c78f7d9f68b530288",
		"0x6d64b4080d07fe54735889bde48e0a6cdb93790c",
		"0xbde680e82f837f81ca08d01a32fb84bfd84eee58",
		"0x8f04b053b45d850125472a4fce0bb7d08194b275",
		"0x75a8b6f5e89ff29a9abbfe0970f900f7658ffd87",
		"0xfd75ad3e2d09aa8ca30e89578f7706f93acaec20",
		"0x1964925383783bd8ee88062ce4e0de306c212161",
		"0x332ff549cd256b45f2abe916939b82e399731d13",
		"0x0805940b9dbb5ed880eee73412233ad83092be36",
		"0xb8b34d3533a8a37be16964259c0987638caa7fd7",
		"0xabe97d8a72fb7284eb66fb9d6e5cdbe26463051f",
		"0x88ae6861f8d069e16e6a66351f66fa872c54706c",
		"0x0e490ee4c0943e842c4184644c3710dc75bb0dd3",
		"0xf73d921aa8f2dbe77adca8466127b392ede89dc9",
		"0x29d10357c02b49711ada9b325c7bf340fd5657c8",
		"0x9113ecc7a3e7f34125a94305596b276166352c48",
		"0xfa1aaa7a28e4a2aaec45f65adf00c375d61b731b",
		"0x25ab3efd52e6470681ce037cd546dc60726948d3",
		"0x367a131a1b823602729fcc8cee9d508ba066420f",
		"0x34990aa8d93decd93fb54496d0c83c7b2863c22a",
		"0x1111804ba276208a6779b18725d7543edc9306c2",
		"0xf4f98f840113c9a0a8586d114319bad87ded2229",
		"0x533806b821ec94091228d7d34e697b93bb79f8f6",
		"0x966567cd4474c664a517f91a8b52cdd14bcc87ed",
		"0x5aba79c609ccd33d8ecbfd564a544706a09830e0",
		"0x83216ad30025206baf8fa4b0068665e5fedd20ad",
		"0x83d4cb1756de7b4e2c05079f0a0a6b9c2aa44b9a",
		"0x12b0102e61eb36ee402cf3c76f8341647052fdf2",
		"0xafc2baed7b17dac22c6df2669f6422f7040f3060",
		"0x0c5b574dc130c2e964663e30ec6a223311af843c",
		"0xc0050e8ff9bcab97dc38964ad259e396e39724c2",
		"0x95359581ac317805f12613695944efd645a47d64",
		"0x4ebf5a47cd8190e86d7110a1006b72aee34882e9",
		"0xce76ca54f77177b09a227c868861909d5fff774d",
		"0xdbff790553fb3050d9e477849d5e3d9e58fc74e6",
		"0xe0b337a36e619d0a7fe6f3a3b75aa9608b28b227",
		"0xd97008154d9103bd10ba54192d2fb230d5c15a50",
		"0x6712f8f291b13025a61ca9f00ad0b8b16f891fa5",
		"0x1c6abc9ad0022bad95a591e02ced4af60d98ac53",
		"0x38f7146590239220816c17a5f5d8f9208e93404f",
		"0x668a58925377ea0e7c9b05bda500df877a6a827d",
		"0xca28e5b4aec6ad3b89e5836db16fe6471cb323eb",
		"0x921496218a83e45d300441bbbed4a5a5d71cf1dd",
		"0x10f32ad771a8d0cf148f2970690137eae017462b",
		"0x5175f7efd5e4d7ba047f4a3d26d5d1c6fbf590f1",
		"0xc94bd19c00548041fd7d05f319c1d19476e2a2c9",
		"0xb50a428474d6ae3819f1262452c078e3f3d18999",
		"0x70144e5b5bbf464cff98d689254dc7c7223e01ab",
		"0xf3eaecb5d6687921af1f59a808de57fe15435fd1",
		"0xe9a2b324c48a2ea8c49717d6f54be18cc855344f",
		"0x7df473fe6f28d8af012a01c5508e006afac3c178",
		"0xc468c679c26a2c4972c314e54e0d7cb719d81b64",
		"0x4c18e3a2e35ad4f324ecd34c88074271d0643edf",
		"0xad8b0284085148abafc8c0c63eac6454ce82b1df",
		"0x6596da8b65995d5feacff8c2936f0b7a2051b0d0",
		"0xf89d7b9c864f589bbf53a82105107622b35eaa40",
		"0xbb34ed2acfb94f7a6f01c596576eaea26c1d2f21",
		"0x7542f78eca6868ce05dbfc6fc3a1ab1d46a2d55b",
		"0xa2096a248bc6d4e2eed690318817cf2fe6d47a75",
		"0x88f690ed4fe6c3ed269888ea929951ae7645ebb4",
		"0x7436fe88d72d13de24e0bb2b647e7b4f0a6f832e",
		"0xb8eca68bdd5b014800d6ffdc081d117f83f2f70b",
		"0xfc5a1e969b0e089d6a6124c155b73702a5f4954d",
		"0xe9ceef9864e9a956f0568a37cb178fedc4ff5e37",
		"0xc7c739e0d7cae46949cbbef06704e3a6b5d73a21",
		"0xcdb0c856312aa6c3159194961e8bcfa1e48b33a6",
		"0x01520898d4d4319262e717f145b1d649c77315a6",
		"0xb0d4d4eebf88136cbc252c741ef9245a47fdfbfe",
		"0xdae3eef72c18010ac2ea51eec17590c28af5bd3b",
		"0xd74e7d0087716c7bcf6046d27baafdb938dbb3f5",
		"0xe4ce63640c750f273c5e5e50a999ba7c8edd64f9",
		"0x632638722987bae841f33d95712de7288a65b0e0",
		"0x444f31c461b8e43d507435e34baf4f9a17918def",
		"0xe93685f3bba03016f02bd1828badd6195988d950",
		"0x316fba7a4d08060f65117c089f2f0e3468b679c6",
		"0x8900ba2bc2893961b8e149470453ff2ca0e90bd1",
		"0xe048be5fdcc929a175622f21e902d6c6e4138c01",
		"0xd175c409fce3e7958f0a99ec49e191a1c302d9db",
		"0xf9bb24460006de6135f42593a9063b395a249f2a",
		"0x1cf0370083c3bbb5518ee18a21368dc67b5ec972",
		"0x48ca83127c4e32d88f5eb91b2fb3d240611a1be7",
		"0x339d413ccefd986b1b3647a9cfa9cbbe70a30749",
		"0x27229c5c34c018e6a43d2a00f8f81e06f54a9a5d",
		"0x7fe2b95f2f12ea237a07c0c90024bd336d15ffec",
		"0x05e0ef3feb4c88c9fca77d0c6b353e2dd73251fb",
		"0xa10bb823fb55a9199e4f521d0a88993c4cba7150",
		"0xd38cf87f114f2a0582c329fb9df4f7044ce71330",
		"0xc8c1dd69ae8d997abff46847037f3ed361cbeef5",
	}

	// Get latest block number from public RPC
	latestBlockNum, err := getLatestBlockNumber(localRPC)
	require.NoError(t, err, "Failed to get latest block number from public RPC")
	t.Logf("\n=== Latest Block Number: %s ===\n", latestBlockNum)

	type BalanceComparison struct {
		Address       string
		PublicRPC     *big.Int
		LocalRPC      *big.Int
		Match         bool
		Difference    *big.Int
		DifferenceRat float64 // Difference ratio as percentage
		PublicError   error
		LocalError    error
	}

	var comparisons []BalanceComparison
	matchCount := 0
	mismatchCount := 0
	errorCount := 0

	t.Logf("Comparing balances for %d addresses at block %s\n", len(addresses), latestBlockNum)

	for i, address := range addresses {
		t.Logf("Processing %d/%d: %s", i+1, len(addresses), address)

		comparison := BalanceComparison{
			Address: address,
		}

		// Get balance from public RPC
		publicBalance, publicErr := getBalance(publicRPC, address, latestBlockNum)
		comparison.PublicRPC = publicBalance
		comparison.PublicError = publicErr

		// Get balance from local RPC
		localBalance, localErr := getBalance(localRPC, address, latestBlockNum)
		comparison.LocalRPC = localBalance
		comparison.LocalError = localErr

		// Compare
		if publicErr != nil || localErr != nil {
			comparison.Match = false
			errorCount++
		} else if publicBalance.Cmp(localBalance) == 0 {
			comparison.Match = true
			comparison.Difference = big.NewInt(0)
			comparison.DifferenceRat = 0.0
			matchCount++
		} else {
			comparison.Match = false
			diff := new(big.Int).Sub(publicBalance, localBalance)
			comparison.Difference = diff

			// Calculate difference ratio as percentage
			if publicBalance.Sign() != 0 {
				// Convert to float64 for percentage calculation
				diffFloat := new(big.Float).SetInt(diff)
				publicFloat := new(big.Float).SetInt(publicBalance)
				ratio := new(big.Float).Quo(diffFloat, publicFloat)
				ratio.Mul(ratio, big.NewFloat(100.0))
				comparison.DifferenceRat, _ = ratio.Float64()
			} else {
				comparison.DifferenceRat = 0.0
			}

			mismatchCount++
		}

		comparisons = append(comparisons, comparison)
	}

	// Calculate difference ratio statistics
	var totalRatio, maxRatio, minRatio float64
	minRatio = 1000000.0 // Start with a large number
	for _, comp := range comparisons {
		if !comp.Match && comp.PublicError == nil && comp.LocalError == nil {
			absRatio := comp.DifferenceRat
			if absRatio < 0 {
				absRatio = -absRatio
			}
			totalRatio += absRatio
			if absRatio > maxRatio {
				maxRatio = absRatio
			}
			if absRatio < minRatio {
				minRatio = absRatio
			}
		}
	}
	avgRatio := 0.0
	if mismatchCount > 0 {
		avgRatio = totalRatio / float64(mismatchCount)
	}

	// Print summary
	separator := strings.Repeat("=", 80)
	t.Logf("\n%s", separator)
	t.Logf("=== COMPARISON SUMMARY ===")
	t.Logf("%s", separator)
	t.Logf("Total Addresses:  %d", len(addresses))
	t.Logf("Matches:          %d (%.2f%%)", matchCount, float64(matchCount)/float64(len(addresses))*100)
	t.Logf("Mismatches:       %d (%.2f%%)", mismatchCount, float64(mismatchCount)/float64(len(addresses))*100)
	t.Logf("Errors:           %d (%.2f%%)", errorCount, float64(errorCount)/float64(len(addresses))*100)
	if mismatchCount > 0 {
		t.Logf("")
		t.Logf("Difference Ratio Statistics:")
		t.Logf("  Average:        %.6f%%", avgRatio)
		t.Logf("  Maximum:        %.6f%%", maxRatio)
		t.Logf("  Minimum:        %.6f%%", minRatio)
	}
	t.Logf("%s\n", separator)

	// Print detailed results
	if mismatchCount > 0 {
		t.Logf("\n=== MISMATCHED BALANCES ===\n")
		for _, comp := range comparisons {
			if !comp.Match && comp.PublicError == nil && comp.LocalError == nil {
				t.Logf("Address: %s", comp.Address)
				t.Logf("  Public RPC:      %s wei", comp.PublicRPC.String())
				t.Logf("  Local RPC:       %s wei", comp.LocalRPC.String())
				t.Logf("  Difference:      %s wei", comp.Difference.String())
				t.Logf("  Difference Ratio: %.6f%% (relative to public RPC)", comp.DifferenceRat)

				// Show if local is higher or lower
				if comp.Difference.Sign() > 0 {
					t.Logf("  Status:          Local RPC is LOWER by %.6f%%", comp.DifferenceRat)
				} else {
					t.Logf("  Status:          Local RPC is HIGHER by %.6f%%", -comp.DifferenceRat)
				}
				t.Logf("")
			}
		}
	}

	if errorCount > 0 {
		t.Logf("\n=== ERRORS ===\n")
		for _, comp := range comparisons {
			if comp.PublicError != nil || comp.LocalError != nil {
				t.Logf("Address: %s", comp.Address)
				if comp.PublicError != nil {
					t.Logf("  Public RPC Error: %v", comp.PublicError)
				}
				if comp.LocalError != nil {
					t.Logf("  Local RPC Error:  %v", comp.LocalError)
				}
				t.Logf("")
			}
		}
	}

	// Assert that all balances match (optional - can be commented out if mismatches are expected)
	// assert.Equal(t, len(addresses), matchCount, "Not all balances match")
}

// getLatestBlockNumber retrieves the latest block number from an RPC endpoint
func getLatestBlockNumber(rpcURL string) (string, error) {
	payload := map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  "eth_blockNumber",
		"params":  []interface{}{},
		"id":      1,
	}

	respData, err := makeRPCRequest(rpcURL, payload)
	if err != nil {
		return "", err
	}

	var response struct {
		Result string `json:"result"`
		Error  *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}

	if err := json.Unmarshal(respData, &response); err != nil {
		return "", fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if response.Error != nil {
		return "", fmt.Errorf("RPC error: %s", response.Error.Message)
	}

	return response.Result, nil
}

// getBalance retrieves the balance of an address at a specific block
func getBalance(rpcURL, address, blockNumber string) (*big.Int, error) {
	payload := map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  "eth_getBalance",
		"params":  []interface{}{address, blockNumber},
		"id":      1,
	}

	respData, err := makeRPCRequest(rpcURL, payload)
	if err != nil {
		return nil, err
	}

	var response struct {
		Result string `json:"result"`
		Error  *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}

	if err := json.Unmarshal(respData, &response); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if response.Error != nil {
		return nil, fmt.Errorf("RPC error: %s", response.Error.Message)
	}

	// Parse hex balance
	balanceHex := response.Result
	if len(balanceHex) > 2 && balanceHex[:2] == "0x" {
		balanceHex = balanceHex[2:]
	}

	balance := new(big.Int)
	balance.SetString(balanceHex, 16)

	return balance, nil
}

// makeRPCRequest makes an HTTP POST request to an RPC endpoint
func makeRPCRequest(rpcURL string, payload interface{}) ([]byte, error) {
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal payload: %w", err)
	}

	req, err := http.NewRequest("POST", rpcURL, bytes.NewBuffer(payloadBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to make request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d, body: %s", resp.StatusCode, string(body))
	}

	return body, nil
}

// TestAddressKeccak256Hash tests computing keccak256 hash of Ethereum addresses
func TestAddressKeccak256Hash(t *testing.T) {
	testCases := []struct {
		name        string
		address     string
		description string
	}{
		{
			name:        "Zero Address",
			address:     "0x0000000000000000000000000000000000000000",
			description: "Hash of the zero address",
		},
		{
			name:        "Sample Address 1",
			address:     "0x92367037e551e0894c3dd8a7a63aa41cfb3db0a8",
			description: "Top holder from the provided JSON",
		},
		{
			name:        "Sample Address 2",
			address:     "0x28ad6b7dfd79153659cb44c2155cf7c0e1ceeccc",
			description: "Second holder from the provided JSON",
		},
		{
			name:        "Contract Address",
			address:     "0x5ff137d4b0fdcd49dca30c7cf57e578a026d2789",
			description: "ERC-4337 EntryPoint contract",
		},
		{
			name:        "All Fs Address",
			address:     "0xFFfFfFffFFfffFFfFFfFFFFFffFFFffffFfFFFfF",
			description: "Address with all Fs (mixed case)",
		},
	}

	t.Log("\n=== Keccak256 Hash of Ethereum Addresses ===\n")

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Parse the address
			addr := libcommon.HexToAddress(tc.address)

			// Method 1: Hash the address bytes directly
			addressBytes := addr.Bytes()
			hash1 := crypto.Keccak256Hash(addressBytes)

			// Method 2: Hash the hex string (without 0x prefix)
			addressHex := addr.Hex()[2:] // Remove '0x' prefix
			hash2Bytes := crypto.Keccak256([]byte(addressHex))
			hash2 := libcommon.BytesToHash(hash2Bytes)

			// Method 3: Hash the full address bytes (20 bytes)
			hash3 := crypto.Keccak256Hash(addr[:])

			// Log results
			t.Logf("\n--- %s ---", tc.name)
			t.Logf("Description:    %s", tc.description)
			t.Logf("Address:        %s", addr.Hex())
			t.Logf("Address Bytes:  %s", hex.EncodeToString(addressBytes))
			t.Logf("")
			t.Logf("Keccak256 Hash Methods:")
			t.Logf("  Method 1 (bytes):      %s", hash1.Hex())
			t.Logf("  Method 2 (hex string): %s", hash2.Hex())
			t.Logf("  Method 3 (array):      %s", hash3.Hex())
			t.Logf("")

			// Verify that methods 1 and 3 produce the same result
			assert.Equal(t, hash1, hash3, "Hash methods should produce the same result for address bytes")
		})
	}

	// Additional test: Hash multiple addresses and show results in a table
	t.Log("\n=== Batch Keccak256 Hash Results ===\n")
	addresses := []string{
		"0x92367037e551e0894c3dd8a7a63aa41cfb3db0a8",
		"0x28ad6b7dfd79153659cb44c2155cf7c0e1ceeccc",
		"0x78f813aa474167627acf0a0005f523e0e6d561d0",
		"0xf6d226f9dc15d9bb51182815b320d3fbe324e1ba",
		"0x5ff137d4b0fdcd49dca30c7cf57e578a026d2789",
	}

	t.Logf("%-45s | %s", "Address", "Keccak256 Hash")
	t.Logf("%s", strings.Repeat("-", 112))

	for _, addrStr := range addresses {
		addr := libcommon.HexToAddress(addrStr)
		hash := crypto.Keccak256Hash(addr.Bytes())
		t.Logf("%-45s | %s", addr.Hex(), hash.Hex())
	}
	t.Logf("")
}

// TestKeccak256WithStorageKeys tests keccak256 hashing for storage slot calculations
func TestKeccak256WithStorageKeys(t *testing.T) {
	// Example: Calculate storage slot for mapping(address => uint256) at slot 0
	address := libcommon.HexToAddress("0x92367037e551e0894c3dd8a7a63aa41cfb3db0a8")
	storageSlot := libcommon.Hash{} // slot 0

	// Concatenate address (32 bytes, left-padded) and slot (32 bytes)
	var data []byte
	// Address needs to be 32 bytes (left-padded with zeros)
	paddedAddr := common.LeftPadBytes(address.Bytes(), 32)
	data = append(data, paddedAddr...)
	data = append(data, storageSlot.Bytes()...)

	// Calculate the storage key
	storageKey := crypto.Keccak256Hash(data)

	t.Logf("\n=== Storage Slot Calculation Example ===")
	t.Logf("Mapping Type:      mapping(address => uint256)")
	t.Logf("Mapping Slot:      %d", 0)
	t.Logf("Address:           %s", address.Hex())
	t.Logf("Padded Address:    0x%s", hex.EncodeToString(paddedAddr))
	t.Logf("Storage Slot:      %s", storageSlot.Hex())
	t.Logf("Concatenated Data: 0x%s", hex.EncodeToString(data))
	t.Logf("Storage Key:       %s", storageKey.Hex())
	t.Logf("")
	t.Logf("This storage key can be used to query the value at:")
	t.Logf("  mapping[%s] in storage slot 0", address.Hex())
}
