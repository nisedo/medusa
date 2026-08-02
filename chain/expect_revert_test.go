package chain

import (
	"bytes"
	"encoding/hex"
	"math/big"
	"strings"
	"testing"

	"github.com/crytic/medusa-geth/common"
	"github.com/crytic/medusa-geth/core/vm"
	"github.com/stretchr/testify/require"
)

type expectRevertRegistrationTestCase struct {
	signature  string
	selector   string
	arguments  []any
	data       []byte
	hasData    bool
	partial    bool
	reverter   *common.Address
	remaining  uint64
	allowCount bool
}

func TestExpectRevertCheatCodeRegistrations(t *testing.T) {
	t.Parallel()

	revertSelector := [4]byte{0xde, 0xad, 0xbe, 0xef}
	revertData := []byte{0xde, 0xad, 0xbe, 0xef, 0x01, 0x02}
	reverter := common.HexToAddress("0x1234")

	tests := []expectRevertRegistrationTestCase{
		{
			signature: "expectRevert()",
			selector:  "f4844814",
			remaining: 1,
		},
		{
			signature: "expectRevert(bytes4)",
			selector:  "c31eb0e0",
			arguments: []any{revertSelector},
			data:      revertSelector[:],
			hasData:   true,
			remaining: 1,
		},
		{
			signature: "expectRevert(bytes)",
			selector:  "f28dceb3",
			arguments: []any{revertData},
			data:      revertData,
			hasData:   true,
			remaining: 1,
		},
		{
			signature: "expectRevert(address)",
			selector:  "d814f38a",
			arguments: []any{reverter},
			reverter:  &reverter,
			remaining: 1,
		},
		{
			signature: "expectRevert(bytes4,address)",
			selector:  "260bc5de",
			arguments: []any{revertSelector, reverter},
			data:      revertSelector[:],
			hasData:   true,
			reverter:  &reverter,
			remaining: 1,
		},
		{
			signature: "expectRevert(bytes,address)",
			selector:  "61ebcf12",
			arguments: []any{revertData, reverter},
			data:      revertData,
			hasData:   true,
			reverter:  &reverter,
			remaining: 1,
		},
		{
			signature:  "expectRevert(uint64)",
			selector:   "4ee38244",
			arguments:  []any{uint64(2)},
			remaining:  2,
			allowCount: true,
		},
		{
			signature:  "expectRevert(bytes4,uint64)",
			selector:   "e45ca72d",
			arguments:  []any{revertSelector, uint64(2)},
			data:       revertSelector[:],
			hasData:    true,
			remaining:  2,
			allowCount: true,
		},
		{
			signature:  "expectRevert(bytes,uint64)",
			selector:   "4994c273",
			arguments:  []any{revertData, uint64(2)},
			data:       revertData,
			hasData:    true,
			remaining:  2,
			allowCount: true,
		},
		{
			signature:  "expectRevert(address,uint64)",
			selector:   "1ff5f952",
			arguments:  []any{reverter, uint64(2)},
			reverter:   &reverter,
			remaining:  2,
			allowCount: true,
		},
		{
			signature:  "expectRevert(bytes4,address,uint64)",
			selector:   "b0762d73",
			arguments:  []any{revertSelector, reverter, uint64(2)},
			data:       revertSelector[:],
			hasData:    true,
			reverter:   &reverter,
			remaining:  2,
			allowCount: true,
		},
		{
			signature:  "expectRevert(bytes,address,uint64)",
			selector:   "d345fb1f",
			arguments:  []any{revertData, reverter, uint64(2)},
			data:       revertData,
			hasData:    true,
			reverter:   &reverter,
			remaining:  2,
			allowCount: true,
		},
		{
			signature: "expectPartialRevert(bytes4)",
			selector:  "11fb5b9c",
			arguments: []any{revertSelector},
			data:      revertSelector[:],
			hasData:   true,
			partial:   true,
			remaining: 1,
		},
		{
			signature: "expectPartialRevert(bytes4,address)",
			selector:  "51aa008a",
			arguments: []any{revertSelector, reverter},
			data:      revertSelector[:],
			hasData:   true,
			partial:   true,
			reverter:  &reverter,
			remaining: 1,
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.signature, func(t *testing.T) {
			t.Parallel()

			tracer := newCheatCodeTracer()
			owner := newCheatCodeTracerCallFrame()
			cheatCodeFrame := newCheatCodeTracerCallFrame()
			tracer.callFrames = []*cheatCodeTracerCallFrame{owner, cheatCodeFrame}
			tracer.callDepth = 1

			contract, err := getStandardCheatCodeContract(tracer)
			require.NoError(t, err)
			method, ok := contract.Abi().Methods[test.signature]
			require.True(t, ok)

			expectedSelector, err := hex.DecodeString(test.selector)
			require.NoError(t, err)
			require.Equal(t, expectedSelector, method.ID)

			packedArguments, err := method.Inputs.Pack(test.arguments...)
			require.NoError(t, err)
			input := append(bytes.Clone(method.ID), packedArguments...)
			output, err := contract.Run(input)
			require.NoError(t, err)
			require.Empty(t, output)

			require.NotNil(t, owner.expectRevert)
			require.Equal(t, test.data, owner.expectRevert.data)
			require.Equal(t, test.hasData, owner.expectRevert.hasData)
			require.Equal(t, test.partial, owner.expectRevert.partial)
			require.Equal(t, test.remaining, owner.expectRevert.remaining)
			require.Equal(t, test.allowCount, owner.expectRevert.allowCount)
			if test.reverter == nil {
				require.Nil(t, owner.expectRevert.reverter)
			} else {
				require.NotNil(t, owner.expectRevert.reverter)
				require.Equal(t, *test.reverter, *owner.expectRevert.reverter)
			}
		})
	}

	contract, err := getStandardCheatCodeContract(newCheatCodeTracer())
	require.NoError(t, err)
	registered := 0
	for signature := range contract.Abi().Methods {
		if strings.HasPrefix(signature, "expectRevert") ||
			strings.HasPrefix(signature, "expectPartialRevert") {
			registered++
		}
	}
	require.Equal(t, len(tests), registered)
}

