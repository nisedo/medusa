package fuzzing

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/crytic/medusa/fuzzing/calls"
	"github.com/crytic/medusa/fuzzing/config"
	"github.com/crytic/medusa/fuzzing/executiontracer"
	"github.com/crytic/medusa/fuzzing/report"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// readResultsFile reads the results file at the provided path into a generic JSON object, the way a tool that does
// not use this module's types would.
func readResultsFile(t *testing.T, path string) map[string]any {
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	var results map[string]any
	require.NoError(t, json.Unmarshal(data, &results))
	return results
}

// TestResultsFileForFailedTests verifies the results file of a campaign with failing tests: the outcome, the failed
// tests with a replayable call sequence and a structured execution trace, the metrics, and the coverage.
func TestResultsFileForFailedTests(t *testing.T) {
	runFuzzerTest(t, &fuzzerSolcFileTest{
		filePath: "testdata/contracts/assertions/assert_and_property_test.sol",
		configUpdates: func(config *config.ProjectConfig) {
			config.Fuzzing.TargetContracts = []string{"TestContract"}
			config.Fuzzing.ResultsFile = filepath.Join("results", "medusa.json")
			config.Fuzzing.Testing.StopOnFailedTest = false
			config.Fuzzing.TestLimit = 2_000
			config.Fuzzing.Testing.OptimizationTesting.Enabled = false
			config.Slither.UseSlither = false
		},
		method: func(f *fuzzerTestContext) {
			require.NoError(t, f.fuzzer.Start())
			results := readResultsFile(t, f.fuzzer.config.Fuzzing.ResultsFile)

			// The campaign ended with failed tests.
			assert.EqualValues(t, 1, results["schemaVersion"])
			assert.Equal(t, "failed", results["outcome"])
			assert.NotContains(t, results, "error")
			assert.Contains(t, results, "endedAt")
			metrics := results["metrics"].(map[string]any)
			assert.Positive(t, metrics["callsTested"])
			assert.Positive(t, metrics["branchesHit"])

			// Both the assertion and the property test failed, each with a call sequence and traces.
			failedTypes := make(map[string]bool)
			for _, test := range results["tests"].([]any) {
				test := test.(map[string]any)
				if test["status"] != "failed" {
					continue
				}
				failedTypes[test["type"].(string)] = true
				assert.Equal(t, "TestContract", test["contract"])
				assert.NotEmpty(t, test["method"])
				assert.NotEmpty(t, test["message"])

				// The call sequence can be loaded as a corpus call sequence, to replay it.
				callSequence := test["callSequence"].([]any)
				require.NotEmpty(t, callSequence)
				callSequenceData, err := json.Marshal(callSequence)
				require.NoError(t, err)
				var corpusCallSequence calls.CallSequence
				require.NoError(t, json.Unmarshal(callSequenceData, &corpusCallSequence))
				assert.Len(t, corpusCallSequence, len(callSequence))

				// The last call is traced. For an assertion test, that call is the one which panicked.
				lastCallTrace := callSequence[len(callSequence)-1].(map[string]any)["trace"].(map[string]any)
				assert.Equal(t, "TestContract", lastCallTrace["contract"])
				assert.NotEmpty(t, lastCallTrace["method"])
				if test["type"] == "assertion" {
					assert.Equal(t, "panic", lastCallTrace["status"])
					assert.NotEmpty(t, lastCallTrace["error"])
				}

				// A property test also has the trace of the property method itself.
				if test["type"] == "property" {
					propertyTrace := test["trace"].(map[string]any)
					assert.Equal(t, test["method"], propertyTrace["method"])
				}
			}
			assert.True(t, failedTypes["assertion"], "the assertion test should have failed")
			assert.True(t, failedTypes["property"], "the property test should have failed")

			// Coverage lists the source file and its functions.
			coverage := results["coverage"].(map[string]any)
			assert.Positive(t, coverage["linesCovered"])
			files := coverage["files"].([]any)
			require.Len(t, files, 1)
			functions := files[0].(map[string]any)["functions"].([]any)
			assert.NotEmpty(t, functions)
			for _, function := range functions {
				function := function.(map[string]any)
				assert.NotEmpty(t, function["name"])
				assert.Positive(t, function["lines"].(map[string]any)["start"])
			}

			// The report is also available through the API, and agrees with the file.
			campaignReport, err := f.fuzzer.Report()
			require.NoError(t, err)
			assert.EqualValues(t, results["outcome"], campaignReport.Outcome)
			assert.Len(t, campaignReport.Tests, len(results["tests"].([]any)))
		},
	})
}

