package chain

import (
	"math/big"
	"testing"

	"github.com/crytic/medusa-geth/common"
	"github.com/crytic/medusa-geth/core/vm"
	chainTypes "github.com/crytic/medusa/chain/types"
	"github.com/stretchr/testify/require"
)

func TestCheatCodeSnapshotsUseOpaqueTokensAndRestoreState(t *testing.T) {
	t.Parallel()

	testChain, senders := createChain(t)
	t.Cleanup(testChain.Close)
	tracer := snapshotCheatCodeTracerForTest(t, testChain)
	state := testChain.State()
	key := common.HexToHash("0x01")
	initialValue := common.HexToHash("0x11")
	secondValue := common.HexToHash("0x22")
	finalValue := common.HexToHash("0x33")
	state.SetState(senders[0], key, initialValue)

	// Advance geth's internal revision IDs. The user token must remain a
	// separate, monotonic ID rather than exposing one of these revisions.
	for range 5 {
		state.Snapshot()
	}

	firstToken := tracer.createStateSnapshot()
	firstSnapshot, ok := tracer.stateSnapshots[firstToken]
	require.True(t, ok)
	firstRevision := firstSnapshot.stateRevision
	require.Equal(t, firstStateSnapshotToken, firstToken)
	require.NotEqual(t, firstToken, uint64(firstRevision))

	state.SetState(senders[0], key, secondValue)
	secondToken := tracer.createStateSnapshot()
	secondSnapshot, ok := tracer.stateSnapshots[secondToken]
	require.True(t, ok)
	secondRevision := secondSnapshot.stateRevision
	require.Greater(t, secondRevision, firstRevision)
	require.Equal(t, firstToken+1, secondToken)

	state.SetState(senders[0], key, finalValue)
	reverted, rawReturnData := tracer.revertToStateSnapshot(new(big.Int).SetUint64(secondToken))
	require.True(t, reverted)
	require.Nil(t, rawReturnData)
	require.Equal(t, secondValue, state.GetState(senders[0], key))
	require.Contains(t, tracer.stateSnapshots, firstToken)
	require.NotContains(t, tracer.stateSnapshots, secondToken)

	reverted, rawReturnData = tracer.revertToStateSnapshot(new(big.Int).SetUint64(firstToken))
	require.True(t, reverted)
	require.Nil(t, rawReturnData)
	require.Equal(t, initialValue, state.GetState(senders[0], key))
	require.Empty(t, tracer.stateSnapshots)

	reverted, rawReturnData = tracer.revertToStateSnapshot(new(big.Int).SetUint64(firstToken))
	require.False(t, reverted)
	require.Nil(t, rawReturnData)
}

func TestCheatCodeTracerCapturesIndependentLabelHistory(t *testing.T) {
	t.Parallel()

	testChain, _ := createChain(t)
	t.Cleanup(testChain.Close)
	tracer := snapshotCheatCodeTracerForTest(t, testChain)
	tracer.results = &cheatCodeTracerResults{}
	address := common.HexToAddress("0x100")
	testChain.Labels[address] = "before"
	results := &chainTypes.MessageResults{AdditionalResults: make(map[string]any)}

	tracer.CaptureTxEndSetAdditionalResults(results)
	testChain.Labels[address] = "after"

	capturedLabels, ok := results.AdditionalResults[labelsKey].(map[common.Address]string)
	require.True(t, ok)
	require.Equal(t, map[common.Address]string{address: "before"}, capturedLabels)
}

