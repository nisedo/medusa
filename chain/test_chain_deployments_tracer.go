package chain

import (
	"bytes"
	"math/big"

	"github.com/crytic/medusa-geth/common"
	"github.com/crytic/medusa-geth/core/tracing"
	coretypes "github.com/crytic/medusa-geth/core/types"
	"github.com/crytic/medusa-geth/core/vm"
	"github.com/crytic/medusa-geth/eth/tracers"
	"github.com/crytic/medusa/chain/types"
)

// testChainDeploymentsTracer implements TestChainTracer, capturing information regarding contract deployments and
// self-destructs. It is a special tracer that is used internally by each TestChain. It subscribes to its block-related
// events in order to power the TestChain's contract deployment related events.
type testChainDeploymentsTracer struct {
	// results describes the results being currently captured.
	results []types.DeployedContractBytecodeChange

	// callDepth refers to the current EVM depth during tracing.
	callDepth uint64

	// evm refers to the last tracing.VMContext captured.
	evmContext *tracing.VMContext

	// pendingCallFrames represents per-call-frame data deployment information being captured by the tracer.
	// This is committed as each call frame succeeds, so that contract deployments which later encountered an error
	// and reverted are not considered. The index of each element in the array represents its call frame depth.
	pendingCallFrames []*testChainDeploymentsTracerCallFrame

	// nativeTracer is the underlying tracer interface that the deployment tracer follows
	nativeTracer *TestChainTracer
}

// testChainDeploymentsTracerCallFrame represents per-call-frame data traced by a testChainDeploymentsTracer.
type testChainDeploymentsTracerCallFrame struct {
	// results describes the results being currently captured.
	results []types.DeployedContractBytecodeChange
}

type testChainDeploymentsTracerSnapshot struct {
	results           []types.DeployedContractBytecodeChange
	callDepth         uint64
	pendingCallFrames []*testChainDeploymentsTracerCallFrame
}

// newTestChainDeploymentsTracer creates a testChainDeploymentsTracer
func newTestChainDeploymentsTracer() *testChainDeploymentsTracer {
	tracer := &testChainDeploymentsTracer{}
	innerTracer := &tracers.Tracer{
		Hooks: &tracing.Hooks{
			OnTxStart: tracer.OnTxStart,
			OnTxEnd:   tracer.OnTxEnd,
			OnEnter:   tracer.OnEnter,
			OnExit:    tracer.OnExit,
			OnOpcode:  tracer.OnOpcode,
		},
	}
	tracer.nativeTracer = &TestChainTracer{Tracer: innerTracer, CaptureTxEndSetAdditionalResults: tracer.CaptureTxEndSetAdditionalResults}

	return tracer

}

// NativeTracer returns the underlying TestChainTracer.
func (t *testChainDeploymentsTracer) NativeTracer() *TestChainTracer {
	return t.nativeTracer
}

func (t *testChainDeploymentsTracer) snapshot() *testChainDeploymentsTracerSnapshot {
	if t == nil {
		return nil
	}
	snapshot := &testChainDeploymentsTracerSnapshot{
		results:           cloneDeploymentChanges(t.results),
		callDepth:         t.callDepth,
		pendingCallFrames: make([]*testChainDeploymentsTracerCallFrame, len(t.pendingCallFrames)),
	}
	for index, frame := range t.pendingCallFrames {
		snapshot.pendingCallFrames[index] = &testChainDeploymentsTracerCallFrame{
			results: cloneDeploymentChanges(frame.results),
		}
	}
	return snapshot
}

func (t *testChainDeploymentsTracer) restore(snapshot *testChainDeploymentsTracerSnapshot) {
	if t == nil || snapshot == nil {
		return
	}
	// A snapshot token can outlive successful child frames and be consumed by
	// an ancestor. Keep the current live stack, but fold the captured child
	// results into that ancestor exactly as successful OnExit calls would.
	if t.callDepth > snapshot.callDepth ||
		int(t.callDepth) >= len(t.pendingCallFrames) ||
		int(snapshot.callDepth) >= len(snapshot.pendingCallFrames) {
		return
	}
	t.results = cloneDeploymentChanges(snapshot.results)
	if t.callDepth == 0 {
		t.pendingCallFrames = []*testChainDeploymentsTracerCallFrame{{
			results: cloneDeploymentChanges(snapshot.pendingCallFrames[0].results),
		}}
		return
	}

	currentDepth := int(t.callDepth)
	currentCallerDepth := currentDepth - 1
	snapshotCheatDepth := int(snapshot.callDepth)
	restoredFrames := make([]*testChainDeploymentsTracerCallFrame, currentDepth+1)
	for depth := 0; depth < currentCallerDepth; depth++ {
		restoredFrames[depth] = &testChainDeploymentsTracerCallFrame{
			results: cloneDeploymentChanges(snapshot.pendingCallFrames[depth].results),
		}
	}

	callerResults := make([]types.DeployedContractBytecodeChange, 0)
	for depth := currentCallerDepth; depth < snapshotCheatDepth; depth++ {
		callerResults = append(
			callerResults,
			cloneDeploymentChanges(snapshot.pendingCallFrames[depth].results)...,
		)
	}
	restoredFrames[currentCallerDepth] = &testChainDeploymentsTracerCallFrame{results: callerResults}
	restoredFrames[currentDepth] = &testChainDeploymentsTracerCallFrame{
		results: cloneDeploymentChanges(snapshot.pendingCallFrames[snapshotCheatDepth].results),
	}
	t.pendingCallFrames = restoredFrames
}

