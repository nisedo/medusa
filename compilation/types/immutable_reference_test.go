package types

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNormalizeImmutableReferences(t *testing.T) {
	t.Run("sorts valid references and permits adjacency", func(t *testing.T) {
		references, err := NormalizeImmutableReferences([]ImmutableReference{
			{Start: 4, Length: 2},
			{Start: 1, Length: 3},
		}, 8)
		require.NoError(t, err)
		require.Equal(t, []ImmutableReference{{Start: 1, Length: 3}, {Start: 4, Length: 2}}, references)
	})

	testCases := []struct {
		name       string
		references []ImmutableReference
		codeLength int
	}{
		{name: "zero length", references: []ImmutableReference{{Start: 1, Length: 0}}, codeLength: 3},
		{name: "duplicate", references: []ImmutableReference{{Start: 1, Length: 1}, {Start: 1, Length: 1}}, codeLength: 3},
		{name: "overlap", references: []ImmutableReference{{Start: 1, Length: 2}, {Start: 2, Length: 1}}, codeLength: 4},
		{name: "start at end", references: []ImmutableReference{{Start: 3, Length: 1}}, codeLength: 3},
		{name: "length out of range", references: []ImmutableReference{{Start: 2, Length: 2}}, codeLength: 3},
		{name: "overflow sized", references: []ImmutableReference{{Start: math.MaxUint64, Length: 2}}, codeLength: 3},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := NormalizeImmutableReferences(testCase.references, testCase.codeLength)
			require.Error(t, err)
		})
	}
}

func TestValidateImmutableReferencesRejectsNonzeroPlaceholder(t *testing.T) {
	_, err := ValidateImmutableReferences(
		[]byte{0xaa, 0x00, 0x01, 0xbb},
		[]ImmutableReference{{Start: 1, Length: 2}},
	)
	require.ErrorContains(t, err, "nonzero compiler placeholder")
}

func TestRuntimeBytecodeMatchesImmutableTemplate(t *testing.T) {
	template := []byte{0xaa, 0x00, 0x00, 0xbb}
	references, err := ValidateImmutableReferences(template, []ImmutableReference{{Start: 1, Length: 2}})
	require.NoError(t, err)

	require.True(t, RuntimeBytecodeMatchesImmutableTemplate(template, []byte{0xaa, 0x01, 0x02, 0xbb}, references))
	require.True(t, RuntimeBytecodeMatchesImmutableTemplate(template, []byte{0xaa, 0x03, 0x04, 0xbb}, references))
	require.False(t, RuntimeBytecodeMatchesImmutableTemplate(template, []byte{0xab, 0x01, 0x02, 0xbb}, references))
	require.False(t, RuntimeBytecodeMatchesImmutableTemplate(template, []byte{0xaa, 0x01, 0x02, 0xbc}, references))
	require.False(t, RuntimeBytecodeMatchesImmutableTemplate(template, []byte{0xaa, 0x01}, references))
	require.False(t, RuntimeBytecodeMatchesImmutableTemplate(
		[]byte{0xaa, 0x01, 0x00, 0xbb},
		[]byte{0xaa, 0x02, 0x03, 0xbb},
		references,
	))
}
