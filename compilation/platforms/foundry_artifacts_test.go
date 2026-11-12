package platforms

import (
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/crytic/medusa/compilation/types"
	"github.com/crytic/medusa/utils/testutils"
	"github.com/fxamacker/cbor"
	"github.com/stretchr/testify/require"
)

func TestEnrichFoundryImmutableReferencesReadsSelectedBuildInfoWithoutOut(t *testing.T) {
	projectRoot, buildInfoDirectory := testFoundryProject(t)
	writeFoundryBuildInfo(t, buildInfoDirectory, testFoundryCompilerOutput(
		"0xaa0000bb",
		map[string][]foundryCompilerReference{"1": {{Start: uint64Pointer(1), Length: uint64Pointer(2)}}},
		nil,
	))
	compilations := testFoundryCompilations(projectRoot, []byte{0xaa, 0x00, 0x00, 0xbb})

	require.NoError(t, enrichFoundryImmutableReferences(
		compilations,
		projectRoot,
		testFoundryArgs(projectRoot),
	))
	references := compilations[0].SourcePathToArtifact[filepath.Join(projectRoot, "Target.sol")].
		Contracts["Target"].RuntimeBytecodeImmutableReferences
	require.Equal(t, []types.ImmutableReference{{Start: 1, Length: 2}}, references)
	require.NoDirExists(t, filepath.Join(projectRoot, "out"))
}

func TestEnrichFoundryImmutableReferencesRejectsOutOfRangeCompilerData(t *testing.T) {
	projectRoot, buildInfoDirectory := testFoundryProject(t)
	writeFoundryBuildInfo(t, buildInfoDirectory, testFoundryCompilerOutput(
		"0xaa0000bb",
		map[string][]foundryCompilerReference{"1": {{Start: uint64Pointer(3), Length: uint64Pointer(2)}}},
		nil,
	))
	compilations := testFoundryCompilations(projectRoot, []byte{0xaa, 0x00, 0x00, 0xbb})

	err := enrichFoundryImmutableReferences(compilations, projectRoot, testFoundryArgs(projectRoot))
	require.ErrorContains(t, err, "invalid immutable references")
}

func TestEnrichFoundryImmutableReferencesRequiresStartAndLength(t *testing.T) {
	projectRoot, buildInfoDirectory := testFoundryProject(t)
	writeFoundryBuildInfo(t, buildInfoDirectory, testFoundryCompilerOutput(
		"0xaa0000bb",
		map[string][]foundryCompilerReference{"1": {{Length: uint64Pointer(2)}}},
		nil,
	))
	compilations := testFoundryCompilations(projectRoot, []byte{0xaa, 0x00, 0x00, 0xbb})

	err := enrichFoundryImmutableReferences(compilations, projectRoot, testFoundryArgs(projectRoot))
	require.ErrorContains(t, err, "must contain start and length")
}

func TestEnrichFoundryImmutableReferencesMatchesCryticMetadataRemoval(t *testing.T) {
	projectRoot, buildInfoDirectory := testFoundryProject(t)
	objectWithoutMetadata := "aa0000bb"
	writeFoundryBuildInfo(t, buildInfoDirectory, testFoundryCompilerOutput(
		objectWithoutMetadata+testMetadataSuffixHex(t),
		map[string][]foundryCompilerReference{"1": {{Start: uint64Pointer(1), Length: uint64Pointer(2)}}},
		nil,
	))
	compiledRuntime, err := hex.DecodeString(objectWithoutMetadata)
	require.NoError(t, err)
	compilations := testFoundryCompilations(projectRoot, compiledRuntime)
	args := append(testFoundryArgs(projectRoot), "--compile-remove-metadata")

	require.NoError(t, enrichFoundryImmutableReferences(compilations, projectRoot, args))
	require.Equal(
		t,
		[]types.ImmutableReference{{Start: 1, Length: 2}},
		compilations[0].SourcePathToArtifact[filepath.Join(projectRoot, "Target.sol")].
			Contracts["Target"].RuntimeBytecodeImmutableReferences,
	)
}