func cloneDeploymentChanges(
	changes []types.DeployedContractBytecodeChange,
) []types.DeployedContractBytecodeChange {
	cloned := make([]types.DeployedContractBytecodeChange, len(changes))
	for index, change := range changes {
		cloned[index] = change
		if change.Contract != nil {
			contract := *change.Contract
			contract.InitBytecode = bytes.Clone(change.Contract.InitBytecode)
			contract.RuntimeBytecode = bytes.Clone(change.Contract.RuntimeBytecode)
			cloned[index].Contract = &contract
		}
	}
	return cloned
}

// OnTxStart is called upon the start of transaction execution, as defined by tracers.Tracer.
func (t *testChainDeploymentsTracer) OnTxStart(vm *tracing.VMContext, tx *coretypes.Transaction, from common.Address) {
	// Reset our tracer state
	t.results = make([]types.DeployedContractBytecodeChange, 0)
	t.pendingCallFrames = make([]*testChainDeploymentsTracerCallFrame, 0)

	// Store our evm reference
	t.evmContext = vm
}

// OnTxEnd is called upon the end of transaction execution, as defined by tracers.Tracer.
func (t *testChainDeploymentsTracer) OnTxEnd(receipt *coretypes.Receipt, err error) {

}

// OnEnter is called upon entering of the call frame, as defined by tracers.Tracer.
func (t *testChainDeploymentsTracer) OnEnter(depth int, typ byte, from common.Address, to common.Address, input []byte, gas uint64, value *big.Int) {
	// Create our call frame struct to track data for this call frame.
	callFrameData := &testChainDeploymentsTracerCallFrame{}
	t.pendingCallFrames = append(t.pendingCallFrames, callFrameData)

	// Update call depth if this is not the top-level call frame
	isTopLevelFrame := depth == 0
	if !isTopLevelFrame {
		t.callDepth++
	}

	// If this is a contract creation, record the `to` address as a pending deployment (if it succeeds upon exit,
	// we commit it).
	if typ == byte(vm.CREATE) || typ == byte(vm.CREATE2) {
		callFrameData.results = append(callFrameData.results, types.DeployedContractBytecodeChange{
			Contract: &types.DeployedContractBytecode{
				Address:         to,
				InitBytecode:    input,
				RuntimeBytecode: nil,
			},
			Creation:        true,
			DynamicCreation: !isTopLevelFrame, // If we're not at the top level, this is a dynamic creation.
			SelfDestructed:  false,
			Destroyed:       false,
		})
	}
}

// OnExit is called after a call to finalize tracing completes for the top of a call frame, as defined by tracers.Tracer.
func (t *testChainDeploymentsTracer) OnExit(depth int, output []byte, gasUsed uint64, err error, reverted bool) {
	// Check to see if this is the top level call frame
	isTopLevelFrame := depth == 0

	// Fetch runtime bytecode for all deployments in this frame which did not record one, before exiting.
	// We had to fetch it upon exit as it does not exist during creation of course.
	for _, contractChange := range t.pendingCallFrames[t.callDepth].results {
		if contractChange.Creation && contractChange.Contract.RuntimeBytecode == nil {
			contractChange.Contract.RuntimeBytecode = t.evmContext.StateDB.GetCode(contractChange.Contract.Address)
		}
	}

	// If we didn't encounter any errors and this is the top level call frame, commit all the results
	if isTopLevelFrame {
		if err == nil {
			t.results = append(t.results, t.pendingCallFrames[t.callDepth].results...)
		}
	} else {
		// If we didn't encounter an error in this call frame, we push our captured data up one frame.
		if err == nil {
			t.pendingCallFrames[t.callDepth-1].results = append(t.pendingCallFrames[t.callDepth-1].results, t.pendingCallFrames[t.callDepth].results...)
		}

		// We're exiting the current frame, so remove our frame data and decrement the call depth.
		t.pendingCallFrames = t.pendingCallFrames[:t.callDepth]
		t.callDepth--
	}

}

// OnOpcode records data from an EVM state update, as defined by tracers.Tracer.
func (t *testChainDeploymentsTracer) OnOpcode(pc uint64, op byte, gas, cost uint64, scope tracing.OpContext, rData []byte, depth int, err error) {
	// If we encounter a SELFDESTRUCT operation, record the change to our contract in our results.
	if op == byte(vm.SELFDESTRUCT) {
		callFrameData := t.pendingCallFrames[t.callDepth]
		addr := scope.Address()
		code := t.evmContext.StateDB.GetCode(addr)
		callFrameData.results = append(callFrameData.results, types.DeployedContractBytecodeChange{
			Contract: &types.DeployedContractBytecode{
				Address:         addr,
				InitBytecode:    nil,
				RuntimeBytecode: code,
			},
			Creation:        false,
			DynamicCreation: false,
			SelfDestructed:  true,
			// Check if this is a new contract (not previously deployed and self destructed).
			// TODO: Note that if we allow users to eventually choose which fork they want to use, we will have to check
			//  if we are before Cancun or not. For the sake of simplicity we will just check for the existence of the
			//  address
			Destroyed: !t.evmContext.StateDB.Exist(addr),
		})
	}
}

// CaptureTxEndSetAdditionalResults can be used to set additional results captured from execution tracing. If this
// tracer is used during transaction execution (block creation), the results can later be queried from the block.
// This method will only be called on the added tracer if it implements the extended TestChainTracer interface.
func (t *testChainDeploymentsTracer) CaptureTxEndSetAdditionalResults(results *types.MessageResults) {
	// Set our results. This is an internal tracer used by the test chain, so we don't need to use the
	// "additional results" field as other tracers might, we instead populate the field explicitly defined.
	results.ContractDeploymentChanges = t.results
}
