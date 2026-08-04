package coverage

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/crytic/medusa-geth/accounts/abi"
	"github.com/crytic/medusa-geth/common"
	"github.com/crytic/medusa-geth/core"
	gethTypes "github.com/crytic/medusa-geth/core/types"
	"github.com/crytic/medusa/chain"
	chainTypes "github.com/crytic/medusa/chain/types"
	"github.com/crytic/medusa/compilation/platforms"
	compilationTypes "github.com/crytic/medusa/compilation/types"
	"github.com/crytic/medusa/fuzzing/contracts"
	"github.com/crytic/medusa/logging"
	"github.com/stretchr/testify/require"
)

func TestMetadataFreeImmutableRuntimeCoverage(t *testing.T) {
	target := "../testdata/contracts/coverage/immutable_runtime.sol"

	compiler := platforms.NewCryticCompilationConfig(target)
	compiler.ExportDirectory = t.TempDir()
	compiler.SolcVersion = "0.8.28"
	compiler.Args = []string{"--solc-args", "--metadata-hash none"}
	compilations, _, err := compiler.Compile()
	require.NoError(t, err)
	require.Len(t, compilations, 1)
	require.NoError(t, compilations[0].CacheSourceCode())

	standardRuntime, immutableReferences := compileImmutableCoverageStandardJSON(t, target)
	immutableContract, immutableSourcePath := attachImmutableCoverageReferences(
		t,
		&compilations[0],
		standardRuntime,
		immutableReferences,
	)
	ordinaryContract, ordinarySourcePath := findCompiledCoverageContract(t, &compilations[0], "OrdinaryCoverage")
	_, hasMetadataIdentity := runtimeMetadataCoverageMapHash(immutableContract.RuntimeBytecode)
	require.False(t, hasMetadataIdentity)

	contractDefinitions := contracts.Contracts{
		contracts.NewContract("ImmutableCoverage", immutableSourcePath, immutableContract, &compilations[0]),
		contracts.NewContract("OrdinaryCoverage", ordinarySourcePath, ordinaryContract, &compilations[0]),
	}
	tracer, err := NewCoverageTracerWithContractDefinitions(contractDefinitions)
	require.NoError(t, err)

	sender := common.HexToAddress("0x0707")
	genesis := gethTypes.GenesisAlloc{
		sender: {Balance: new(big.Int).Div(abi.MaxInt256, big.NewInt(2))},
	}
	testChain, err := chain.NewTestChain(context.Background(), genesis, nil)
	require.NoError(t, err)
	t.Cleanup(testChain.Close)
	testChain.AddTracer(tracer.NativeTracer(), true, false)
	totalCoverage := NewCoverageMaps()

	firstAddress, firstRuntime := deployImmutableCoverageContract(t, testChain, totalCoverage, sender, immutableContract, 7)
	secondAddress, secondRuntime := deployImmutableCoverageContract(t, testChain, totalCoverage, sender, immutableContract, 11)
	require.NotEqual(t, firstRuntime, secondRuntime)
	require.NotEqual(t, immutableContract.RuntimeBytecode, firstRuntime)
	require.NotEqual(t, immutableContract.RuntimeBytecode, secondRuntime)

	advanceData, err := immutableContract.Abi.Pack("advance")
	require.NoError(t, err)
	for _, address := range []common.Address{firstAddress, secondAddress} {
		executeImmutableCoverageCall(t, testChain, totalCoverage, sender, address, advanceData)
	}

	firstHash := runtimeCoverageMapHash(firstRuntime, tracer.runtimeBytecodeResolver)
	secondHash := runtimeCoverageMapHash(secondRuntime, tracer.runtimeBytecodeResolver)
	compiledHash, err := getCompiledContractCoverageMapHash(immutableContract, false)
	require.NoError(t, err)
	require.Equal(t, compiledHash, firstHash)
	require.Equal(t, firstHash, secondHash)
	require.Contains(t, totalCoverage.maps[compiledHash], firstAddress)
	require.Contains(t, totalCoverage.maps[compiledHash], secondAddress)

	compiledMap, err := totalCoverage.GetCompiledContractCoverageMap(immutableContract, false)
	require.NoError(t, err)
	require.NotNil(t, compiledMap)

	ordinaryAddress, _ := deployImmutableCoverageContract(t, testChain, totalCoverage, sender, ordinaryContract, 0)
	ordinaryAdvanceData, err := ordinaryContract.Abi.Pack("advance")
	require.NoError(t, err)
	executeImmutableCoverageCall(t, testChain, totalCoverage, sender, ordinaryAddress, ordinaryAdvanceData)
	ordinaryMap, err := totalCoverage.GetCompiledContractCoverageMap(ordinaryContract, false)
	require.NoError(t, err)
	require.NotNil(t, ordinaryMap)

	analysis, err := AnalyzeSourceCoverage(compilations, totalCoverage, nil, logging.GlobalLogger)
	require.NoError(t, err)
	require.GreaterOrEqual(t, sourceLineSuccessHits(analysis, "total += step;"), uint64(2))
	require.GreaterOrEqual(t, sourceLineSuccessHits(analysis, "total += 1;"), uint64(1))
}

