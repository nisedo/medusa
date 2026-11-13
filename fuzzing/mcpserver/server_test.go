package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"math/big"
	"net/http"
	"testing"
	"time"

	"github.com/crytic/medusa-geth/crypto"
	"github.com/crytic/medusa/compilation"
	"github.com/crytic/medusa/compilation/platforms"
	"github.com/crytic/medusa/fuzzing"
	"github.com/crytic/medusa/fuzzing/config"
	"github.com/crytic/medusa/fuzzing/executiontracer"
	"github.com/crytic/medusa/fuzzing/report"
	"github.com/crytic/medusa/utils/testutils"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testSolcVersion is the solc version the test contract is compiled with, as in the fuzzing package's tests.
const testSolcVersion = "0.8.28"

// newProjectConfig creates a project configuration which compiles the contract at the provided path.
func newProjectConfig(t *testing.T, contractPath string) *config.ProjectConfig {
	platformConfig := platforms.NewCryticCompilationConfig(contractPath)
	platformConfig.SolcVersion = testSolcVersion
	compilationConfig, err := compilation.NewCompilationConfigFromPlatformConfig(platformConfig)
	require.NoError(t, err)
	projectConfig, err := config.GetDefaultProjectConfig("")
	require.NoError(t, err)
	projectConfig.Compilation = compilationConfig
	projectConfig.Slither.UseSlither = false
	return projectConfig
}

// startCampaign starts a fuzzing campaign on the contract at the provided path, which runs until the returned stop
// function is called. The stop function waits for the campaign to end.
func startCampaign(t *testing.T, contractPath string) (*fuzzing.Fuzzer, func()) {
	projectConfig := newProjectConfig(t, contractPath)
	projectConfig.Fuzzing.TargetContracts = []string{"TestContract"}
	projectConfig.Fuzzing.Workers = 2
	projectConfig.Fuzzing.TestLimit = 0
	projectConfig.Fuzzing.Timeout = 0
	projectConfig.Fuzzing.CallSequenceLength = 10
	projectConfig.Fuzzing.CoverageFormats = []string{}
	projectConfig.Fuzzing.Testing.StopOnFailedTest = false
	projectConfig.Fuzzing.Testing.OptimizationTesting.Enabled = false

	fuzzer, err := fuzzing.NewFuzzer(*projectConfig)
	require.NoError(t, err)
	campaignEnded := make(chan error, 1)
	go func() { campaignEnded <- fuzzer.Start() }()
	return fuzzer, func() {
		fuzzer.Stop()
		select {
		case err := <-campaignEnded:
			assert.NoError(t, err)
		case <-time.After(2 * time.Minute):
			t.Error("the campaign did not stop")
		}
	}
}

// callTool calls a tool of the server and decodes its structured output into the provided value.
// Returns the error message if the tool reported an error, or an empty string otherwise.
func callTool(t *testing.T, session *mcp.ClientSession, name string, arguments any, output any) string {
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: arguments})
	require.NoError(t, err)
	if result.IsError {
		require.NotEmpty(t, result.Content)
		return result.Content[0].(*mcp.TextContent).Text
	}
	data, err := json.Marshal(result.StructuredContent)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(data, output))
	return ""
}

// sequenceOutput is the output of the execute_sequence and add_sequence tools, as a client decodes it.
type sequenceOutput struct {
	Calls []struct {
		Trace *executiontracer.TraceCall `json:"trace"`
	} `json:"calls"`
	NewCoverage []bool `json:"newCoverage"`
	FailedTests []struct {
		Type      report.TestType            `json:"type"`
		Name      string                     `json:"name"`
		AfterCall int                        `json:"afterCall"`
		Trace     *executiontracer.TraceCall `json:"trace"`
	} `json:"failedTests"`
	AddedToCorpus bool `json:"addedToCorpus"`
}