func TestCryticCompilationConfigFoundryMetadataRemovalRetainsImmutableReferences(t *testing.T) {
	projectRoot := t.TempDir()
	sourceDirectory := filepath.Join(projectRoot, "src")
	require.NoError(t, os.Mkdir(sourceDirectory, 0o700))
	_, testFile, _, ok := runtime.Caller(0)
	require.True(t, ok)
	source, err := os.ReadFile(filepath.Join(
		filepath.Dir(testFile),
		"..", "..", "fuzzing", "testdata", "contracts", "coverage", "immutable_runtime.sol",
	))
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(sourceDirectory, "ImmutableCoverage.sol"), source, 0o600))
	require.NoError(t, os.WriteFile(
		filepath.Join(projectRoot, "foundry.toml"),
		[]byte("[profile.default]\nsrc = 'src'\nout = 'out'\nbytecode_hash = 'none'\n"),
		0o600,
	))
	testutils.WriteSolidityBuildInfo(
		t,
		filepath.Join(projectRoot, "out", "build-info"),
		"src/ImmutableCoverage.sol",
		source,
		"0.8.28",
		"none",
	)

	compiler := NewCryticCompilationConfig(projectRoot)
	compiler.ExportDirectory = filepath.Join(projectRoot, "crytic-export")
	compiler.Args = []string{
		"--config-file", filepath.Join(projectRoot, "missing-crytic-options.json"),
		"--compile-force-framework", "foundry",
		"--compile-remove-metadata",
		"--foundry-ignore-compile",
		"--foundry-build-info-directory", "out/build-info",
	}
	compilations, _, err := compiler.Compile()
	require.NoError(t, err)

	found := false
	for _, compilation := range compilations {
		for _, sourceArtifact := range compilation.SourcePathToArtifact {
			contract, exists := sourceArtifact.Contracts["ImmutableCoverage"]
			if !exists {
				continue
			}
			found = true
			require.NotEmpty(t, contract.RuntimeBytecodeImmutableReferences)
			require.Nil(t, types.ExtractContractMetadata(contract.RuntimeBytecode))
		}
	}
	require.True(t, found)
}

func TestEnrichFoundryImmutableReferencesMatchesDeclaredLibraryLinkingOnly(t *testing.T) {
	projectRoot, buildInfoDirectory := testFoundryProject(t)
	const placeholder = "__$1234567890123456789012345678901234$__"
	object := "aa" + placeholder + "0000bb"
	links := map[string]map[string][]foundryCompilerReference{
		"Library.sol": {
			"Library": {{Start: uint64Pointer(1), Length: uint64Pointer(20)}},
		},
	}
	writeFoundryBuildInfo(t, buildInfoDirectory, testFoundryCompilerOutput(
		object,
		map[string][]foundryCompilerReference{"1": {{Start: uint64Pointer(21), Length: uint64Pointer(2)}}},
		links,
	))
	linkedRuntime, err := hex.DecodeString("aa" + "0000000000000000000000000000000000001234" + "0000bb")
	require.NoError(t, err)
	compilations := testFoundryCompilations(projectRoot, linkedRuntime)

	args := append(testFoundryArgs(projectRoot), "--compile-autolink")
	require.NoError(t, enrichFoundryImmutableReferences(compilations, projectRoot, args))

	changedOutsideLink := append([]byte(nil), linkedRuntime...)
	changedOutsideLink[len(changedOutsideLink)-1] = 0xbc
	compilations = testFoundryCompilations(projectRoot, changedOutsideLink)
	err = enrichFoundryImmutableReferences(compilations, projectRoot, args)
	require.ErrorContains(t, err, "does not match compiled contract")

	compilations = testFoundryCompilations(projectRoot, linkedRuntime)
	err = enrichFoundryImmutableReferences(compilations, projectRoot, testFoundryArgs(projectRoot))
	require.ErrorContains(t, err, "does not match compiled contract")
}

