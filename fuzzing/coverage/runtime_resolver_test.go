package coverage

import (
	"encoding/binary"
	"os"
	"os/exec"
	"testing"

	"github.com/crytic/medusa-geth/common"
	compilationTypes "github.com/crytic/medusa/compilation/types"
	"github.com/crytic/medusa/fuzzing/contracts"
	"github.com/fxamacker/cbor"
	"github.com/stretchr/testify/require"
)

func TestImmutableRuntimeResolverCanonicalizesDeclaredRanges(t *testing.T) {
	template := []byte{0xaa, 0x00, 0x00, 0xbb}
	references := []compilationTypes.ImmutableReference{{Start: 1, Length: 2}}
	compiledContract := &compilationTypes.CompiledContract{
		RuntimeBytecode:                    template,
		RuntimeBytecodeImmutableReferences: references,
	}
	resolver, err := NewRuntimeBytecodeResolver(contracts.Contracts{
		contracts.NewContract("Immutable", "Immutable.sol", compiledContract, nil),
	})
	require.NoError(t, err)

	firstHash := runtimeCoverageMapHash([]byte{0xaa, 0x01, 0x02, 0xbb}, resolver)
	secondHash := runtimeCoverageMapHash([]byte{0xaa, 0x03, 0x04, 0xbb}, resolver)
	compiledHash, err := getCompiledContractCoverageMapHash(compiledContract, false)
	require.NoError(t, err)

	require.Equal(t, compiledHash, firstHash)
	require.Equal(t, firstHash, secondHash)

	coverageMaps := NewCoverageMaps()
	_, err = coverageMaps.UpdateAt(common.HexToAddress("0x1"), firstHash, 7)
	require.NoError(t, err)
	coverageMap, err := coverageMaps.GetCompiledContractCoverageMap(compiledContract, false)
	require.NoError(t, err)
	require.NotNil(t, coverageMap)
	require.Equal(t, uint64(1), coverageMap.HitCount(7))
}

func TestImmutableRuntimeResolverRejectsDifferenceOutsideRanges(t *testing.T) {
	template := []byte{0xaa, 0x00, 0x00, 0xbb}
	compiledContract := &compilationTypes.CompiledContract{
		RuntimeBytecode:                    template,
		RuntimeBytecodeImmutableReferences: []compilationTypes.ImmutableReference{{Start: 1, Length: 2}},
	}
	resolver, err := NewRuntimeBytecodeResolver(contracts.Contracts{
		contracts.NewContract("Immutable", "Immutable.sol", compiledContract, nil),
	})
	require.NoError(t, err)

	runtimeBytecode := []byte{0xab, 0x01, 0x02, 0xbb}
	require.Equal(t, getContractCoverageMapHash(runtimeBytecode, false), runtimeCoverageMapHash(runtimeBytecode, resolver))
}

func TestOrdinaryRuntimeUsesOrdinaryIdentity(t *testing.T) {
	runtimeBytecode := []byte{0xaa, 0x01, 0x02, 0xbb}
	resolver, err := NewRuntimeBytecodeResolver(contracts.Contracts{
		contracts.NewContract("Ordinary", "Ordinary.sol", &compilationTypes.CompiledContract{RuntimeBytecode: runtimeBytecode}, nil),
	})
	require.NoError(t, err)
	require.Equal(t, getContractCoverageMapHash(runtimeBytecode, false), runtimeCoverageMapHash(runtimeBytecode, resolver))
}

func TestMetadataRuntimeUsesMetadataIdentity(t *testing.T) {
	ipfsHash := append([]byte{0x12, 0x20}, make([]byte, 32)...)
	metadata, err := cbor.Marshal(map[string]any{
		"ipfs": ipfsHash,
		"solc": []byte{0x00, 0x08, 0x1c},
	}, cbor.EncOptions{Sort: cbor.SortCanonical})
	require.NoError(t, err)
	metadataSuffix := appendRuntimeMetadataFooter(metadata)
	template := append([]byte{0xaa, 0x00, 0x00, 0xbb}, metadataSuffix...)
	runtimeBytecode := append([]byte{0xaa, 0x01, 0x02, 0xbb}, metadataSuffix...)
	compiledContract := &compilationTypes.CompiledContract{
		RuntimeBytecode:                    template,
		RuntimeBytecodeImmutableReferences: []compilationTypes.ImmutableReference{{Start: 1, Length: 2}},
	}
	resolver, err := NewRuntimeBytecodeResolver(contracts.Contracts{
		contracts.NewContract("Metadata", "Metadata.sol", compiledContract, nil),
	})
	require.NoError(t, err)

	expectedHash := domainSeparatedBytecodeHash(metadataRuntimeCoverageIdentityDomain, ipfsHash)
	require.Equal(t, expectedHash, getContractCoverageMapHash(runtimeBytecode, false))
	require.Equal(t, expectedHash, runtimeCoverageMapHash(runtimeBytecode, resolver))
	compiledHash, err := getCompiledContractCoverageMapHash(compiledContract, false)
	require.NoError(t, err)
	require.Equal(t, expectedHash, compiledHash)
}

