package fuzzing

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sync/atomic"
	"testing"

	"github.com/crytic/medusa-geth/common"
	"github.com/crytic/medusa/compilation"
	"github.com/crytic/medusa/compilation/platforms"
	compilationTypes "github.com/crytic/medusa/compilation/types"
	"github.com/crytic/medusa/fuzzing/calls"
	"github.com/crytic/medusa/fuzzing/config"
	"github.com/crytic/medusa/fuzzing/contracts"
	"github.com/crytic/medusa/utils/testutils"
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

func TestFoundryMetadataFreeImmutableCoverageThroughPublicFuzzerPath(t *testing.T) {
	projectRoot := t.TempDir()
	sourceDirectory := filepath.Join(projectRoot, "src")
	require.NoError(t, os.Mkdir(sourceDirectory, 0o700))
	_, testFile, _, ok := runtime.Caller(0)
	require.True(t, ok)
	source, err := os.ReadFile(filepath.Join(
		filepath.Dir(testFile),
		"testdata", "contracts", "coverage", "immutable_runtime.sol",
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

	compiler := platforms.NewCryticCompilationConfig(projectRoot)
	compiler.ExportDirectory = filepath.Join(projectRoot, "crytic-export")
	compiler.Args = []string{
		"--config-file", filepath.Join(projectRoot, "missing-crytic-options.json"),
		"--compile-force-framework", "foundry",
		"--foundry-ignore-compile",
		"--foundry-build-info-directory", "out/build-info",
	}
	compilationConfig, err := compilation.NewCompilationConfigFromPlatformConfig(compiler)
	require.NoError(t, err)
	projectConfig := foundryImmutableCoverageProjectConfig(t, compilationConfig, projectRoot)

	fuzzer, err := NewFuzzer(*projectConfig)
	require.NoError(t, err)
	var immutableDefinition *contracts.Contract
	for _, definition := range fuzzer.ContractDefinitions() {
		if definition.Name() == "ImmutableCoverage" {
			immutableDefinition = definition
			break
		}
	}
	require.NotNil(t, immutableDefinition)
	require.NotEmpty(t, immutableDefinition.CompiledContract().RuntimeBytecodeImmutableReferences)
	metadata := compilationTypes.ExtractContractMetadata(immutableDefinition.CompiledContract().RuntimeBytecode)
	require.True(t, metadata == nil || metadata.ExtractBytecodeHash() == nil)

	var observedAdvance atomic.Bool
	fuzzer.Hooks.CallSequenceTestFuncs = append(
		fuzzer.Hooks.CallSequenceTestFuncs,
		func(_ *FuzzerWorker, sequence calls.CallSequence) ([]ShrinkCallSequenceRequest, error) {
			if len(sequence) == 0 {
				return nil, nil
			}
			method, err := sequence[len(sequence)-1].Method()
			if err != nil {
				return nil, err
			}
			if method == nil || method.Name != "advance" {
				return nil, fmt.Errorf("expected only ImmutableCoverage.advance(), got %v", method)
			}
			observedAdvance.Store(true)
			fuzzer.Stop()
			return nil, nil
		},
	)

	require.NoError(t, fuzzer.Start())
	require.True(t, observedAdvance.Load())
	lcov, err := os.ReadFile(filepath.Join(projectConfig.Fuzzing.CorpusDirectory, "coverage", "lcov.info"))
	require.NoError(t, err)
	require.Regexp(t, regexp.MustCompile(`(?m)^DA:13,[1-9][0-9]*$`), string(lcov))
}

func foundryImmutableCoverageProjectConfig(
	t *testing.T,
	compilationConfig *compilation.CompilationConfig,
	projectRoot string,
) *config.ProjectConfig {
	t.Helper()
	projectConfig, err := config.GetDefaultProjectConfig("")
	require.NoError(t, err)
	projectConfig.Compilation = compilationConfig
	projectConfig.Fuzzing.Workers = 1
	projectConfig.Fuzzing.WorkerResetLimit = 10
	projectConfig.Fuzzing.Timeout = 5
	projectConfig.Fuzzing.TestLimit = 10
	projectConfig.Fuzzing.CallSequenceLength = 1
	projectConfig.Fuzzing.PruneFrequency = 0
	projectConfig.Fuzzing.CorpusDirectory = filepath.Join(projectRoot, "corpus")
	projectConfig.Fuzzing.CoverageFormats = []string{"lcov"}
	projectConfig.Fuzzing.TargetContracts = []string{"ImmutableCoverage"}
	projectConfig.Fuzzing.ConstructorArgs = map[string]map[string]any{
		"ImmutableCoverage": {"step_": "7"},
	}
	projectConfig.Fuzzing.Testing.TargetFunctionSignatures = []string{"ImmutableCoverage.advance()"}
	projectConfig.Fuzzing.Testing.StopOnNoTests = false
	projectConfig.Fuzzing.Testing.StopOnFailedContractMatching = true
	projectConfig.Fuzzing.Testing.AssertionTesting.Enabled = false
	projectConfig.Fuzzing.Testing.PropertyTesting.Enabled = false
	projectConfig.Fuzzing.Testing.OptimizationTesting.Enabled = false
	projectConfig.Slither.UseSlither = false
	return projectConfig
}