func TestCheatCodeSnapshotTokensResetPerTransactionButNeverRepeat(t *testing.T) {
	t.Parallel()

	testChain, _ := createChain(t)
	t.Cleanup(testChain.Close)
	tracer := snapshotCheatCodeTracerForTest(t, testChain)

	tracer.OnTxStart(nil, nil, common.Address{})
	firstToken := tracer.createStateSnapshot()
	require.Contains(t, tracer.stateSnapshots, firstToken)

	tracer.OnTxStart(nil, nil, common.Address{})
	require.Empty(t, tracer.stateSnapshots)

	reverted, rawReturnData := tracer.revertToStateSnapshot(new(big.Int).SetUint64(firstToken))
	require.False(t, reverted)
	require.Nil(t, rawReturnData)

	secondToken := tracer.createStateSnapshot()
	require.Greater(t, secondToken, firstToken)
	require.NotEqual(t, firstToken, secondToken)
	require.Contains(t, tracer.stateSnapshots, secondToken)
}

func TestCheatCodeSnapshotRejectsInvalidTokens(t *testing.T) {
	t.Parallel()

	testChain, _ := createChain(t)
	t.Cleanup(testChain.Close)
	tracer := snapshotCheatCodeTracerForTest(t, testChain)
	validToken := tracer.createStateSnapshot()

	tooLarge := new(big.Int).Lsh(big.NewInt(1), 80)
	for _, token := range []*big.Int{
		nil,
		big.NewInt(-1),
		tooLarge,
		new(big.Int).SetUint64(999),
	} {
		reverted, rawReturnData := tracer.revertToStateSnapshot(token)
		require.False(t, reverted)
		require.Nil(t, rawReturnData)
	}

	reverted, rawReturnData := tracer.revertToStateSnapshot(new(big.Int).SetUint64(validToken))
	require.True(t, reverted)
	require.Nil(t, rawReturnData)
}

func TestCheatCodeSnapshotTokenExpiresWithItsCreatingFrame(t *testing.T) {
	t.Parallel()

	testChain, _ := createChain(t)
	t.Cleanup(testChain.Close)
	tracer := snapshotCheatCodeTracerForTest(t, testChain)
	frame := newCheatCodeTracerCallFrame()
	tracer.callFrames = []*cheatCodeTracerCallFrame{frame}
	tracer.callDepth = 0

	token := tracer.createStateSnapshot()
	require.Contains(t, tracer.stateSnapshots, token)
	frame.onChainRevertRestoreHooks.Execute(false, true)
	require.NotContains(t, tracer.stateSnapshots, token)
}

func TestCheatCodeSnapshotChildRollbackInvalidatesToken(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name string
		err  error
	}{
		{name: "raw revert", err: vm.ErrExecutionReverted},
		{
			name: "synthetic expectRevert failure",
			err:  expectRevertAssertionFailure(vm.CallFrameResult{}).Err,
		},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			testChain, _ := createChain(t)
			t.Cleanup(testChain.Close)
			tracer := snapshotCheatCodeTracerForTest(t, testChain)
			tracer.OnTxStart(nil, nil, common.Address{})
			tracer.OnEnter(0, byte(vm.CALL), common.Address{}, common.HexToAddress("0x100"), nil, 0, nil)
			childRevision := testChain.State().Snapshot()
			tracer.OnEnter(1, byte(vm.CALL), common.Address{}, common.HexToAddress("0x200"), nil, 0, nil)
			testChain.State().Snapshot()
			tracer.OnEnter(
				2,
				byte(vm.CALL),
				common.Address{},
				StandardCheatcodeContractAddress,
				nil,
				0,
				nil,
			)

			token := tracer.createStateSnapshot()
			tracer.OnExit(2, nil, 0, nil, false)
			require.Contains(t, tracer.stateSnapshots, token)

			// Geth rolls state back before it emits the child exit callback.
			testChain.State().RevertToSnapshot(childRevision)
			tracer.OnExit(1, nil, 0, test.err, true)
			require.NotContains(t, tracer.stateSnapshots, token)

			reverted, rawReturnData := tracer.revertToStateSnapshot(
				new(big.Int).SetUint64(token),
			)
			require.False(t, reverted)
			require.Nil(t, rawReturnData)
		})
	}
}

