package platforms

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/crytic/medusa/compilation/types"
	"github.com/stretchr/testify/require"
)

func TestEnrichFoundryImmutableReferencesRejectsOutOfRangeCompilerData(t *testing.T) {
	projectRoot := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(projectRoot, "foundry.toml"), []byte("[profile.default]\n"), 0o600))
	artifactDirectory := filepath.Join(projectRoot, "out", "Target.sol")
	require.NoError(t, os.MkdirAll(artifactDirectory, 0o700))

	artifact := testFoundryArtifact("0xaa0000bb", map[string][]foundryArtifactImmutableReference{
		"1": {{Start: uint64Pointer(3), Length: uint64Pointer(2)}},
	})
	artifactData, err := json.Marshal(artifact)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(artifactDirectory, "Target.json"), artifactData, 0o600))

	compilations := []types.Compilation{{
		SourcePathToArtifact: map[string]types.SourceArtifact{
			"Target.sol": {
				Contracts: map[string]types.CompiledContract{
					"Target": {RuntimeBytecode: []byte{0xaa, 0x00, 0x00, 0xbb}},
				},
			},
		},
	}}
	err = enrichFoundryImmutableReferences(
		compilations,
		projectRoot,
		[]string{"--foundry-out-directory", "out"},
	)
	require.ErrorContains(t, err, "invalid immutable references")
}

func TestEnrichFoundryImmutableReferencesUsesExactRuntimeTemplate(t *testing.T) {
	projectRoot := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(projectRoot, "foundry.toml"), []byte("[profile.default]\n"), 0o600))
	artifactDirectory := filepath.Join(projectRoot, "out", "Target.sol")
	require.NoError(t, os.MkdirAll(artifactDirectory, 0o700))

	artifact := testFoundryArtifact("0xaa0000bb", map[string][]foundryArtifactImmutableReference{
		"1": {{Start: uint64Pointer(1), Length: uint64Pointer(2)}},
	})
	artifactData, err := json.Marshal(artifact)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(artifactDirectory, "Target.json"), artifactData, 0o600))

	compilations := []types.Compilation{{
		SourcePathToArtifact: map[string]types.SourceArtifact{
			"Target.sol": {
				Contracts: map[string]types.CompiledContract{
					"Target":   {RuntimeBytecode: []byte{0xaa, 0x00, 0x00, 0xbb}},
					"Ordinary": {RuntimeBytecode: []byte{0xaa, 0x00, 0x00, 0xbb}},
				},
			},
		},
	}}
	require.NoError(t, enrichFoundryImmutableReferences(
		compilations,
		projectRoot,
		[]string{"--foundry-out-directory=out"},
	))

	contracts := compilations[0].SourcePathToArtifact["Target.sol"].Contracts
	require.Equal(t, []types.ImmutableReference{{Start: 1, Length: 2}}, contracts["Target"].RuntimeBytecodeImmutableReferences)
	require.Empty(t, contracts["Ordinary"].RuntimeBytecodeImmutableReferences)
}

func TestEnrichFoundryImmutableReferencesRequiresStartAndLength(t *testing.T) {
	projectRoot := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(projectRoot, "foundry.toml"), []byte("[profile.default]\n"), 0o600))
	artifactDirectory := filepath.Join(projectRoot, "out", "Target.sol")
	require.NoError(t, os.MkdirAll(artifactDirectory, 0o700))

	artifact := testFoundryArtifact("0xaa0000bb", map[string][]foundryArtifactImmutableReference{
		"1": {{Length: uint64Pointer(2)}},
	})
	artifactData, err := json.Marshal(artifact)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(artifactDirectory, "Target.json"), artifactData, 0o600))

	compilations := []types.Compilation{{
		SourcePathToArtifact: map[string]types.SourceArtifact{
			"Target.sol": {
				Contracts: map[string]types.CompiledContract{
					"Target": {RuntimeBytecode: []byte{0xaa, 0x00, 0x00, 0xbb}},
				},
			},
		},
	}}
	err = enrichFoundryImmutableReferences(compilations, projectRoot, []string{"--foundry-out-directory", "out"})
	require.ErrorContains(t, err, "must contain start and length")
}

func TestResolveFoundryProjectRootHonorsForcedFramework(t *testing.T) {
	projectRoot := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(projectRoot, "foundry.toml"), []byte("[profile.default]\n"), 0o600))

	_, isFoundry, err := resolveFoundryProjectRoot(projectRoot, []string{"--compile-force-framework", "solc"})
	require.NoError(t, err)
	require.False(t, isFoundry)

	resolvedRoot, isFoundry, err := resolveFoundryProjectRoot(projectRoot, []string{"--compile-force-framework=Foundry"})
	require.NoError(t, err)
	require.True(t, isFoundry)
	require.Equal(t, projectRoot, resolvedRoot)

	_, isFoundry, err = resolveFoundryProjectRoot("mainnet:0x0000000000000000000000000000000000000000", nil)
	require.NoError(t, err)
	require.False(t, isFoundry)
}

func TestResolveFoundryOutDirectoryIgnoreCompileUsesDefault(t *testing.T) {
	projectRoot := t.TempDir()
	outDirectory := filepath.Join(projectRoot, "out")
	require.NoError(t, os.Mkdir(outDirectory, 0o700))
	require.NoError(t, os.WriteFile(
		filepath.Join(projectRoot, "foundry.toml"),
		[]byte("[profile.default]\nout = 'custom-out'\n"),
		0o600,
	))

	resolved, err := resolveFoundryOutDirectory(projectRoot, []string{"--ignore-compile"})
	require.NoError(t, err)
	expected, err := filepath.EvalSymlinks(outDirectory)
	require.NoError(t, err)
	require.Equal(t, filepath.Clean(expected), resolved)
}

func testFoundryArtifact(
	object string,
	references map[string][]foundryArtifactImmutableReference,
) foundryArtifact {
	artifact := foundryArtifact{
		DeployedBytecode: foundryArtifactBytecode{
			Object:              object,
			ImmutableReferences: references,
		},
	}
	artifact.Metadata.Settings.CompilationTarget = map[string]string{"Target.sol": "Target"}
	return artifact
}

func uint64Pointer(value uint64) *uint64 {
	return &value
}
