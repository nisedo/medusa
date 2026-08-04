package testutils

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// WriteSolidityBuildInfo compiles one in-memory Solidity source and writes a Hardhat/Foundry-compatible build-info
// file. Tests use it to exercise compiler-output ingestion without requiring a framework-specific build tool.
func WriteSolidityBuildInfo(
	t *testing.T,
	buildInfoDirectory string,
	sourcePath string,
	source []byte,
	solcVersion string,
	bytecodeHash string,
) {
	t.Helper()
	input := map[string]any{
		"language": "Solidity",
		"sources": map[string]any{
			sourcePath: map[string]string{"content": string(source)},
		},
		"settings": map[string]any{
			"metadata": map[string]string{"bytecodeHash": bytecodeHash},
			"outputSelection": map[string]any{
				"*": map[string]any{
					"":  []string{"ast"},
					"*": []string{"abi", "devdoc", "userdoc", "evm.bytecode", "evm.deployedBytecode"},
				},
			},
		},
	}
	inputJSON, err := json.Marshal(input)
	require.NoError(t, err)
	cmd := exec.Command("solc", "--standard-json")
	cmd.Stdin = bytes.NewReader(inputJSON)
	outputJSON, err := cmd.Output()
	require.NoError(t, err)

	var compilerResult struct {
		Errors []struct {
			Severity         string `json:"severity"`
			FormattedMessage string `json:"formattedMessage"`
		} `json:"errors"`
	}
	require.NoError(t, json.Unmarshal(outputJSON, &compilerResult))
	for _, compilerError := range compilerResult.Errors {
		require.NotEqual(t, "error", compilerError.Severity, compilerError.FormattedMessage)
	}

	buildInfo := struct {
		SolcVersion string          `json:"solcVersion"`
		Input       json.RawMessage `json:"input"`
		Output      json.RawMessage `json:"output"`
	}{
		SolcVersion: solcVersion,
		Input:       inputJSON,
		Output:      outputJSON,
	}
	buildInfoJSON, err := json.Marshal(buildInfo)
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(buildInfoDirectory, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(buildInfoDirectory, "unit.json"), buildInfoJSON, 0o600))
}