func TestExpectRevertMatcher(t *testing.T) {
	t.Parallel()

	revertErr := vm.ErrExecutionReverted
	selector := []byte{0xde, 0xad, 0xbe, 0xef}
	otherSelector := []byte{0xca, 0xfe, 0xba, 0xbe}
	errorBoom := encodeExpectRevertStringForTest(t, errorStringSelector, "boom")
	cheatcodeErrorBoom := encodeExpectRevertStringForTest(t, cheatcodeErrorStringSelector, "boom")
	errorOther := encodeExpectRevertStringForTest(t, errorStringSelector, "other")

	tests := []struct {
		name        string
		expectation *expectRevertExpectation
		result      vm.CallFrameResult
		matches     bool
	}{
		{
			name:        "any revert",
			expectation: &expectRevertExpectation{remaining: 1},
			result:      vm.CallFrameResult{Output: otherSelector, Err: revertErr},
			matches:     true,
		},
		{
			name:        "any revert rejects success",
			expectation: &expectRevertExpectation{remaining: 1},
			result:      vm.CallFrameResult{Output: otherSelector},
			matches:     false,
		},
		{
			name: "exact data",
			expectation: &expectRevertExpectation{
				data:      selector,
				hasData:   true,
				remaining: 1,
			},
			result:  vm.CallFrameResult{Output: bytes.Clone(selector), Err: revertErr},
			matches: true,
		},
		{
			name: "exact data rejects suffix",
			expectation: &expectRevertExpectation{
				data:      selector,
				hasData:   true,
				remaining: 1,
			},
			result:  vm.CallFrameResult{Output: append(bytes.Clone(selector), 0x01), Err: revertErr},
			matches: false,
		},
		{
			name: "exact empty data",
			expectation: &expectRevertExpectation{
				data:      []byte{},
				hasData:   true,
				remaining: 1,
			},
			result:  vm.CallFrameResult{Output: []byte{}, Err: revertErr},
			matches: true,
		},
		{
			name: "partial selector",
			expectation: &expectRevertExpectation{
				data:      selector,
				hasData:   true,
				partial:   true,
				remaining: 1,
			},
			result:  vm.CallFrameResult{Output: append(bytes.Clone(selector), 0x01, 0x02), Err: revertErr},
			matches: true,
		},
		{
			name: "partial selector rejects another selector",
			expectation: &expectRevertExpectation{
				data:      selector,
				hasData:   true,
				partial:   true,
				remaining: 1,
			},
			result:  vm.CallFrameResult{Output: otherSelector, Err: revertErr},
			matches: false,
		},
		{
			name: "partial selector requires four actual bytes",
			expectation: &expectRevertExpectation{
				data:      selector,
				hasData:   true,
				partial:   true,
				remaining: 1,
			},
			result:  vm.CallFrameResult{Output: selector[:3], Err: revertErr},
			matches: false,
		},
		{
			name: "raw string matches Error string",
			expectation: &expectRevertExpectation{
				data:      []byte("boom"),
				hasData:   true,
				remaining: 1,
			},
			result:  vm.CallFrameResult{Output: errorBoom, Err: revertErr},
			matches: true,
		},
		{
			name: "raw string matches CheatcodeError string",
			expectation: &expectRevertExpectation{
				data:      []byte("boom"),
				hasData:   true,
				remaining: 1,
			},
			result:  vm.CallFrameResult{Output: cheatcodeErrorBoom, Err: revertErr},
			matches: true,
		},
		{
			name: "Error string matches CheatcodeError string",
			expectation: &expectRevertExpectation{
				data:      errorBoom,
				hasData:   true,
				remaining: 1,
			},
			result:  vm.CallFrameResult{Output: cheatcodeErrorBoom, Err: revertErr},
			matches: true,
		},
		{
			name: "CheatcodeError string matches Error string",
			expectation: &expectRevertExpectation{
				data:      cheatcodeErrorBoom,
				hasData:   true,
				remaining: 1,
			},
			result:  vm.CallFrameResult{Output: errorBoom, Err: revertErr},
			matches: true,
		},
		{
			name: "normalized strings still compare their contents",
			expectation: &expectRevertExpectation{
				data:      errorOther,
				hasData:   true,
				remaining: 1,
			},
			result:  vm.CallFrameResult{Output: cheatcodeErrorBoom, Err: revertErr},
			matches: false,
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, test.matches, test.expectation.matches(test.result, nil))
		})
	}
}