func TestImmutableRuntimeResolverFullBytecodeMode(t *testing.T) {
	if os.Getenv("MEDUSA_TEST_FULL_BYTECODE") != "1" {
		cmd := exec.Command(os.Args[0], "-test.run=^TestImmutableRuntimeResolverFullBytecodeMode$")
		cmd.Env = append(os.Environ(), "MEDUSA_TEST_FULL_BYTECODE=1", "USE_FULL_BYTECODE=1")
		output, err := cmd.CombinedOutput()
		require.NoErrorf(t, err, "full-bytecode subprocess failed:\n%s", string(output))
		return
	}

	template := []byte{0xaa, 0x00, 0x00, 0xbb}
	compiledContract := &compilationTypes.CompiledContract{
		RuntimeBytecode:                    template,
		RuntimeBytecodeImmutableReferences: []compilationTypes.ImmutableReference{{Start: 1, Length: 2}},
	}
	resolver, err := NewRuntimeBytecodeResolver(contracts.Contracts{
		contracts.NewContract("Immutable", "Immutable.sol", compiledContract, nil),
	})
	require.NoError(t, err)

	firstHash := runtimeCoverageMapHash([]byte{0xaa, 0x01, 0x02, 0xbb}, resolver)
	secondHash := runtimeCoverageMapHash([]byte{0xaa, 0x03, 0x04, 0xbb}, resolver)
	compiledHash, err := getCompiledContractCoverageMapHash(compiledContract, false)
	require.NoError(t, err)
	require.Equal(t, compiledHash, firstHash)
	require.Equal(t, firstHash, secondHash)
}

func TestImmutableRuntimeResolverIsolatesOrdinaryTemplateCollision(t *testing.T) {
	immutableTemplate := []byte{0xaa, 0x00, 0x00, 0xbb}
	deployedRuntime := []byte{0xaa, 0x01, 0x02, 0xbb}
	immutableContract := &compilationTypes.CompiledContract{
		RuntimeBytecode:                    immutableTemplate,
		RuntimeBytecodeImmutableReferences: []compilationTypes.ImmutableReference{{Start: 1, Length: 2}},
	}
	ordinaryContract := &compilationTypes.CompiledContract{RuntimeBytecode: deployedRuntime}
	resolver, err := NewRuntimeBytecodeResolver(contracts.Contracts{
		contracts.NewContract("Immutable", "Immutable.sol", immutableContract, nil),
		contracts.NewContract("Ordinary", "Ordinary.sol", ordinaryContract, nil),
	})
	require.NoError(t, err)

	resolvedHash := runtimeCoverageMapHash(deployedRuntime, resolver)
	immutableHash, err := getCompiledContractCoverageMapHash(immutableContract, false)
	require.NoError(t, err)
	require.NotEqual(t, immutableHash, resolvedHash)
	require.NotEqual(t, getContractCoverageMapHash(deployedRuntime, false), resolvedHash)
}

func TestImmutableRuntimeResolverIsolatesAmbiguousMasks(t *testing.T) {
	first := &compilationTypes.CompiledContract{
		RuntimeBytecode:                    []byte{0x00, 0x00, 0x03},
		RuntimeBytecodeImmutableReferences: []compilationTypes.ImmutableReference{{Start: 0, Length: 2}},
	}
	second := &compilationTypes.CompiledContract{
		RuntimeBytecode:                    []byte{0x01, 0x00, 0x00},
		RuntimeBytecodeImmutableReferences: []compilationTypes.ImmutableReference{{Start: 1, Length: 2}},
	}
	resolver, err := NewRuntimeBytecodeResolver(contracts.Contracts{
		contracts.NewContract("First", "First.sol", first, nil),
		contracts.NewContract("Second", "Second.sol", second, nil),
	})
	require.NoError(t, err)

	runtimeBytecode := []byte{0x01, 0x02, 0x03}
	resolvedHash := runtimeCoverageMapHash(runtimeBytecode, resolver)
	firstHash, err := getCompiledContractCoverageMapHash(first, false)
	require.NoError(t, err)
	secondHash, err := getCompiledContractCoverageMapHash(second, false)
	require.NoError(t, err)
	require.NotEqual(t, firstHash, resolvedHash)
	require.NotEqual(t, secondHash, resolvedHash)
}

