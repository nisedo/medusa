package fuzzing

import (
	"testing"

	"github.com/crytic/medusa/chain"
	"github.com/crytic/medusa/fuzzing/calls"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSnapshotCheatCodeSafety(t *testing.T) {
	targets := []string{
		"testManagedBlockState()",
		"testSameScopeSnapshotThenOwnerRevert()",
		"testChildSnapshotExpiresOnRevert()",
		"testRollRestoresWhenChildReverts()",
		"testChildSnapshotUsedByParent()",
	}
	for _, target := range targets {
		target := target
		t.Run(target, func(t *testing.T) {
			runExpectRevertFuzzerCase(
				t,
				"testdata/contracts/cheat_codes/vm/snapshot_and_revert_to.sol",
				target,
				false,
			)
		})
	}
}

func TestSnapshotCheatCodeRestoresDeploymentMetadata(t *testing.T) {
	tests := []struct {
		target          string
		expectedChanges int
	}{
		{target: "testDeploymentMetadataReverted()", expectedChanges: 0},
		{target: "testChildSnapshotUsedByParent()", expectedChanges: 1},
	}
	for _, test := range tests {
		test := test
		t.Run(test.target, func(t *testing.T) {
			runSnapshotDeploymentMetadataCase(t, test.target, test.expectedChanges)
		})
	}
}

func runSnapshotDeploymentMetadataCase(t *testing.T, target string, expectedChanges int) {
	t.Helper()

	runFuzzerTest(t, &fuzzerSolcFileTest{
		filePath:      "testdata/contracts/cheat_codes/vm/snapshot_and_revert_to.sol",
		configUpdates: expectRevertTestConfig(target),
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
					assert.NoError(f.t, results.ExecutionResult.Err)
					assert.Len(f.t, results.ContractDeploymentChanges, expectedChanges)
					assert.Equal(f.t, expectedChanges > 0, worker.chain.HasPendingStateChanges())
					assert.Equal(f.t, expectedChanges, addedEvents)
					assert.Zero(f.t, removedEvents)
					for _, change := range results.ContractDeploymentChanges {
						assert.True(f.t, change.Creation)
						assert.False(f.t, change.SelfDestructed)
					}
					return nil, nil
				},
			)

			require.NoError(f.t, f.fuzzer.Start())
			require.True(f.t, observedResult)
			if expectedChanges == 0 {
				require.Zero(f.t, addedEvents)
				require.Zero(f.t, removedEvents)
			}
			assertFailedTestsExpected(f, false)
		},
	})
}