func TestExpectRevertMatcherReverterAndCount(t *testing.T) {
	t.Parallel()

	expectedReverter := common.HexToAddress("0x1234")
	otherReverter := common.HexToAddress("0x5678")
	revertResult := vm.CallFrameResult{Err: vm.ErrExecutionReverted}

	withReverter := &expectRevertExpectation{
		reverter:  &expectedReverter,
		remaining: 1,
	}
	require.True(t, withReverter.matches(revertResult, &expectedReverter))
	require.False(t, withReverter.matches(revertResult, nil))
	require.False(t, withReverter.matches(revertResult, &otherReverter))

	zeroCount := &expectRevertExpectation{
		remaining:  0,
		allowCount: true,
	}
	require.True(t, zeroCount.matches(vm.CallFrameResult{}, nil))
	require.False(t, zeroCount.matches(revertResult, nil))
}

func TestExpectRevertResultPresentationForEveryCallOpcode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		opcode vm.OpCode
		create bool
	}{
		{name: "CALL", opcode: vm.CALL},
		{name: "CALLCODE", opcode: vm.CALLCODE},
		{name: "DELEGATECALL", opcode: vm.DELEGATECALL},
		{name: "STATICCALL", opcode: vm.STATICCALL},
		{name: "CREATE", opcode: vm.CREATE, create: true},
		{name: "CREATE2", opcode: vm.CREATE2, create: true},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			tracer, owner := newTargetedExpectRevertTracerForTest(&expectRevertExpectation{remaining: 1})
			result := tracer.overrideCallFrameResult(
				vm.CallFrameContext{
					Depth:  1,
					OpCode: test.opcode,
					From:   common.HexToAddress("0x100"),
					To:     common.HexToAddress("0x200"),
				},
				vm.CallFrameResult{
					Output:        []byte{0xde, 0xad},
					CreateAddress: common.HexToAddress("0x300"),
					Err:           vm.ErrExecutionReverted,
				},
			)

			require.NoError(t, result.Err)
			require.Nil(t, owner.expectRevert)
			require.False(t, tracer.expectRevertFailed)
			if test.create {
				require.Empty(t, result.Output)
				require.Equal(t, expectRevertDummyCreateAddress, result.CreateAddress)
				return
			}
			require.Len(t, result.Output, expectRevertDummyCallOutputSize)
			require.Equal(t, make([]byte, expectRevertDummyCallOutputSize), result.Output)
			require.Equal(t, common.Address{}, result.CreateAddress)
		})
	}
}

