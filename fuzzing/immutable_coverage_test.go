package fuzzing

import (
	"testing"

	"github.com/crytic/medusa-geth/common"
	compilationTypes "github.com/crytic/medusa/compilation/types"
	"github.com/crytic/medusa/fuzzing/contracts"
	"github.com/stretchr/testify/require"
)

func TestLinkKnownContractBytecodesUpdatesImmutableDefinitionAndArtifact(t *testing.T) {
	const placeholder = "1234567890123456789012345678901234"
	rawRuntime := []byte("aa__$" + placeholder + "$__0000bb")
	compiledContract := &compilationTypes.CompiledContract{
		InitBytecode:                       rawRuntime,
		RuntimeBytecode:                    rawRuntime,
		RuntimeBytecodeImmutableReferences: []compilationTypes.ImmutableReference{{Start: 21, Length: 2}},
		LibraryPlaceholders:                map[string]any{placeholder: "CoverageLibrary"},
	}
	compilation := compilationTypes.NewCompilation()
	compilation.SourcePathToArtifact["Child.sol"] = compilationTypes.SourceArtifact{
		Contracts: map[string]compilationTypes.CompiledContract{"Child": *compiledContract},
	}
	definition := contracts.NewContract("Child", "Child.sol", compiledContract, compilation)
	fuzzer := &Fuzzer{
		contractDefinitions: contracts.Contracts{definition},
		deployedLibraries: map[string]common.Address{
			"CoverageLibrary": common.HexToAddress("0x1234"),
		},
	}

	fuzzer.linkKnownContractBytecodes()

	require.Empty(t, compiledContract.LibraryPlaceholders)
	_, err := compilationTypes.ValidateImmutableReferences(
		compiledContract.RuntimeBytecode,
		compiledContract.RuntimeBytecodeImmutableReferences,
	)
	require.NoError(t, err)
	artifactContract := compilation.SourcePathToArtifact["Child.sol"].Contracts["Child"]
	require.Equal(t, *compiledContract, artifactContract)
}