// TestResultsFileForCampaignError verifies the results file is still written when the campaign ends with an error
// before any fuzzing happens, and reports that error.
func TestResultsFileForCampaignError(t *testing.T) {
	runFuzzerTest(t, &fuzzerSolcFileTest{
		filePath: "testdata/contracts/assertions/assert_immediate.sol",
		configUpdates: func(config *config.ProjectConfig) {
			config.Fuzzing.TargetContracts = []string{"MisspelledContract"}
			config.Fuzzing.ResultsFile = "medusa.json"
			config.Fuzzing.TestLimit = 1
			config.Slither.UseSlither = false
		},
		method: func(f *fuzzerTestContext) {
			require.Error(t, f.fuzzer.Start())
			results := readResultsFile(t, f.fuzzer.config.Fuzzing.ResultsFile)

			assert.Equal(t, "error", results["outcome"])
			assert.Contains(t, results["error"], "MisspelledContract")
			assert.Empty(t, results["tests"])
			assert.NotContains(t, results, "coverage")
		},
	})
}

// TestResultsFileForPassedTests verifies the results file of a campaign in which no test fails.
func TestResultsFileForPassedTests(t *testing.T) {
	runFuzzerTest(t, &fuzzerSolcFileTest{
		filePath: "testdata/contracts/cheat_codes/vm/property_no_leak.sol",
		configUpdates: func(config *config.ProjectConfig) {
			config.Fuzzing.TargetContracts = []string{"TestContract"}
			config.Fuzzing.ResultsFile = "medusa.json"
			config.Fuzzing.TestLimit = 500
			config.Fuzzing.Testing.OptimizationTesting.Enabled = false
			config.Fuzzing.TestChainConfig.CheatCodeConfig.CheatCodesEnabled = true
			config.Slither.UseSlither = false
		},
		method: func(f *fuzzerTestContext) {
			require.NoError(t, f.fuzzer.Start())
			results := readResultsFile(t, f.fuzzer.config.Fuzzing.ResultsFile)

			// Every test passed, so none has a call sequence.
			assert.Equal(t, "passed", results["outcome"])
			tests := results["tests"].([]any)
			require.NotEmpty(t, tests)
			for _, test := range tests {
				test := test.(map[string]any)
				assert.Equal(t, "passed", test["status"])
				assert.NotContains(t, test, "callSequence")
				assert.NotContains(t, test, "trace")
			}
		},
	})
}

// collectTraceCalls returns the provided trace call and every call nested within it, and the name of every event
// emitted within them.
func collectTraceCalls(call *executiontracer.TraceCall) ([]*executiontracer.TraceCall, []string) {
	traceCalls := []*executiontracer.TraceCall{call}
	eventNames := make([]string, 0)
	for _, step := range call.Steps {
		if step.Event != nil {
			eventNames = append(eventNames, step.Event.Name)
		}
		if step.Call != nil {
			nestedCalls, nestedEventNames := collectTraceCalls(step.Call)
			traceCalls = append(traceCalls, nestedCalls...)
			eventNames = append(eventNames, nestedEventNames...)
		}
	}
	return traceCalls, eventNames
}