func TestExpectRevertCountZeroAndTwo(t *testing.T) {
	t.Parallel()

	t.Run("zero count preserves successful call result", func(t *testing.T) {
		t.Parallel()

		tracer, owner := newTargetedExpectRevertTracerForTest(&expectRevertExpectation{
			remaining:  0,
			allowCount: true,
		})
		raw := vm.CallFrameResult{
			Output:        []byte{0xaa, 0xbb},
			CreateAddress: common.HexToAddress("0x1234"),
		}
		result := tracer.overrideCallFrameResult(
			vm.CallFrameContext{Depth: 1, OpCode: vm.CALL},
			raw,
		)
		require.Equal(t, raw, result)
		require.Nil(t, owner.expectRevert)
		require.False(t, tracer.expectRevertFailed)
	})

	t.Run("zero count rejects a revert", func(t *testing.T) {
		t.Parallel()

		tracer, owner := newTargetedExpectRevertTracerForTest(&expectRevertExpectation{
			remaining:  0,
			allowCount: true,
		})
		result := tracer.overrideCallFrameResult(
			vm.CallFrameContext{Depth: 1, OpCode: vm.CALL},
			vm.CallFrameResult{Err: vm.ErrExecutionReverted},
		)
		requireExpectRevertAssertionFailureForTest(t, result)
		require.Nil(t, owner.expectRevert)
		require.True(t, tracer.expectRevertFailed)
	})

	t.Run("count two consumes two direct child reverts", func(t *testing.T) {
		t.Parallel()

		owner := newCheatCodeTracerCallFrame()
		owner.expectRevert = &expectRevertExpectation{
			remaining:  2,
			allowCount: true,
		}
		tracer := newCheatCodeTracer()

		for call := 1; call <= 2; call++ {
			target := newCheatCodeTracerCallFrame()
			target.expectRevertOwner = owner
			target.isExpectedRevertTarget = true
			tracer.callFrames = []*cheatCodeTracerCallFrame{owner, target}
			tracer.callDepth = 1

			result := tracer.overrideCallFrameResult(
				vm.CallFrameContext{Depth: 1, OpCode: vm.CALL},
				vm.CallFrameResult{Err: vm.ErrExecutionReverted},
			)
			require.NoError(t, result.Err)
			if call == 1 {
				require.NotNil(t, owner.expectRevert)
				require.Equal(t, uint64(1), owner.expectRevert.remaining)
			} else {
				require.Nil(t, owner.expectRevert)
			}
		}
		require.False(t, tracer.expectRevertFailed)
	})
}

