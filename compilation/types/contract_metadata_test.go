package types

import (
	"encoding/binary"
	"testing"

	"github.com/fxamacker/cbor"
	"github.com/stretchr/testify/require"
)

func TestExtractContractMetadataRequiresExactLengthDelimitedSuffix(t *testing.T) {
	metadataBytes, err := cbor.Marshal(ContractMetadata{
		"ipfs": []byte{0x12, 0x20, 0xaa},
		"solc": []byte{0x00, 0x08, 0x1c},
	}, cbor.EncOptions{Sort: cbor.SortCanonical})
	require.NoError(t, err)

	bytecode := appendMetadataSuffix([]byte{0x60, 0x00}, metadataBytes)
	metadata := ExtractContractMetadata(bytecode)
	require.NotNil(t, metadata)
	require.Equal(t, []byte{0x12, 0x20, 0xaa}, metadata.ExtractBytecodeHash())
}

func TestExtractContractMetadataRejectsInvalidFooter(t *testing.T) {
	require.Nil(t, ExtractContractMetadata(nil))
	require.Nil(t, ExtractContractMetadata([]byte{0x00, 0x00}))
	require.Nil(t, ExtractContractMetadata([]byte{0xa1, 0x00, 0x00, 0x04}))
	require.Nil(t, ExtractContractMetadata([]byte{0xff, 0x00, 0x01}))
}

func TestExtractContractMetadataRejectsTrailingCBORItem(t *testing.T) {
	first, err := cbor.Marshal(
		ContractMetadata{"solc": []byte{0x00, 0x08, 0x1c}},
		cbor.EncOptions{Sort: cbor.SortCanonical},
	)
	require.NoError(t, err)
	second, err := cbor.Marshal(
		ContractMetadata{"ipfs": []byte{0xaa}},
		cbor.EncOptions{Sort: cbor.SortCanonical},
	)
	require.NoError(t, err)

	metadataBytes := append(append([]byte(nil), first...), second...)
	require.Nil(t, ExtractContractMetadata(appendMetadataSuffix([]byte{0x60, 0x00}, metadataBytes)))
}

func appendMetadataSuffix(bytecode []byte, metadata []byte) []byte {
	result := append(append([]byte(nil), bytecode...), metadata...)
	var footer [2]byte
	binary.BigEndian.PutUint16(footer[:], uint16(len(metadata)))
	return append(result, footer[:]...)
}