// TestStructuredExecutionTraces verifies the machine-readable form of execution traces describes nested calls,
// proxy calls, contract creations, events, revert reasons, custom errors, panics and self-destructs.
func TestStructuredExecutionTraces(t *testing.T) {
	checksPerTest := map[string]func(t *testing.T, traceCalls []*executiontracer.TraceCall, eventNames []string){
		"testdata/contracts/execution_tracing/call_and_deployment_args.sol": func(t *testing.T, traceCalls []*executiontracer.TraceCall, eventNames []string) {
			// The deployment and the call both have their arguments decoded.
			arguments := make(map[string]string)
			for _, traceCall := range traceCalls {
				if traceCall.Arguments != nil {
					arguments[traceCall.Kind] += *traceCall.Arguments
				}
			}
			assert.Contains(t, arguments[executiontracer.TraceCallKindCreation], "Hello from deployment args!")
			assert.Contains(t, arguments[executiontracer.TraceCallKindCall], "Hello from call args!")
		},
		"testdata/contracts/execution_tracing/event_emission.sol": func(t *testing.T, traceCalls []*executiontracer.TraceCall, eventNames []string) {
			assert.Contains(t, eventNames, "TestEvent")
			assert.Contains(t, eventNames, "TestIndexedEvent")
			assert.Contains(t, eventNames, "TestMixedEvent")
		},
		"testdata/contracts/execution_tracing/proxy_call.sol": func(t *testing.T, traceCalls []*executiontracer.TraceCall, eventNames []string) {
			// The delegate call runs another contract's code, at the address of the calling contract.
			proxyCalls := 0
			for _, traceCall := range traceCalls {
				if traceCall.Kind == executiontracer.TraceCallKindProxyCall {
					proxyCalls++
					assert.Equal(t, "InnerDeploymentContract", traceCall.Contract)
					require.NotNil(t, traceCall.CodeAddress)
					assert.NotEqual(t, traceCall.To, *traceCall.CodeAddress)
					require.NotNil(t, traceCall.Arguments)
					assert.Contains(t, *traceCall.Arguments, "Hello from proxy call args!")
				}
			}
			assert.Positive(t, proxyCalls)
		},
		"testdata/contracts/execution_tracing/revert_custom_error.sol": func(t *testing.T, traceCalls []*executiontracer.TraceCall, eventNames []string) {
			errors := make([]string, 0)
			for _, traceCall := range traceCalls {
				if traceCall.Status == executiontracer.TraceCallStatusRevert {
					errors = append(errors, traceCall.Error)
				}
			}
			require.NotEmpty(t, errors)
			assert.Contains(t, errors[0], "CustomError(")
			assert.Contains(t, errors[0], "Hello from a custom error!")
		},
		"testdata/contracts/execution_tracing/revert_reasons.sol": func(t *testing.T, traceCalls []*executiontracer.TraceCall, eventNames []string) {
			errors := make([]string, 0)
			for _, traceCall := range traceCalls {
				if traceCall.Status == executiontracer.TraceCallStatusRevert {
					errors = append(errors, traceCall.Error)
				}
			}
			assert.Contains(t, errors, "RevertingContract was called and reverted.")
		},
		"testdata/contracts/execution_tracing/self_destruct.sol": func(t *testing.T, traceCalls []*executiontracer.TraceCall, eventNames []string) {
			selfDestructed := false
			for _, traceCall := range traceCalls {
				selfDestructed = selfDestructed || traceCall.SelfDestructed
			}
			assert.True(t, selfDestructed)
		},
	}
	for filePath, check := range checksPerTest {
		runFuzzerTest(t, &fuzzerSolcFileTest{
			filePath: filePath,
			configUpdates: func(config *config.ProjectConfig) {
				config.Fuzzing.TargetContracts = []string{"TestContract"}
				config.Fuzzing.Testing.PropertyTesting.Enabled = false
				config.Fuzzing.Testing.OptimizationTesting.Enabled = false
				config.Slither.UseSlither = false
			},
			method: func(f *fuzzerTestContext) {
				require.NoError(t, f.fuzzer.Start())

				// Obtain the report of the first failed test, and the trace of the last call of its call sequence.
				campaignReport, err := f.fuzzer.Report()
				require.NoError(t, err)
				var traceCall *executiontracer.TraceCall
				for _, test := range campaignReport.Tests {
					if test.Status == report.TestStatusFailed {
						require.NotEmpty(t, test.CallSequence)
						traceCall = test.CallSequence[len(test.CallSequence)-1].Trace.Call()
						break
					}
				}
				require.NotNil(t, traceCall, "expected a failed test with a traced call sequence")

				// The top level call is the assertion test's method, which panicked.
				assert.Equal(t, executiontracer.TraceCallKindCall, traceCall.Kind)
				assert.Equal(t, "TestContract", traceCall.Contract)
				assert.Equal(t, executiontracer.TraceCallStatusPanic, traceCall.Status)
				assert.Equal(t, "panic: assertion failed", traceCall.Error)

				// Verify what happened within the call.
				traceCalls, eventNames := collectTraceCalls(traceCall)
				check(t, traceCalls, eventNames)

				// The trace encodes to JSON.
				_, err = json.Marshal(traceCall)
				assert.NoError(t, err)
			},
		})
	}
}