func TestExpectRevertOptionalReverterDirectAndNested(t *testing.T) {
	t.Parallel()

	t.Run("direct reverter", func(t *testing.T) {
		t.Parallel()

		reverter := common.HexToAddress("0x2222")
		tracer, owner := newTargetedExpectRevertTracerForTest(&expectRevertExpectation{
			reverter:  &reverter,
			remaining: 1,
		})
		result := tracer.overrideCallFrameResult(
			vm.CallFrameContext{
				Depth:  1,
				OpCode: vm.CALL,
				From:   common.HexToAddress("0x1111"),
				To:     reverter,
			},
			vm.CallFrameResult{Err: vm.ErrExecutionReverted},
		)
		require.NoError(t, result.Err)
		require.Nil(t, owner.expectRevert)
	})

	t.Run("nested reverter is retained when the target bubbles its failure", func(t *testing.T) {
		t.Parallel()

		targetAddress := common.HexToAddress("0x2222")
		nestedReverter := common.HexToAddress("0x3333")
		revertData := []byte{0xde, 0xad, 0xbe, 0xef}
		tracer, owner := newTargetedExpectRevertTracerForTest(&expectRevertExpectation{
			data:      revertData,
			hasData:   true,
			reverter:  &nestedReverter,
			remaining: 1,
		})
		target := tracer.CurrentCallFrame()
		nested := newCheatCodeTracerCallFrame()
		tracer.callFrames = append(tracer.callFrames, nested)
		tracer.callDepth = 2

		nestedResult := tracer.overrideCallFrameResult(
			vm.CallFrameContext{
				Depth:  2,
				OpCode: vm.CALL,
				From:   targetAddress,
				To:     nestedReverter,
			},
			vm.CallFrameResult{Output: revertData, Err: vm.ErrExecutionReverted},
		)
		require.ErrorIs(t, nestedResult.Err, vm.ErrExecutionReverted)
		require.NotNil(t, target.expectedRevertRevertedBy)
		require.Equal(t, nestedReverter, *target.expectedRevertRevertedBy)

		tracer.callFrames = tracer.callFrames[:2]
		tracer.callDepth = 1
		targetResult := tracer.overrideCallFrameResult(
			vm.CallFrameContext{
				Depth:  1,
				OpCode: vm.CALL,
				From:   common.HexToAddress("0x1111"),
				To:     targetAddress,
			},
			vm.CallFrameResult{Output: revertData, Err: vm.ErrExecutionReverted},
		)
		require.NoError(t, targetResult.Err)
		require.Nil(t, owner.expectRevert)
	})

	t.Run("counted nested reverts consistently use the deepest reverter", func(t *testing.T) {
		t.Parallel()

		targetAddress := common.HexToAddress("0x2222")
		nestedReverter := common.HexToAddress("0x3333")
		owner := newCheatCodeTracerCallFrame()
		owner.expectRevert = &expectRevertExpectation{
			reverter:   &nestedReverter,
			remaining:  2,
			allowCount: true,
		}
		tracer := newCheatCodeTracer()

		for iteration := 0; iteration < 2; iteration++ {
			target := newCheatCodeTracerCallFrame()
			target.expectRevertOwner = owner
			target.isExpectedRevertTarget = true
			nested := newCheatCodeTracerCallFrame()
			tracer.callFrames = []*cheatCodeTracerCallFrame{owner, target, nested}
			tracer.callDepth = 2

			nestedResult := tracer.overrideCallFrameResult(
				vm.CallFrameContext{
					Depth:  2,
					OpCode: vm.CALL,
					From:   targetAddress,
					To:     nestedReverter,
				},
				vm.CallFrameResult{Err: vm.ErrExecutionReverted},
			)
			require.ErrorIs(t, nestedResult.Err, vm.ErrExecutionReverted)

			tracer.callFrames = tracer.callFrames[:2]
			tracer.callDepth = 1
			targetResult := tracer.overrideCallFrameResult(
				vm.CallFrameContext{
					Depth:  1,
					OpCode: vm.CALL,
					From:   common.HexToAddress("0x1111"),
					To:     targetAddress,
				},
				vm.CallFrameResult{Err: vm.ErrExecutionReverted},
			)
			require.NoError(t, targetResult.Err)
		}
		require.Nil(t, owner.expectRevert)
		require.False(t, tracer.expectRevertFailed)
	})

	t.Run("CALLCODE and DELEGATECALL execute at From", func(t *testing.T) {
		t.Parallel()

		executionAddress := common.HexToAddress("0x1111")
		codeAddress := common.HexToAddress("0x2222")
		for _, opcode := range []vm.OpCode{vm.CALLCODE, vm.DELEGATECALL} {
			require.Equal(t, executionAddress, callFrameExecutionAddress(vm.CallFrameContext{
				OpCode: opcode,
				From:   executionAddress,
				To:     codeAddress,
			}))
		}
		require.Equal(t, codeAddress, callFrameExecutionAddress(vm.CallFrameContext{
			OpCode: vm.CALL,
			From:   executionAddress,
			To:     codeAddress,
		}))
	})
}

func TestExpectRevertSelectsTheNextDirectNonCheatChild(t *testing.T) {
	t.Parallel()

	tracer := newCheatCodeTracer()
	rootAddress := common.HexToAddress("0x1000")
	targetAddress := common.HexToAddress("0x2000")
	nestedAddress := common.HexToAddress("0x3000")

	tracer.OnEnter(0, byte(vm.CALL), common.Address{}, rootAddress, nil, 0, big.NewInt(0))
	root := tracer.CurrentCallFrame()
	root.expectRevert = &expectRevertExpectation{remaining: 1}

	for _, cheatAddress := range []common.Address{
		StandardCheatcodeContractAddress,
		ConsoleLogContractAddress,
	} {
		tracer.OnEnter(1, byte(vm.CALL), rootAddress, cheatAddress, nil, 0, big.NewInt(0))
		cheatFrame := tracer.CurrentCallFrame()
		require.False(t, cheatFrame.isExpectedRevertTarget)
		require.Nil(t, cheatFrame.expectRevertOwner)
		tracer.OnExit(1, nil, 0, nil, false)
		require.NotNil(t, root.expectRevert)
	}

	tracer.OnEnter(1, byte(vm.CALL), rootAddress, targetAddress, nil, 0, big.NewInt(0))
	target := tracer.CurrentCallFrame()
	require.True(t, target.isExpectedRevertTarget)
	require.Same(t, root, target.expectRevertOwner)

	tracer.OnEnter(2, byte(vm.CALL), targetAddress, nestedAddress, nil, 0, big.NewInt(0))
	nested := tracer.CurrentCallFrame()
	require.False(t, nested.isExpectedRevertTarget)
	require.Nil(t, nested.expectRevertOwner)

	tracer.OnExit(2, nil, 0, nil, false)
	tracer.OnExit(1, nil, 0, nil, false)
}