// TestServerTools drives a running campaign through every tool, the way an agent would: it finds code the fuzzer
// has not reached, tries a call sequence which reaches it, adds that sequence, and sees the fuzzer fail a test with it.
func TestServerTools(t *testing.T) {
	contractPath := testutils.CopyToTestDirectory(t, "testdata/locked.sol")
	testutils.ExecuteInDirectory(t, contractPath, func() {
		fuzzer, stopCampaign := startCampaign(t, contractPath)
		defer stopCampaign()

		// Connect a client to the server.
		serverTransport, clientTransport := mcp.NewInMemoryTransports()
		serverSession, err := New(fuzzer, "test").MCPServer().Connect(context.Background(), serverTransport, nil)
		require.NoError(t, err)
		defer func() { _ = serverSession.Close() }()
		session, err := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "test"}, nil).Connect(context.Background(), clientTransport, nil)
		require.NoError(t, err)
		defer func() { _ = session.Close() }()

		// status: wait for the campaign to be fuzzing.
		var status statusOutput
		require.Eventually(t, func() bool {
			status = statusOutput{}
			return callTool(t, session, "status", nil, &status) == "" && status.Metrics.CallsTested > 0
		}, time.Minute, 50*time.Millisecond, "the campaign did not start fuzzing")
		assert.Equal(t, report.OutcomeRunning, status.Outcome)
		assert.Positive(t, status.DurationSeconds)
		assert.Zero(t, status.Tests[report.TestStatusFailed])

		// target: the contract can be called, from the configured senders.
		var target targetOutput
		require.Empty(t, callTool(t, session, "target", nil, &target))
		require.Len(t, target.Contracts, 1)
		assert.Equal(t, "TestContract", target.Contracts[0].Name)
		signatures := make([]string, 0)
		for _, method := range target.Contracts[0].Methods {
			signatures = append(signatures, method.Signature)
		}
		assert.Equal(t, []string{"afterTenDays()", "checkLocked()", "property_locked()", "unlock(uint256)", "unlocked()"}, signatures)
		assert.NotEmpty(t, target.Senders)

		// coverage: the fuzzer cannot guess the key, so the line which unlocks is never executed.
		var coverage coverageOutput
		require.Eventually(t, func() bool {
			coverage = coverageOutput{}
			return callTool(t, session, "coverage", map[string]any{"file": "locked.sol"}, &coverage) == "" && coverage.LinesCovered > 0
		}, time.Minute, 50*time.Millisecond, "the campaign did not cover anything")
		require.Len(t, coverage.Files, 1)
		assert.NotEmpty(t, coverage.Files[0].UncoveredLines)
		assert.Contains(t, coverage.Source, "-    8 |             unlocked = true;")
		assert.Contains(t, coverage.Source, "     1 | // The assertion in this contract")
		var summary coverageOutput
		require.Empty(t, callTool(t, session, "coverage", nil, &summary))
		assert.Len(t, summary.Files, 1)
		assert.Empty(t, summary.Source)

		// execute_sequence: unlocking with the right key reaches new code, and makes the assertion fail.
		key := new(big.Int).SetBytes(crypto.Keccak256([]byte("medusa"))).String()
		sequence := map[string]any{"calls": []map[string]any{
			{"contract": "TestContract", "method": "unlock", "arguments": []any{key}},
			{"contract": "TestContract", "method": "checkLocked()", "sender": target.Senders[len(target.Senders)-1], "blockNumberDelay": 5},
		}}
		var executed sequenceOutput
		require.Empty(t, callTool(t, session, "execute_sequence", sequence, &executed))
		require.Len(t, executed.Calls, 2)
		assert.Equal(t, []bool{true, true}, executed.NewCoverage)
		assert.False(t, executed.AddedToCorpus)
		assert.Equal(t, "unlock(uint256)", executed.Calls[0].Trace.Method)
		assert.Equal(t, executiontracer.TraceCallStatusSuccess, executed.Calls[0].Trace.Status)
		assert.Equal(t, executiontracer.TraceCallStatusPanic, executed.Calls[1].Trace.Status)
		assert.Equal(t, "panic: assertion failed", executed.Calls[1].Trace.Error)

		// The tests the sequence fails are reported: the property after the first call, the assertion after the second.
		require.Len(t, executed.FailedTests, 2)
		assert.Equal(t, report.TestTypeProperty, executed.FailedTests[0].Type)
		assert.Equal(t, 1, executed.FailedTests[0].AfterCall)
		require.NotNil(t, executed.FailedTests[0].Trace)
		assert.Equal(t, "property_locked()", executed.FailedTests[0].Trace.Method)
		assert.Equal(t, report.TestTypeAssertion, executed.FailedTests[1].Type)
		assert.Equal(t, 2, executed.FailedTests[1].AfterCall)
		assert.Contains(t, executed.FailedTests[1].Name, "checkLocked()")

		// Executing a sequence changes nothing: the same sequence still reaches new code, and no test failed.
		require.Empty(t, callTool(t, session, "execute_sequence", sequence, &executed))
		assert.Equal(t, []bool{true, true}, executed.NewCoverage)
		var failedTests testsOutput
		require.Empty(t, callTool(t, session, "tests", map[string]any{"status": "failed"}, &failedTests))
		assert.Empty(t, failedTests.Tests)

		// A small integer can be written as a JSON number; the wrong key does not unlock.
		var wrongKey sequenceOutput
		require.Empty(t, callTool(t, session, "execute_sequence", map[string]any{"calls": []map[string]any{
			{"contract": target.Contracts[0].Address, "method": "unlock", "arguments": []any{7}},
			{"contract": "TestContract", "method": "checkLocked"},
		}}, &wrongKey))
		assert.Equal(t, executiontracer.TraceCallStatusSuccess, wrongKey.Calls[1].Trace.Status)

		// A timestamp delay applies even without a block number delay.
		var delayed sequenceOutput
		require.Empty(t, callTool(t, session, "execute_sequence", map[string]any{"calls": []map[string]any{
			{"contract": "TestContract", "method": "afterTenDays"},
			{"contract": "TestContract", "method": "afterTenDays", "blockTimestampDelay": 10 * 24 * 60 * 60},
		}}, &delayed))
		assert.Equal(t, "too early", delayed.Calls[0].Trace.Error)
		assert.Equal(t, executiontracer.TraceCallStatusSuccess, delayed.Calls[1].Trace.Status)

		// Mistakes are reported as tool errors which say what to do instead.
		for arguments, expectedError := range map[string]string{
			`{"calls":[{"contract":"Missing","method":"unlock"}]}`:                                    `no contract named "Missing" is deployed; deployed contracts: TestContract`,
			`{"calls":[{"contract":"TestContract","method":"missing"}]}`:                              `contract TestContract has no method "missing"`,
			`{"calls":[{"contract":"TestContract","method":"unlock","arguments":[1.5]}]}`:             `is not an exact integer`,
			`{"calls":[{"contract":"TestContract","method":"unlock","value":"1","arguments":["1"]}]}`: `is not payable`,
		} {
			var ignored sequenceOutput
			assert.Contains(t, callTool(t, session, "execute_sequence", json.RawMessage(arguments), &ignored), expectedError)
		}
		assert.Contains(t, callTool(t, session, "tests", map[string]any{"status": "broken"}, &failedTests), "unknown status")
		assert.Contains(t, callTool(t, session, "add_sequence", map[string]any{"calls": []any{}}, &executed), "at least one call")

		// add_sequence: the sequence is added to the corpus once, and a worker replays it, which fails both tests.
		var added sequenceOutput
		require.Empty(t, callTool(t, session, "add_sequence", sequence, &added))
		assert.True(t, added.AddedToCorpus)
		require.Empty(t, callTool(t, session, "add_sequence", sequence, &added))
		assert.False(t, added.AddedToCorpus, "the corpus already has the sequence")
		require.Eventually(t, func() bool {
			failedTests = testsOutput{}
			return callTool(t, session, "tests", map[string]any{"status": "failed"}, &failedTests) == "" && len(failedTests.Tests) == 2
		}, time.Minute, 50*time.Millisecond, "the added sequence did not fail both tests")
		for _, test := range failedTests.Tests {
			assert.NotEmpty(t, test.CallSequence)
			assert.LessOrEqual(t, len(test.CallSequence), 2, "the failing call sequence of %s was not shrunk", test.Method)
		}

		// The replay recorded the coverage, so the sequence no longer reaches new code.
		require.Eventually(t, func() bool {
			return callTool(t, session, "execute_sequence", sequence, &executed) == "" && !executed.NewCoverage[0] && !executed.NewCoverage[1]
		}, time.Minute, 50*time.Millisecond, "the coverage of the added sequence was not recorded")
		require.Empty(t, callTool(t, session, "status", nil, &status))
		assert.Equal(t, 2, status.Tests[report.TestStatusFailed])
		assert.NotNil(t, status.Metrics.LastNewCoverageAt)

		// Other workers keep reaching the failures from the corpus, but only one shrinks each.
		assert.EqualValues(t, 2, status.Metrics.FailedTests)
	})
}

