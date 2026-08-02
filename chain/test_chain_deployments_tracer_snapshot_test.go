package chain

import (
	"testing"

	"github.com/crytic/medusa-geth/common"
	"github.com/crytic/medusa-geth/core/vm"
	chainTypes "github.com/crytic/medusa/chain/types"
	"github.com/stretchr/testify/require"
)

func TestDeploymentTracerSnapshotFoldsCompletedChildIntoAncestor(t *testing.T) {
	t.Parallel()

	ancestorChange := deploymentChangeForTest(common.HexToAddress("0x100"), 0x01)
	childChange := deploymentChangeForTest(common.HexToAddress("0x200"), 0x02)
	tracer := newTestChainDeploymentsTracer()
	tracer.callDepth = 2
	tracer.pendingCallFrames = []*testChainDeploymentsTracerCallFrame{
		{results: []chainTypes.DeployedContractBytecodeChange{ancestorChange}},
		{results: []chainTypes.DeployedContractBytecodeChange{childChange}},
		{},
	}
	snapshot := tracer.snapshot()

	// The child returned successfully, and the parent is now inside the
	// revertTo precompile. Changes after the user snapshot must disappear.
	tracer.callDepth = 1
	tracer.pendingCallFrames = []*testChainDeploymentsTracerCallFrame{
		{results: []chainTypes.DeployedContractBytecodeChange{
			ancestorChange,
			childChange,
			deploymentChangeForTest(common.HexToAddress("0x300"), 0x03),
		}},
		{},
	}
	tracer.restore(snapshot)

	require.Equal(t, uint64(1), tracer.callDepth)
	require.Len(t, tracer.pendingCallFrames, 2)
	require.Equal(
		t,
		[]chainTypes.DeployedContractBytecodeChange{ancestorChange, childChange},
		tracer.pendingCallFrames[0].results,
	)
	require.Empty(t, tracer.pendingCallFrames[1].results)

	// The restored values must not alias the snapshot or live tracer values.
	snapshot.pendingCallFrames[1].results[0].Contract.RuntimeBytecode[0] = 0xff
	require.Equal(t, byte(0x02), tracer.pendingCallFrames[0].results[1].Contract.RuntimeBytecode[0])
}

func TestDeploymentTracerDoesNotCommitRevertedTopLevelFrame(t *testing.T) {
	t.Parallel()

	tracer := newTestChainDeploymentsTracer()
	tracer.callDepth = 0
	tracer.pendingCallFrames = []*testChainDeploymentsTracerCallFrame{{
		results: []chainTypes.DeployedContractBytecodeChange{
			deploymentChangeForTest(common.HexToAddress("0x100"), 0x01),
		},
	}}

	tracer.OnExit(0, nil, 0, vm.ErrExecutionReverted, true)
	require.Empty(t, tracer.results)
}

func deploymentChangeForTest(
	address common.Address,
	runtimeByte byte,
) chainTypes.DeployedContractBytecodeChange {
	return chainTypes.DeployedContractBytecodeChange{
		Contract: &chainTypes.DeployedContractBytecode{
			Address:         address,
			InitBytecode:    []byte{runtimeByte + 1},
			RuntimeBytecode: []byte{runtimeByte},
		},
		Creation:        true,
		DynamicCreation: true,
	}
}