func TestExpectRevertOverrideRunsBeforeRawTracerExit(t *testing.T) {
	t.Parallel()

	tracer := newCheatCodeTracer()
	rootAddress := common.HexToAddress("0x1000")
	targetAddress := common.HexToAddress("0x2000")

	tracer.OnEnter(0, byte(vm.CALL), common.Address{}, rootAddress, nil, 0, big.NewInt(0))
	root := tracer.CurrentCallFrame()
	root.expectRevert = &expectRevertExpectation{remaining: 1}
	tracer.OnEnter(1, byte(vm.CALL), rootAddress, targetAddress, nil, 0, big.NewInt(0))
	target := tracer.CurrentCallFrame()
	require.True(t, target.isExpectedRevertTarget)

	// medusa-geth invokes the override while the exiting target is still the
	// tracer's current frame. It then reports the raw revert to OnExit so
	// non-StateDB restore hooks still run.
	presented := tracer.overrideCallFrameResult(
		vm.CallFrameContext{
			Depth:  1,
			OpCode: vm.CALL,
			From:   rootAddress,
			To:     targetAddress,
		},
		vm.CallFrameResult{Output: []byte("raw revert"), Err: vm.ErrExecutionReverted},
	)
	require.NoError(t, presented.Err)
	require.Len(t, presented.Output, expectRevertDummyCallOutputSize)
	require.Nil(t, root.expectRevert)
	require.Same(t, target, tracer.CurrentCallFrame())

	tracer.OnExit(1, []byte("raw revert"), 0, vm.ErrExecutionReverted, true)
	require.Same(t, root, tracer.CurrentCallFrame())
	require.False(t, tracer.expectRevertFailed)
}

func TestExpectRevertFailureRemainsStickyThroughCaughtCallsAndRoot(t *testing.T) {
	t.Parallel()

	root := newCheatCodeTracerCallFrame()
	owner := newCheatCodeTracerCallFrame()
	target := newCheatCodeTracerCallFrame()
	owner.expectRevert = &expectRevertExpectation{remaining: 1}
	target.expectRevertOwner = owner
	target.isExpectedRevertTarget = true

	tracer := newCheatCodeTracer()
	tracer.callFrames = []*cheatCodeTracerCallFrame{root, owner, target}
	tracer.callDepth = 2

	targetResult := tracer.overrideCallFrameResult(
		vm.CallFrameContext{Depth: 2, OpCode: vm.CALL},
		vm.CallFrameResult{},
	)
	requireExpectRevertAssertionFailureForTest(t, targetResult)
	require.True(t, tracer.expectRevertFailed)
	require.Nil(t, owner.expectRevert)

	// Model an intermediate Solidity low-level call or try/catch catching the
	// target's presented failure. The latched failure must still fail that
	// owner frame and then the transaction root.
	tracer.callFrames = tracer.callFrames[:2]
	tracer.callDepth = 1
	ownerResult := tracer.overrideCallFrameResult(
		vm.CallFrameContext{Depth: 1, OpCode: vm.CALL},
		vm.CallFrameResult{Output: []byte("caught")},
	)
	requireExpectRevertAssertionFailureForTest(t, ownerResult)

	tracer.callFrames = tracer.callFrames[:1]
	tracer.callDepth = 0
	rootResult := tracer.overrideCallFrameResult(
		vm.CallFrameContext{Depth: 0, OpCode: vm.CALL},
		vm.CallFrameResult{},
	)
	requireExpectRevertAssertionFailureForTest(t, rootResult)
}

