package contracts

import (
	"testing"

	compilationTypes "github.com/crytic/medusa/compilation/types"
	"github.com/stretchr/testify/require"
)

func TestMatchBytecodeRejectsAmbiguousImmutableRuntime(t *testing.T) {
	first := NewContract("First", "First.sol", &compilationTypes.CompiledContract{
		InitBytecode:                       []byte{0xaa},
		RuntimeBytecode:                    []byte{0x00, 0x00, 0x03},
		RuntimeBytecodeImmutableReferences: []compilationTypes.ImmutableReference{{Start: 0, Length: 2}},
	}, nil)
	second := NewContract("Second", "Second.sol", &compilationTypes.CompiledContract{
		InitBytecode:                       []byte{0xbb},
		RuntimeBytecode:                    []byte{0x01, 0x00, 0x00},
		RuntimeBytecodeImmutableReferences: []compilationTypes.ImmutableReference{{Start: 1, Length: 2}},
	}, nil)
	definitions := Contracts{first, second}

	require.Nil(t, definitions.MatchBytecode(nil, []byte{0x01, 0x02, 0x03}))
	require.Same(t, first, definitions.MatchBytecode([]byte{0xaa, 0x99}, []byte{0x01, 0x02, 0x03}))
}

func TestMatchBytecodeUsesRuntimeToBreakInitTemplateTie(t *testing.T) {
	first := NewContract("First", "First.sol", &compilationTypes.CompiledContract{
		InitBytecode:    []byte{0xaa},
		RuntimeBytecode: []byte{0x01},
	}, nil)
	second := NewContract("Second", "Second.sol", &compilationTypes.CompiledContract{
		InitBytecode:    []byte{0xaa},
		RuntimeBytecode: []byte{0x02},
	}, nil)
	definitions := Contracts{first, second}

	require.Same(t, second, definitions.MatchBytecode([]byte{0xaa, 0x99}, []byte{0x02}))
	require.Nil(t, definitions.MatchBytecode([]byte{0xaa, 0x99}, nil))
}