func TestCheatCodeSnapshotRejectsUnsafeNestedRewind(t *testing.T) {
	t.Parallel()

	testChain, _ := createChain(t)
	t.Cleanup(testChain.Close)
	tracer := snapshotCheatCodeTracerForTest(t, testChain)
	token := tracer.createStateSnapshot()

	owner := newCheatCodeTracerCallFrame()
	owner.expectRevert = &expectRevertExpectation{remaining: 1}
	target := newCheatCodeTracerCallFrame()
	tracer.onExpectedRevertFrameEnter(target, owner, common.HexToAddress("0x1234"))
	require.True(t, target.isExpectedRevertTarget)
	cheatFrame := newCheatCodeTracerCallFrame()
	tracer.callFrames = []*cheatCodeTracerCallFrame{owner, target, cheatFrame}
	tracer.callDepth = 2

	reverted, rawReturnData := tracer.revertToStateSnapshot(new(big.Int).SetUint64(token))
	require.False(t, reverted)
	require.Nil(t, rawReturnData)
	require.False(t, tracer.expectRevertFailed)
	require.Contains(t, tracer.stateSnapshots, token)
}

func TestCheatCodeSnapshotAllowsSameScopeRewind(t *testing.T) {
	t.Parallel()

	testChain, _ := createChain(t)
	t.Cleanup(testChain.Close)
	tracer := snapshotCheatCodeTracerForTest(t, testChain)
	owner := newCheatCodeTracerCallFrame()
	tracer.callFrames = []*cheatCodeTracerCallFrame{
		owner,
		newCheatCodeTracerCallFrame(),
	}
	tracer.callDepth = 1
	token := tracer.createStateSnapshot()

	tracer.callFrames[1] = newCheatCodeTracerCallFrame()
	reverted, rawReturnData := tracer.revertToStateSnapshot(new(big.Int).SetUint64(token))
	require.True(t, reverted)
	require.Nil(t, rawReturnData)
	require.NotContains(t, tracer.stateSnapshots, token)
}

func TestCheatCodeSnapshotDoesNotRewindAfterExpectedRevertFailure(t *testing.T) {
	t.Parallel()

	testChain, _ := createChain(t)
	t.Cleanup(testChain.Close)
	tracer := snapshotCheatCodeTracerForTest(t, testChain)
	owner := newCheatCodeTracerCallFrame()
	tracer.callFrames = []*cheatCodeTracerCallFrame{
		owner,
		newCheatCodeTracerCallFrame(),
	}
	tracer.callDepth = 1
	token := tracer.createStateSnapshot()

	tracer.callFrames[1] = newCheatCodeTracerCallFrame()
	tracer.expectRevertFailed = true
	reverted, rawReturnData := tracer.revertToStateSnapshot(new(big.Int).SetUint64(token))
	require.False(t, reverted)
	require.Nil(t, rawReturnData)
	require.Contains(t, tracer.stateSnapshots, token)
}