func compileImmutableCoverageStandardJSON(
	t *testing.T,
	target string,
) ([]byte, []compilationTypes.ImmutableReference) {
	t.Helper()
	source, err := os.ReadFile(target)
	require.NoError(t, err)

	input := map[string]any{
		"language": "Solidity",
		"sources": map[string]any{
			target: map[string]string{"content": string(source)},
		},
		"settings": map[string]any{
			"metadata": map[string]string{"bytecodeHash": "none"},
			"outputSelection": map[string]any{
				"*": map[string]any{
					"*": []string{
						"evm.deployedBytecode.object",
						"evm.deployedBytecode.immutableReferences",
					},
				},
			},
		},
	}
	inputJSON, err := json.Marshal(input)
	require.NoError(t, err)

	cmd := exec.Command("solc", "--standard-json")
	cmd.Stdin = bytes.NewReader(inputJSON)
	output, err := cmd.Output()
	require.NoError(t, err)

	type standardJSONContract struct {
		EVM struct {
			DeployedBytecode struct {
				Object              string                                           `json:"object"`
				ImmutableReferences map[string][]compilationTypes.ImmutableReference `json:"immutableReferences"`
			} `json:"deployedBytecode"`
		} `json:"evm"`
	}
	var result struct {
		Contracts map[string]map[string]standardJSONContract `json:"contracts"`
		Errors    []struct {
			Severity         string `json:"severity"`
			FormattedMessage string `json:"formattedMessage"`
		} `json:"errors"`
	}
	require.NoError(t, json.Unmarshal(output, &result))
	for _, compilerError := range result.Errors {
		if compilerError.Severity == "error" {
			require.FailNow(t, "standard JSON compilation failed", compilerError.FormattedMessage)
		}
	}

	artifact, ok := result.Contracts[target]["ImmutableCoverage"]
	require.True(t, ok)
	runtimeBytecode, err := hex.DecodeString(strings.TrimPrefix(artifact.EVM.DeployedBytecode.Object, "0x"))
	require.NoError(t, err)

	var references []compilationTypes.ImmutableReference
	for _, immutableReferences := range artifact.EVM.DeployedBytecode.ImmutableReferences {
		references = append(references, immutableReferences...)
	}
	references, err = compilationTypes.ValidateImmutableReferences(runtimeBytecode, references)
	require.NoError(t, err)
	require.NotEmpty(t, references)
	return runtimeBytecode, references
}

