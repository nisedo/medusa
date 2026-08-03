package types

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	validSlitherJSON = `{"success":true,"error":"","results":{"printers":[{"description":"{\"constants_used\":{\"TestContract\":{\"testFuzz(uint256)\":[[{\"type\":\"uint256\",\"value\":\"135\"}]]}}}"}]}}`
	slitherWarning   = "Warning: Found unknown 'lint_on_build' config"
)

func TestRunSlitherSeparatesStderr(t *testing.T) {
	cachePath := filepath.Join(t.TempDir(), "slither_results.json")
	config := &SlitherConfig{
		UseSlither:     true,
		CachePath:      cachePath,
		OverwriteCache: true,
	}

	results, err := config.runSlither("target", "", slitherHelperCommand("valid-with-warning"))
	require.NoError(t, err)
	require.Len(t, results.Constants, 1)
	assert.Equal(t, "uint256", results.Constants[0].Type)
	assert.Equal(t, "135", results.Constants[0].Value)

	cached, err := os.ReadFile(cachePath)
	require.NoError(t, err)
	assert.JSONEq(t, validSlitherJSON, string(cached))
	assert.NotContains(t, string(cached), slitherWarning)
}

func TestRunSlitherReportsCommandStderr(t *testing.T) {
	cachePath := filepath.Join(t.TempDir(), "slither_results.json")
	config := &SlitherConfig{
		UseSlither:     true,
		CachePath:      cachePath,
		OverwriteCache: true,
	}

	_, err := config.runSlither("target", "", slitherHelperCommand("command-failure"))
	require.Error(t, err)
	assert.ErrorContains(t, err, "slither command failed")
	assert.ErrorContains(t, err, "fatal slither diagnostic")

	_, statErr := os.Stat(cachePath)
	assert.ErrorIs(t, statErr, os.ErrNotExist)
}

func TestRunSlitherReportsStderrWithMalformedJSON(t *testing.T) {
	cachePath := filepath.Join(t.TempDir(), "slither_results.json")
	config := &SlitherConfig{
		UseSlither:     true,
		CachePath:      cachePath,
		OverwriteCache: true,
	}

	_, err := config.runSlither("target", "", slitherHelperCommand("malformed-json"))
	require.Error(t, err)
	assert.ErrorContains(t, err, "could not parse Slither JSON output")
	assert.ErrorContains(t, err, "malformed JSON diagnostic")

	_, statErr := os.Stat(cachePath)
	assert.ErrorIs(t, statErr, os.ErrNotExist)
}

func slitherHelperCommand(mode string) slitherCommandFactory {
	return func(_ string, _ ...string) *exec.Cmd {
		command := exec.Command(os.Args[0], "-test.run=^TestSlitherHelperProcess$", "--", mode)
		command.Env = append(os.Environ(), "MEDUSA_SLITHER_HELPER=1")
		return command
	}
}

func TestSlitherHelperProcess(t *testing.T) {
	if os.Getenv("MEDUSA_SLITHER_HELPER") != "1" {
		return
	}

	mode := os.Args[len(os.Args)-1]
	switch mode {
	case "valid-with-warning":
		_, _ = fmt.Fprint(os.Stdout, validSlitherJSON)
		_, _ = fmt.Fprintln(os.Stderr, slitherWarning)
		os.Exit(0)
	case "command-failure":
		_, _ = fmt.Fprintln(os.Stderr, "fatal slither diagnostic")
		os.Exit(2)
	case "malformed-json":
		_, _ = fmt.Fprint(os.Stdout, "{not-json")
		_, _ = fmt.Fprintln(os.Stderr, "malformed JSON diagnostic")
		os.Exit(0)
	default:
		_, _ = fmt.Fprintln(os.Stderr, "unknown helper mode")
		os.Exit(3)
	}
}