func TestCheatCodeSnapshotRestoresManagedStateThroughHandlers(t *testing.T) {
	t.Parallel()

	testChain, _ := createChain(t)
	t.Cleanup(testChain.Close)
	pendingBlock, err := testChain.PendingBlockCreate()
	require.NoError(t, err)
	pendingContext := newTestChainBlockContext(testChain, pendingBlock.Header)
	testChain.pendingBlockContext = &pendingContext
	testChain.pendingBlockChainConfig = testChain.chainConfig

	tracer := snapshotCheatCodeTracerForTest(t, testChain)
	tracer.callFrames = []*cheatCodeTracerCallFrame{
		newCheatCodeTracerCallFrame(),
		newCheatCodeTracerCallFrame(),
	}
	tracer.callDepth = 1
	contract, err := getStandardCheatCodeContract(tracer)
	require.NoError(t, err)

	originalRandom := common.HexToHash("0x100")
	originalCoinbase := common.HexToAddress("0x200")
	originalLabelAddress := common.HexToAddress("0x300")
	addedLabelAddress := common.HexToAddress("0x400")
	testChain.pendingBlockContext.Time = 10
	testChain.pendingBlockContext.BlockNumber = big.NewInt(20)
	testChain.pendingBlockContext.BaseFee = big.NewInt(30)
	testChain.pendingBlockContext.Random = &originalRandom
	testChain.pendingBlockContext.Coinbase = originalCoinbase
	testChain.pendingBlock.Header.Time = 10
	testChain.pendingBlock.Header.Number = big.NewInt(20)
	testChain.pendingBlock.Header.BaseFee = big.NewInt(30)
	testChain.pendingBlock.Header.Coinbase = originalCoinbase
	testChain.pendingBlockChainConfig.ChainID = big.NewInt(40)
	testChain.Labels[originalLabelAddress] = "before"

	snapshotOutput := runStandardCheatCodeForTest(t, contract, "snapshot()")
	require.Len(t, snapshotOutput, 1)
	snapshotToken := snapshotOutput[0].(*big.Int)

	runStandardCheatCodeForTest(t, contract, "warp(uint256)", big.NewInt(110))
	runStandardCheatCodeForTest(t, contract, "roll(uint256)", big.NewInt(120))
	runStandardCheatCodeForTest(t, contract, "fee(uint256)", big.NewInt(130))
	runStandardCheatCodeForTest(t, contract, "prevrandao(bytes32)", [32]byte{0x14})
	runStandardCheatCodeForTest(t, contract, "coinbase(address)", common.HexToAddress("0x500"))
	runStandardCheatCodeForTest(t, contract, "chainId(uint256)", big.NewInt(140))
	runStandardCheatCodeForTest(t, contract, "label(address,string)", originalLabelAddress, "after")
	runStandardCheatCodeForTest(t, contract, "label(address,string)", addedLabelAddress, "added")

	revertOutput := runStandardCheatCodeForTest(t, contract, "revertTo(uint256)", snapshotToken)
	require.Equal(t, []any{true}, revertOutput)
	require.Equal(t, uint64(10), testChain.pendingBlockContext.Time)
	require.Equal(t, big.NewInt(20), testChain.pendingBlockContext.BlockNumber)
	require.Equal(t, big.NewInt(30), testChain.pendingBlockContext.BaseFee)
	require.NotNil(t, testChain.pendingBlockContext.Random)
	require.Equal(t, originalRandom, *testChain.pendingBlockContext.Random)
	require.Equal(t, originalCoinbase, testChain.pendingBlockContext.Coinbase)
	require.Equal(t, uint64(10), testChain.pendingBlock.Header.Time)
	require.Equal(t, big.NewInt(20), testChain.pendingBlock.Header.Number)
	require.Equal(t, big.NewInt(30), testChain.pendingBlock.Header.BaseFee)
	require.Equal(t, originalCoinbase, testChain.pendingBlock.Header.Coinbase)
	require.Equal(t, big.NewInt(40), testChain.pendingBlockChainConfig.ChainID)
	require.Equal(t, map[common.Address]string{originalLabelAddress: "before"}, testChain.Labels)
}

func runStandardCheatCodeForTest(
	t *testing.T,
	contract *CheatCodeContract,
	signature string,
	arguments ...any,
) []any {
	t.Helper()

	method, ok := contract.Abi().Methods[signature]
	require.True(t, ok)
	packedArguments, err := method.Inputs.Pack(arguments...)
	require.NoError(t, err)
	input := append(append([]byte{}, method.ID...), packedArguments...)
	packedOutput, err := contract.Run(input)
	require.NoError(t, err)
	output, err := method.Outputs.Unpack(packedOutput)
	require.NoError(t, err)
	return output
}

func snapshotCheatCodeTracerForTest(t *testing.T, testChain *TestChain) *cheatCodeTracer {
	t.Helper()

	contract, ok := testChain.CheatCodeContracts()[StandardCheatcodeContractAddress]
	require.True(t, ok)
	require.NotNil(t, contract.tracer)
	return contract.tracer
}
