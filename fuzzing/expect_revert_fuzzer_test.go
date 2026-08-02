package fuzzing

import (
	"testing"

	"github.com/crytic/medusa-geth/common"
	"github.com/crytic/medusa-geth/core/vm"
	"github.com/crytic/medusa/chain"
	"github.com/crytic/medusa/fuzzing/calls"
	"github.com/crytic/medusa/fuzzing/config"
	"github.com/stretchr/testify/assert"
)

const (
	expectRevertPassFixture = "testdata/contracts/cheat_codes/vm/expect_revert_pass.sol"
	expectRevertFailFixture = "testdata/contracts/cheat_codes/vm/expect_revert_fail.sol"
)

var expectRevertAssertionReturnData = common.FromHex(
	"0x4e487b710000000000000000000000000000000000000000000000000000000000000001",
)

func expectRevertTestConfig(targetMethod string) func(*config.ProjectConfig) {
	return func(projectConfig *config.ProjectConfig) {
		projectConfig.Fuzzing.TargetContracts = []string{"TestContract"}
		projectConfig.Fuzzing.Workers = 1
		projectConfig.Fuzzing.TestLimit = 100
		projectConfig.Fuzzing.CallSequenceLength = 1
		projectConfig.Fuzzing.Testing.TargetFunctionSignatures = []string{
			"TestContract." + targetMethod,
		}
		projectConfig.Fuzzing.Testing.PropertyTesting.Enabled = false
		projectConfig.Fuzzing.Testing.OptimizationTesting.Enabled = false
		projectConfig.Fuzzing.Testing.AssertionTesting.Enabled = true
		projectConfig.Fuzzing.TestChainConfig.CheatCodeConfig.CheatCodesEnabled = true
		projectConfig.Slither.UseSlither = false
	}
}

func runExpectRevertFuzzerCase(
	t *testing.T,
	fixture string,
	targetMethod string,
	expectFailure bool,
) {
	t.Helper()
	runFuzzerTest(t, &fuzzerSolcFileTest{
		filePath:      fixture,
		configUpdates: expectRevertTestConfig(targetMethod),
		method: func(f *fuzzerTestContext) {
			observedResult := false
			f.fuzzer.Hooks.CallSequenceTestFuncs = append(
				f.fuzzer.Hooks.CallSequenceTestFuncs,
				func(_ *FuzzerWorker, sequence calls.CallSequence) ([]ShrinkCallSequenceRequest, error) {
					if len(sequence) == 0 || sequence[len(sequence)-1].ChainReference == nil {
						return nil, nil
					}

					observedResult = true
					result := sequence[len(sequence)-1].ChainReference.MessageResults().ExecutionResult
					if expectFailure {
						assert.ErrorIs(f.t, result.Err, vm.ErrExecutionReverted)
						assert.Equal(f.t, expectRevertAssertionReturnData, result.ReturnData)
					} else {
						assert.NoError(f.t, result.Err)
						f.fuzzer.Stop()
					}
					return nil, nil
				},
			)

			err := f.fuzzer.Start()
			assert.NoError(f.t, err)
			assert.True(f.t, observedResult)
			assertFailedTestsExpected(f, expectFailure)
		},
	})
}

func TestExpectRevertCheatCodePassingCases(t *testing.T) {
	targets := []string{
		"testAllOverloads()",
		"testExactAndPartialData()",
		"testErrorString()",
		"testCountZeroAndTwo()",
		"testSkipsConsoleAndOtherCheatCalls()",
		"testNestedBubbledReverter()",
		"testCallAndCreatePresentation()",
		"testSequentialExpectations()",
	}
	for _, target := range targets {
		target := target
		t.Run(target, func(t *testing.T) {
			runExpectRevertFuzzerCase(
				t,
				expectRevertPassFixture,
				target,
				false,
			)
		})
	}
}

func TestExpectRevertCheatCodeFailureCases(t *testing.T) {
	targets := []string{
		"testFailUnexpectedSuccess()",
		"testFailWrongData()",
		"testFailExactSelectorIsNotPartial()",
		"testFailWrongReverter()",
		"testFailDuplicateExpectation()",
		"testFailDanglingExpectation()",
		"testFailShortCount()",
		"testFailCaughtMismatch()",
		"testFailActiveTargetRevertTo()",
		"testFailDeploymentBeforeMismatch()",
	}
	for _, target := range targets {
		target := target
		t.Run(target, func(t *testing.T) {
			runExpectRevertFuzzerCase(
				t,
				expectRevertFailFixture,
				target,
				true,
			)
		})
	}
}

func TestExpectRevertFailureRollsBackDeploymentMetadata(t *testing.T) {
	runFuzzerTest(t, &fuzzerSolcFileTest{
		filePath: expectRevertFailFixture,
		configUpdates: expectRevertTestConfig(
			"testFailDeploymentBeforeMismatch()",
		),
		method: func(f *fuzzerTestContext) {
			addedEvents := 0
			removedEvents := 0
			f.fuzzer.Events.WorkerCreated.Subscribe(func(event FuzzerWorkerCreatedEvent) error {
				event.Worker.Events.FuzzerWorkerChainSetup.Subscribe(
					func(setupEvent FuzzerWorkerChainSetupEvent) error {
						setupEvent.Chain.Events.ContractDeploymentAddedEventEmitter.Subscribe(
							func(chain.ContractDeploymentsAddedEvent) error {
								addedEvents++
								return nil
							},
						)
						setupEvent.Chain.Events.ContractDeploymentRemovedEventEmitter.Subscribe(
							func(chain.ContractDeploymentsRemovedEvent) error {
								removedEvents++
								return nil
							},
						)
						return nil
					},
				)
				return nil
			})

			observedResult := false
			f.fuzzer.Hooks.CallSequenceTestFuncs = append(
				f.fuzzer.Hooks.CallSequenceTestFuncs,
				func(worker *FuzzerWorker, sequence calls.CallSequence) ([]ShrinkCallSequenceRequest, error) {
					if len(sequence) == 0 || sequence[len(sequence)-1].ChainReference == nil {
						return nil, nil
					}
					defer f.fuzzer.Stop()
					observedResult = true
					results := sequence[len(sequence)-1].ChainReference.MessageResults()
					assert.ErrorIs(f.t, results.ExecutionResult.Err, vm.ErrExecutionReverted)
					assert.Equal(f.t, expectRevertAssertionReturnData, results.ExecutionResult.ReturnData)
					assert.Empty(f.t, results.ContractDeploymentChanges)
					assert.False(f.t, worker.chain.HasPendingStateChanges())
					assert.Zero(f.t, addedEvents)
					assert.Zero(f.t, removedEvents)
					return nil, nil
				},
			)

			assert.NoError(f.t, f.fuzzer.Start())
			assert.True(f.t, observedResult)
			assert.Zero(f.t, addedEvents)
			assert.Zero(f.t, removedEvents)
		},
	})
}