func TestImmutableRuntimeResolverIsolatesDistinctOwnersOfOneDescriptor(t *testing.T) {
	template := append([]byte{0x7f}, make([]byte, 32)...)
	template = append(template, 0x50)
	references := []compilationTypes.ImmutableReference{{Start: 1, Length: 32}}
	first := &compilationTypes.CompiledContract{
		RuntimeBytecode:                    template,
		RuntimeBytecodeImmutableReferences: references,
		SrcMapsRuntime:                     "0:1:0",
	}
	second := &compilationTypes.CompiledContract{
		RuntimeBytecode:                    append([]byte(nil), template...),
		RuntimeBytecodeImmutableReferences: references,
		SrcMapsRuntime:                     "0:1:1",
	}
	firstDefinition := contracts.NewContract("Same", "A.sol", first, nil)
	secondDefinition := contracts.NewContract("Same", "B.sol", second, nil)
	runtimeBytecode := append([]byte(nil), template...)
	runtimeBytecode[32] = 7

	resolver, err := NewRuntimeBytecodeResolver(contracts.Contracts{firstDefinition, secondDefinition})
	require.NoError(t, err)
	reversedResolver, err := NewRuntimeBytecodeResolver(contracts.Contracts{secondDefinition, firstDefinition})
	require.NoError(t, err)

	expected := domainSeparatedBytecodeHash(ambiguousRuntimeCoverageIdentityDomain, runtimeBytecode)
	canonical := immutableRuntimeCoverageMapHash(template, references)
	require.NotEqual(t, canonical, expected)
	require.Equal(t, expected, runtimeCoverageMapHash(runtimeBytecode, resolver))
	require.Equal(t, expected, runtimeCoverageMapHash(runtimeBytecode, reversedResolver))
}

func TestImmutableRuntimeResolverCoalescesRepeatedOwner(t *testing.T) {
	template := []byte{0xaa, 0x00, 0x00, 0xbb}
	references := []compilationTypes.ImmutableReference{{Start: 1, Length: 2}}
	compiledContract := &compilationTypes.CompiledContract{
		RuntimeBytecode:                    template,
		RuntimeBytecodeImmutableReferences: references,
	}
	definition := contracts.NewContract("Immutable", "Immutable.sol", compiledContract, nil)
	resolver, err := NewRuntimeBytecodeResolver(contracts.Contracts{definition, definition})
	require.NoError(t, err)

	runtimeBytecode := []byte{0xaa, 0x01, 0x02, 0xbb}
	require.Equal(t, immutableRuntimeCoverageMapHash(template, references), runtimeCoverageMapHash(runtimeBytecode, resolver))
}

func TestImmutableBytesCannotImpersonateRuntimeMetadata(t *testing.T) {
	template := append([]byte{0x7f}, make([]byte, 32)...)
	template = append(template, []byte{0x22, 0x22, 0x22, 0x22, 0x22, 0x22, 0x22, 0x22, 0x22, 0xff, 0xff}...)
	references := []compilationTypes.ImmutableReference{{Start: 1, Length: 32}}
	compiledContract := &compilationTypes.CompiledContract{
		RuntimeBytecode:                    template,
		RuntimeBytecodeImmutableReferences: references,
	}
	resolver, err := NewRuntimeBytecodeResolver(contracts.Contracts{
		contracts.NewContract("Immutable", "Immutable.sol", compiledContract, nil),
	})
	require.NoError(t, err)

	runtimeBytecode := append([]byte(nil), template...)
	fakeMetadataPrefix := []byte{0xa1, 0x65, 'b', 'z', 'z', 'r', '0', 0x58, 0x20}
	copy(runtimeBytecode[1:33], append(fakeMetadataPrefix, make([]byte, 23)...))
	_, hasMetadataIdentity := runtimeMetadataCoverageMapHash(runtimeBytecode)
	require.False(t, hasMetadataIdentity)
	require.Equal(
		t,
		immutableRuntimeCoverageMapHash(template, references),
		runtimeCoverageMapHash(runtimeBytecode, resolver),
	)
}

func TestImmutableRuntimeResolverRejectsMalformedReferences(t *testing.T) {
	_, err := NewRuntimeBytecodeResolver(contracts.Contracts{
		contracts.NewContract("Malformed", "Malformed.sol", &compilationTypes.CompiledContract{
			RuntimeBytecode:                    []byte{0xaa, 0x00},
			RuntimeBytecodeImmutableReferences: []compilationTypes.ImmutableReference{{Start: 1, Length: 2}},
		}, nil),
	})
	require.ErrorContains(t, err, "invalid runtime immutable references")
}