// TestServerOverHTTP verifies the server is reachable over HTTP on the loopback interface, lists its tools, reports
// that no campaign is running when there is none, and rejects requests from web pages.
func TestServerOverHTTP(t *testing.T) {
	contractPath := testutils.CopyToTestDirectory(t, "testdata/locked.sol")
	testutils.ExecuteInDirectory(t, contractPath, func() {
		// Create a fuzzer without starting a campaign.
		fuzzer, err := fuzzing.NewFuzzer(*newProjectConfig(t, contractPath))
		require.NoError(t, err)

		address, stop, err := New(fuzzer, "test").Listen(0)
		require.NoError(t, err)
		defer stop()
		endpoint := "http://" + address.String() + Path
		assert.Contains(t, address.String(), "127.0.0.1:")

		// A client can connect and list the tools.
		session, err := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "test"}, nil).Connect(context.Background(), &mcp.StreamableClientTransport{Endpoint: endpoint}, nil)
		require.NoError(t, err)
		defer func() { _ = session.Close() }()
		tools, err := session.ListTools(context.Background(), nil)
		require.NoError(t, err)
		names := make([]string, 0)
		for _, tool := range tools.Tools {
			names = append(names, tool.Name)
			assert.NotEmpty(t, tool.Description)
		}
		assert.ElementsMatch(t, []string{"status", "target", "tests", "coverage", "execute_sequence", "add_sequence"}, names)

		// Without a running campaign, status still answers and the tools which need one say so.
		var status statusOutput
		require.Empty(t, callTool(t, session, "status", nil, &status))
		assert.Zero(t, status.Metrics.CallsTested)
		var target targetOutput
		assert.Contains(t, callTool(t, session, "target", nil, &target), fuzzing.ErrNoCampaignRunning.Error())

		// A request from a web page (another origin) is rejected.
		request, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader([]byte(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)))
		require.NoError(t, err)
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Accept", "application/json, text/event-stream")
		request.Header.Set("Origin", "https://example.com")
		request.Header.Set("Sec-Fetch-Site", "cross-site")
		response, err := http.DefaultClient.Do(request)
		require.NoError(t, err)
		defer func() { _ = response.Body.Close() }()
		assert.Equal(t, http.StatusForbidden, response.StatusCode)
	})
}