func TestEnrichFoundryImmutableReferencesRejectsInvalidLinkRanges(t *testing.T) {
	const placeholder = "__$1234567890123456789012345678901234$__"
	tests := []struct {
		name      string
		object    string
		immutable map[string][]foundryCompilerReference
		links     map[string]map[string][]foundryCompilerReference
		errorText string
	}{
		{
			name:      "non-20-byte range",
			object:    "aa" + placeholder + "0000bb",
			immutable: testImmutableReferenceMap(21, 2),
			links:     testLinkReferenceMap(1, 19),
			errorText: "instead of 20",
		},
		{
			name:      "out-of-range",
			object:    "aa" + placeholder + "0000bb",
			immutable: testImmutableReferenceMap(21, 2),
			links:     testLinkReferenceMap(5, 20),
			errorText: "outside runtime bytecode",
		},
		{
			name:      "overlapping link ranges",
			object:    "aa" + placeholder + placeholder + "0000bb",
			immutable: testImmutableReferenceMap(41, 2),
			links: map[string]map[string][]foundryCompilerReference{
				"Library.sol": {
					"First":  {{Start: uint64Pointer(1), Length: uint64Pointer(20)}},
					"Second": {{Start: uint64Pointer(10), Length: uint64Pointer(20)}},
				},
			},
			errorText: "overlap",
		},
		{
			name:      "immutable overlap",
			object:    "aa" + placeholder + "0000bb",
			immutable: testImmutableReferenceMap(1, 2),
			links:     testLinkReferenceMap(1, 20),
			errorText: "overlaps library reference",
		},
		{
			name:      "undeclared placeholder",
			object:    "aa" + placeholder + "0000bb",
			immutable: testImmutableReferenceMap(21, 2),
			links:     nil,
			errorText: "undeclared linker placeholder",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			projectRoot, buildInfoDirectory := testFoundryProject(t)
			writeFoundryBuildInfo(t, buildInfoDirectory, testFoundryCompilerOutput(
				test.object,
				test.immutable,
				test.links,
			))
			compilations := testFoundryCompilations(projectRoot, []byte{0xaa})
			err := enrichFoundryImmutableReferences(compilations, projectRoot, testFoundryArgs(projectRoot))
			require.ErrorContains(t, err, test.errorText)
		})
	}
}

func TestReadFoundryCompilerOutputsSupportsSplitBuildInfo(t *testing.T) {
	buildInfoDirectory := t.TempDir()
	require.NoError(t, os.WriteFile(
		filepath.Join(buildInfoDirectory, "unit.json"),
		[]byte(`{"solcVersion":"0.8.28"}`),
		0o600,
	))
	output := testFoundryCompilerOutput("0xaa", nil, nil)
	companionData, err := json.Marshal(foundryBuildInfo{Output: &output})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(
		filepath.Join(buildInfoDirectory, "unit.output.json"),
		companionData,
		0o600,
	))

	outputs, err := readFoundryCompilerOutputs(buildInfoDirectory)
	require.NoError(t, err)
	require.Len(t, outputs, 1)
	require.Contains(t, outputs[0].output.Contracts, "Target.sol")
}

func TestResolveCryticEffectiveOptionsUsesConfigAndLastCLIOverride(t *testing.T) {
	workingDirectory := t.TempDir()
	configPath := filepath.Join(workingDirectory, "options.json")
	require.NoError(t, os.WriteFile(
		configPath,
		[]byte(`{
			"compile_force_framework":"solc",
			"compile_remove_metadata":true,
			"foundry_out_directory":"config-out",
			"foundry_build_info_directory":"config-build-info"
		}`),
		0o600,
	))
	options, err := resolveCryticEffectiveOptions([]string{
		"--config-file", configPath,
		"--compile-force-framework", "solc",
		"--compile-force-framework=foundry",
		"--foundry-out-directory", "cli-out",
		"--foundry-build-info-directory=cli-build-info",
	}, workingDirectory)
	require.NoError(t, err)
	require.Equal(t, "foundry", options.compileForceFramework)
	require.True(t, options.compileRemoveMetadata)
	require.Equal(t, "cli-out", options.foundryOutDirectory)
	require.Equal(t, "cli-build-info", options.foundryBuildInfoDirectory)
}

func TestResolveFoundryProjectRootHonorsConfigForcedFramework(t *testing.T) {
	projectRoot, _ := testFoundryProject(t)
	configPath := filepath.Join(projectRoot, "crytic-options.json")
	require.NoError(t, os.WriteFile(configPath, []byte(`{"compile_force_framework":"solc"}`), 0o600))
	options, err := resolveCryticEffectiveOptions([]string{"--config-file", configPath}, projectRoot)
	require.NoError(t, err)

	_, isFoundry, err := resolveFoundryProjectRoot(projectRoot, options)
	require.NoError(t, err)
	require.False(t, isFoundry)

	options.compileForceFramework = "Foundry"
	resolvedRoot, isFoundry, err := resolveFoundryProjectRoot(projectRoot, options)
	require.NoError(t, err)
	require.True(t, isFoundry)
	canonicalProjectRoot, err := filepath.EvalSymlinks(projectRoot)
	require.NoError(t, err)
	require.Equal(t, canonicalProjectRoot, resolvedRoot)
}