func TestExpectRevertDuplicateDanglingAndTransactionReset(t *testing.T) {
	t.Parallel()

	t.Run("duplicate expectation fails", func(t *testing.T) {
		t.Parallel()

		tracer := newCheatCodeTracer()
		owner := newCheatCodeTracerCallFrame()
		cheatFrame := newCheatCodeTracerCallFrame()
		tracer.callFrames = []*cheatCodeTracerCallFrame{owner, cheatFrame}
		tracer.callDepth = 1

		require.Nil(t, tracer.armExpectRevert(&expectRevertExpectation{remaining: 1}))
		duplicate := tracer.armExpectRevert(&expectRevertExpectation{remaining: 1})
		requireExpectRevertRawAssertionFailureForTest(t, duplicate)
		require.True(t, tracer.expectRevertFailed)
	})

	t.Run("nested expectation fails while an outer expectation is pending", func(t *testing.T) {
		t.Parallel()

		root := newCheatCodeTracerCallFrame()
		root.expectRevert = &expectRevertExpectation{remaining: 1}
		target := newCheatCodeTracerCallFrame()
		cheatFrame := newCheatCodeTracerCallFrame()
		tracer := newCheatCodeTracer()
		tracer.callFrames = []*cheatCodeTracerCallFrame{root, target, cheatFrame}
		tracer.callDepth = 2

		nested := tracer.armExpectRevert(&expectRevertExpectation{remaining: 1})
		requireExpectRevertRawAssertionFailureForTest(t, nested)
		require.Nil(t, target.expectRevert)
		require.NotNil(t, root.expectRevert)
		require.True(t, tracer.expectRevertFailed)
	})

	t.Run("dangling expectation fails its owner", func(t *testing.T) {
		t.Parallel()

		owner := newCheatCodeTracerCallFrame()
		owner.expectRevert = &expectRevertExpectation{remaining: 1}
		tracer := newCheatCodeTracer()
		tracer.callFrames = []*cheatCodeTracerCallFrame{owner}
		tracer.callDepth = 0

		result := tracer.overrideCallFrameResult(
			vm.CallFrameContext{Depth: 0, OpCode: vm.CALL},
			vm.CallFrameResult{},
		)
		requireExpectRevertAssertionFailureForTest(t, result)
		require.Nil(t, owner.expectRevert)
		require.True(t, tracer.expectRevertFailed)
	})

	t.Run("transaction start clears transient expectation state", func(t *testing.T) {
		t.Parallel()

		tracer := newCheatCodeTracer()
		tracer.callDepth = 3
		tracer.callFrames = []*cheatCodeTracerCallFrame{newCheatCodeTracerCallFrame()}
		tracer.expectRevertFailed = true
		tracer.stateSnapshots = map[uint64]*cheatCodeStateSnapshot{
			1: {stateRevision: 7},
		}
		tracer.nextSnapshotToken = 9

		tracer.OnTxStart(nil, nil, common.Address{})

		require.Zero(t, tracer.callDepth)
		require.Empty(t, tracer.callFrames)
		require.False(t, tracer.expectRevertFailed)
		require.Empty(t, tracer.stateSnapshots)
		require.Equal(t, uint64(9), tracer.nextSnapshotToken)
		require.NotNil(t, tracer.results)
	})
}

func encodeExpectRevertStringForTest(t *testing.T, selector [4]byte, value string) []byte {
	t.Helper()

	encoded, err := expectRevertStringArguments.Pack(value)
	require.NoError(t, err)
	return append(bytes.Clone(selector[:]), encoded...)
}

func newTargetedExpectRevertTracerForTest(
	expectation *expectRevertExpectation,
) (*cheatCodeTracer, *cheatCodeTracerCallFrame) {
	owner := newCheatCodeTracerCallFrame()
	owner.expectRevert = expectation
	target := newCheatCodeTracerCallFrame()
	target.expectRevertOwner = owner
	target.isExpectedRevertTarget = true
	tracer := newCheatCodeTracer()
	tracer.callFrames = []*cheatCodeTracerCallFrame{owner, target}
	tracer.callDepth = 1
	return tracer, owner
}

func requireExpectRevertAssertionFailureForTest(t *testing.T, result vm.CallFrameResult) {
	t.Helper()

	expected := cheatCodeAssertionPanic()
	require.ErrorIs(t, result.Err, expected.Err)
	require.Equal(t, expected.ReturnData, result.Output)
}

func requireExpectRevertRawAssertionFailureForTest(t *testing.T, result *cheatCodeRawReturnData) {
	t.Helper()

	expected := cheatCodeAssertionPanic()
	require.NotNil(t, result)
	require.ErrorIs(t, result.Err, expected.Err)
	require.Equal(t, expected.ReturnData, result.ReturnData)
}