func attachImmutableCoverageReferences(
	t *testing.T,
	compilation *compilationTypes.Compilation,
	standardRuntime []byte,
	references []compilationTypes.ImmutableReference,
) (*compilationTypes.CompiledContract, string) {
	t.Helper()
	for sourcePath, source := range compilation.SourcePathToArtifact {
		contract, ok := source.Contracts["ImmutableCoverage"]
		if !ok {
			continue
		}
		require.Equal(t, standardRuntime, contract.RuntimeBytecode)
		contract.RuntimeBytecodeImmutableReferences = references
		source.Contracts["ImmutableCoverage"] = contract
		compilation.SourcePathToArtifact[sourcePath] = source
		return &contract, sourcePath
	}
	require.FailNow(t, "ImmutableCoverage was not found in compiler output")
	return nil, ""
}

func findCompiledCoverageContract(
	t *testing.T,
	compilation *compilationTypes.Compilation,
	contractName string,
) (*compilationTypes.CompiledContract, string) {
	t.Helper()
	for sourcePath, source := range compilation.SourcePathToArtifact {
		if contract, ok := source.Contracts[contractName]; ok {
			return &contract, sourcePath
		}
	}
	require.FailNow(t, fmt.Sprintf("%s was not found in compiler output", contractName))
	return nil, ""
}

func deployImmutableCoverageContract(
	t *testing.T,
	testChain *chain.TestChain,
	totalCoverage *CoverageMaps,
	sender common.Address,
	contract *compilationTypes.CompiledContract,
	constructorValue int64,
) (common.Address, []byte) {
	t.Helper()
	var constructorArgs []any
	if len(contract.Abi.Constructor.Inputs) > 0 {
		constructorArgs = []any{big.NewInt(constructorValue)}
	}
	data, err := contract.GetDeploymentMessageData(constructorArgs)
	require.NoError(t, err)
	block := executeImmutableCoverageMessage(t, testChain, immutableCoverageMessage(testChain, sender, nil, data))
	requireImmutableCoverageUpdate(t, totalCoverage, block.MessageResults[0])
	address := block.MessageResults[0].Receipt.ContractAddress
	return address, bytes.Clone(testChain.State().GetCode(address))
}

func executeImmutableCoverageCall(
	t *testing.T,
	testChain *chain.TestChain,
	totalCoverage *CoverageMaps,
	sender common.Address,
	address common.Address,
	data []byte,
) {
	t.Helper()
	block := executeImmutableCoverageMessage(t, testChain, immutableCoverageMessage(testChain, sender, &address, data))
	requireImmutableCoverageUpdate(t, totalCoverage, block.MessageResults[0])
}

func immutableCoverageMessage(testChain *chain.TestChain, sender common.Address, to *common.Address, data []byte) *core.Message {
	return &core.Message{
		To:               to,
		From:             sender,
		Nonce:            testChain.State().GetNonce(sender),
		Value:            big.NewInt(0),
		GasLimit:         testChain.BlockGasLimit,
		GasPrice:         big.NewInt(1),
		GasFeeCap:        big.NewInt(0),
		GasTipCap:        big.NewInt(0),
		Data:             data,
		SkipNonceChecks:  false,
		SkipFromEOACheck: false,
	}
}

func executeImmutableCoverageMessage(t *testing.T, testChain *chain.TestChain, message *core.Message) *chainTypes.Block {
	t.Helper()
	block, err := testChain.PendingBlockCreate()
	require.NoError(t, err)
	require.NoError(t, testChain.PendingBlockAddTx(message))
	require.NoError(t, testChain.PendingBlockCommit())
	require.Equal(t, gethTypes.ReceiptStatusSuccessful, block.MessageResults[0].Receipt.Status)
	return block
}

func requireImmutableCoverageUpdate(t *testing.T, total *CoverageMaps, results *chainTypes.MessageResults) {
	t.Helper()
	transactionCoverage := GetCoverageTracerResults(results)
	require.NotNil(t, transactionCoverage)
	_, err := total.Update(transactionCoverage)
	require.NoError(t, err)
}

func sourceLineSuccessHits(analysis *SourceAnalysis, sourceFragment string) uint64 {
	for _, file := range analysis.Files {
		for _, line := range file.Lines {
			if strings.Contains(string(line.Contents), sourceFragment) {
				return line.SuccessHitCount
			}
		}
	}
	return 0
}