func TestResolveFoundryProjectRootFollowsTargetSymlink(t *testing.T) {
	projectRoot, _ := testFoundryProject(t)
	sourceDirectory := filepath.Join(projectRoot, "src")
	require.NoError(t, os.Mkdir(sourceDirectory, 0o700))
	targetPath := filepath.Join(sourceDirectory, "Target.sol")
	require.NoError(t, os.WriteFile(targetPath, []byte("contract Target {}\n"), 0o600))

	symlinkPath := filepath.Join(t.TempDir(), "Target.sol")
	if err := os.Symlink(targetPath, symlinkPath); err != nil {
		t.Skipf("symbolic links are unavailable: %v", err)
	}

	resolvedRoot, isFoundry, err := resolveFoundryProjectRoot(symlinkPath, cryticEffectiveOptions{})
	require.NoError(t, err)
	require.True(t, isFoundry)
	canonicalProjectRoot, err := filepath.EvalSymlinks(projectRoot)
	require.NoError(t, err)
	require.Equal(t, canonicalProjectRoot, resolvedRoot)
}

func TestStripCryticMetadataSuffixRejectsArbitraryTruncation(t *testing.T) {
	stripped, err := stripCryticMetadataSuffix("aa" + testMetadataSuffixHex(t))
	require.NoError(t, err)
	require.Equal(t, "aa", stripped)

	_, err = stripCryticMetadataSuffix("aa0001")
	require.ErrorContains(t, err, "not one exact CBOR map")
	_, err = stripCryticMetadataSuffix("aaffff")
	require.ErrorContains(t, err, "out of range")
}

func testFoundryProject(t *testing.T) (string, string) {
	t.Helper()
	projectRoot := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(projectRoot, "foundry.toml"), []byte("[profile.default]\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(projectRoot, "Target.sol"), []byte("contract Target {}\n"), 0o600))
	buildInfoDirectory := filepath.Join(projectRoot, "saved-build-info")
	require.NoError(t, os.Mkdir(buildInfoDirectory, 0o700))
	return projectRoot, buildInfoDirectory
}

func testFoundryArgs(projectRoot string) []string {
	return []string{
		"--config-file", filepath.Join(projectRoot, "missing-crytic-options.json"),
		"--ignore-compile",
		"--foundry-build-info-directory", "saved-build-info",
	}
}

func testFoundryCompilerOutput(
	object string,
	immutableReferences map[string][]foundryCompilerReference,
	linkReferences map[string]map[string][]foundryCompilerReference,
) foundryCompilerOutput {
	var contract foundryCompilerContract
	contract.EVM.DeployedBytecode = foundryCompilerBytecode{
		Object:              object,
		ImmutableReferences: immutableReferences,
		LinkReferences:      linkReferences,
	}
	return foundryCompilerOutput{
		Contracts: map[string]map[string]foundryCompilerContract{
			"Target.sol": {"Target": contract},
		},
	}
}

func writeFoundryBuildInfo(t *testing.T, directory string, output foundryCompilerOutput) {
	t.Helper()
	data, err := json.Marshal(foundryBuildInfo{Output: &output})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(directory, "unit.json"), data, 0o600))
}

func testFoundryCompilations(projectRoot string, runtimeBytecode []byte) []types.Compilation {
	return []types.Compilation{{
		SourcePathToArtifact: map[string]types.SourceArtifact{
			filepath.Join(projectRoot, "Target.sol"): {
				Contracts: map[string]types.CompiledContract{
					"Target": {RuntimeBytecode: runtimeBytecode},
				},
			},
		},
	}}
}

func testImmutableReferenceMap(start uint64, length uint64) map[string][]foundryCompilerReference {
	return map[string][]foundryCompilerReference{
		"1": {{Start: uint64Pointer(start), Length: uint64Pointer(length)}},
	}
}

func testLinkReferenceMap(start uint64, length uint64) map[string]map[string][]foundryCompilerReference {
	return map[string]map[string][]foundryCompilerReference{
		"Library.sol": {
			"Library": {{Start: uint64Pointer(start), Length: uint64Pointer(length)}},
		},
	}
}

func testMetadataSuffixHex(t *testing.T) string {
	t.Helper()
	metadata, err := cbor.Marshal(
		map[string]any{"solc": []byte{0x00, 0x08, 0x1c}},
		cbor.EncOptions{Sort: cbor.SortCanonical},
	)
	require.NoError(t, err)
	var footer [2]byte
	binary.BigEndian.PutUint16(footer[:], uint16(len(metadata)))
	return hex.EncodeToString(append(metadata, footer[:]...))
}

func uint64Pointer(value uint64) *uint64 {
	return &value
}
