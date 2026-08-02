package chain

import (
	"math/big"

	"github.com/crytic/medusa-geth/common"
)

const firstStateSnapshotToken uint64 = 0x4d45445553410000

type cheatCodeStateSnapshot struct {
	stateRevision         int
	scopeFrames           map[*cheatCodeTracerCallFrame]struct{}
	managedState          cheatCodeManagedState
	deploymentTracerState *testChainDeploymentsTracerSnapshot
}

type cheatCodeManagedState struct {
	contextTime       uint64
	contextNumber     *big.Int
	contextBaseFee    *big.Int
	contextRandom     *common.Hash
	contextCoinbase   common.Address
	headerTime        uint64
	headerNumber      *big.Int
	headerBaseFee     *big.Int
	headerCoinbase    common.Address
	chainID           *big.Int
	labels            map[common.Address]string
	hasPendingContext bool
	hasPendingBlock   bool
	hasChainConfig    bool
}

func (t *cheatCodeTracer) createStateSnapshot() uint64 {
	if t.stateSnapshots == nil {
		t.stateSnapshots = make(map[uint64]*cheatCodeStateSnapshot)
	}
	if t.nextSnapshotToken < firstStateSnapshotToken {
		t.nextSnapshotToken = firstStateSnapshotToken
	} else {
		t.nextSnapshotToken++
		if t.nextSnapshotToken == 0 {
			t.nextSnapshotToken = firstStateSnapshotToken
		}
	}
	token := t.nextSnapshotToken
	t.stateSnapshots[token] = &cheatCodeStateSnapshot{
		stateRevision:         t.chain.State().Snapshot(),
		scopeFrames:           t.currentCheatCodeCallerFrames(),
		managedState:          t.captureCheatCodeManagedState(),
		deploymentTracerState: t.chain.deploymentsTracer.snapshot(),
	}

	// If this frame or one of its parents reverts, geth invalidates the
	// underlying revision. Remove the user token with the same lifetime.
	if frame := t.CurrentCallFrame(); frame != nil {
		frame.onChainRevertRestoreHooks.Push(func() {
			delete(t.stateSnapshots, token)
		})
	}
	return token
}

func (t *cheatCodeTracer) revertToStateSnapshot(value *big.Int) (bool, *cheatCodeRawReturnData) {
	if value == nil || !value.IsUint64() {
		return false, nil
	}
	token := value.Uint64()
	snapshot, ok := t.stateSnapshots[token]
	if !ok || t.expectRevertFailed || !t.canRevertToStateSnapshot(snapshot) {
		return false, nil
	}

	t.chain.State().RevertToSnapshot(snapshot.stateRevision)
	t.restoreCheatCodeManagedState(snapshot.managedState)
	t.chain.deploymentsTracer.restore(snapshot.deploymentTracerState)
	for candidate, candidateSnapshot := range t.stateSnapshots {
		if candidateSnapshot.stateRevision >= snapshot.stateRevision {
			delete(t.stateSnapshots, candidate)
		}
	}
	return true, nil
}

func (t *cheatCodeTracer) canRevertToStateSnapshot(snapshot *cheatCodeStateSnapshot) bool {
	for frame := range t.currentCheatCodeCallerFrames() {
		if _, existedAtSnapshot := snapshot.scopeFrames[frame]; !existedAtSnapshot {
			return false
		}
	}
	return true
}

func (t *cheatCodeTracer) currentCheatCodeCallerFrames() map[*cheatCodeTracerCallFrame]struct{} {
	frames := make(map[*cheatCodeTracerCallFrame]struct{})
	if len(t.callFrames) == 0 {
		return frames
	}

	caller := t.PreviousCallFrame()
	if caller == nil {
		caller = t.CurrentCallFrame()
	}
	for _, frame := range t.callFrames {
		frames[frame] = struct{}{}
		if frame == caller {
			break
		}
	}
	return frames
}

func (t *cheatCodeTracer) captureCheatCodeManagedState() cheatCodeManagedState {
	state := cheatCodeManagedState{
		labels: make(map[common.Address]string, len(t.chain.Labels)),
	}
	for address, label := range t.chain.Labels {
		state.labels[address] = label
	}

	if context := t.chain.pendingBlockContext; context != nil {
		state.hasPendingContext = true
		state.contextTime = context.Time
		state.contextNumber = cloneBigInt(context.BlockNumber)
		state.contextBaseFee = cloneBigInt(context.BaseFee)
		state.contextRandom = cloneHashPointer(context.Random)
		state.contextCoinbase = context.Coinbase
	}
	if block := t.chain.pendingBlock; block != nil {
		state.hasPendingBlock = true
		state.headerTime = block.Header.Time
		state.headerNumber = cloneBigInt(block.Header.Number)
		state.headerBaseFee = cloneBigInt(block.Header.BaseFee)
		state.headerCoinbase = block.Header.Coinbase
	}
	if chainConfig := t.chain.pendingBlockChainConfig; chainConfig != nil {
		state.hasChainConfig = true
		state.chainID = cloneBigInt(chainConfig.ChainID)
	}
	return state
}

func (t *cheatCodeTracer) restoreCheatCodeManagedState(state cheatCodeManagedState) {
	if state.hasPendingContext && t.chain.pendingBlockContext != nil {
		context := t.chain.pendingBlockContext
		context.Time = state.contextTime
		context.BlockNumber = cloneBigInt(state.contextNumber)
		context.BaseFee = cloneBigInt(state.contextBaseFee)
		context.Random = cloneHashPointer(state.contextRandom)
		context.Coinbase = state.contextCoinbase
	}
	if state.hasPendingBlock && t.chain.pendingBlock != nil {
		header := t.chain.pendingBlock.Header
		header.Time = state.headerTime
		header.Number = cloneBigInt(state.headerNumber)
		header.BaseFee = cloneBigInt(state.headerBaseFee)
		header.Coinbase = state.headerCoinbase
	}
	if state.hasChainConfig && t.chain.pendingBlockChainConfig != nil {
		t.chain.pendingBlockChainConfig.ChainID = cloneBigInt(state.chainID)
	}

	for address := range t.chain.Labels {
		delete(t.chain.Labels, address)
	}
	if t.chain.Labels == nil {
		t.chain.Labels = make(map[common.Address]string, len(state.labels))
	}
	for address, label := range state.labels {
		t.chain.Labels[address] = label
	}
}

func cloneBigInt(value *big.Int) *big.Int {
	if value == nil {
		return nil
	}
	return new(big.Int).Set(value)
}

func cloneHashPointer(value *common.Hash) *common.Hash {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}