func TestImmutableRuntimeResolverSkipsUnresolvedLibraryTemplate(t *testing.T) {
	compiledContract := &compilationTypes.CompiledContract{
		RuntimeBytecode:                    []byte("aa__$1234567890123456789012345678901234$__00"),
		RuntimeBytecodeImmutableReferences: []compilationTypes.ImmutableReference{{Start: 1, Length: 2}},
		LibraryPlaceholders:                map[string]any{"1234567890123456789012345678901234": nil},
	}
	resolver, err := NewRuntimeBytecodeResolver(contracts.Contracts{
		contracts.NewContract("Unlinked", "Unlinked.sol", compiledContract, nil),
	})
	require.NoError(t, err)
	require.NotNil(t, resolver)

	_, err = getCompiledContractCoverageMapHash(compiledContract, false)
	require.NoError(t, err)
}

func TestImmutableRuntimeResolverMatchesAfterLibraryLinking(t *testing.T) {
	const placeholder = "1234567890123456789012345678901234"
	compiledContract := &compilationTypes.CompiledContract{
		InitBytecode:                       []byte("aa__$" + placeholder + "$__0000bb"),
		RuntimeBytecode:                    []byte("aa__$" + placeholder + "$__0000bb"),
		RuntimeBytecodeImmutableReferences: []compilationTypes.ImmutableReference{{Start: 21, Length: 2}},
		LibraryPlaceholders:                map[string]any{placeholder: "CoverageLibrary"},
	}
	compiledContract.LinkBytecodes("LinkedImmutable", map[string]common.Address{
		"CoverageLibrary": common.HexToAddress("0x1234"),
	})
	resolver, err := NewRuntimeBytecodeResolver(contracts.Contracts{
		contracts.NewContract("LinkedImmutable", "LinkedImmutable.sol", compiledContract, nil),
	})
	require.NoError(t, err)

	deployedRuntime := append([]byte(nil), compiledContract.RuntimeBytecode...)
	deployedRuntime[21] = 0x01
	deployedRuntime[22] = 0x02
	compiledHash, err := getCompiledContractCoverageMapHash(compiledContract, false)
	require.NoError(t, err)
	require.Equal(t, compiledHash, runtimeCoverageMapHash(deployedRuntime, resolver))
}

func TestImmutableRuntimeIdentityIncludesLinkedLibraryAddress(t *testing.T) {
	const placeholder = "1234567890123456789012345678901234"
	newLinkedContract := func(address string) *compilationTypes.CompiledContract {
		contract := &compilationTypes.CompiledContract{
			InitBytecode:                       []byte("aa__$" + placeholder + "$__0000bb"),
			RuntimeBytecode:                    []byte("aa__$" + placeholder + "$__0000bb"),
			RuntimeBytecodeImmutableReferences: []compilationTypes.ImmutableReference{{Start: 21, Length: 2}},
			LibraryPlaceholders:                map[string]any{placeholder: "CoverageLibrary"},
		}
		contract.LinkBytecodes("LinkedImmutable", map[string]common.Address{
			"CoverageLibrary": common.HexToAddress(address),
		})
		return contract
	}
	first := newLinkedContract("0x1234")
	second := newLinkedContract("0x5678")
	resolver, err := NewRuntimeBytecodeResolver(contracts.Contracts{
		contracts.NewContract("First", "First.sol", first, nil),
		contracts.NewContract("Second", "Second.sol", second, nil),
	})
	require.NoError(t, err)

	firstRuntime := append([]byte(nil), first.RuntimeBytecode...)
	firstRuntime[21] = 1
	secondRuntime := append([]byte(nil), second.RuntimeBytecode...)
	secondRuntime[21] = 1
	firstHash := runtimeCoverageMapHash(firstRuntime, resolver)
	secondHash := runtimeCoverageMapHash(secondRuntime, resolver)
	require.NotEqual(t, firstHash, secondHash)
	firstCompiledHash, err := getCompiledContractCoverageMapHash(first, false)
	require.NoError(t, err)
	secondCompiledHash, err := getCompiledContractCoverageMapHash(second, false)
	require.NoError(t, err)
	require.Equal(t, firstCompiledHash, firstHash)
	require.Equal(t, secondCompiledHash, secondHash)
}

func TestImmutableIdentityCannotReuseOrdinaryPreimage(t *testing.T) {
	template := []byte{0xaa, 0x00, 0x00, 0xbb}
	references := []compilationTypes.ImmutableReference{{Start: 1, Length: 2}}
	immutableHash := immutableRuntimeCoverageMapHash(template, references)
	ordinaryPreimage := immutableRuntimeCoverageMapPayload(template, references)
	ordinaryHash := getContractCoverageMapHash(ordinaryPreimage, false)
	require.NotEqual(t, immutableHash, ordinaryHash)
}

func appendRuntimeMetadataFooter(metadata []byte) []byte {
	result := append([]byte(nil), metadata...)
	var footer [2]byte
	binary.BigEndian.PutUint16(footer[:], uint16(len(metadata)))
	return append(result, footer[:]...)
}
