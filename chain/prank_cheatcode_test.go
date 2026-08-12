package chain

import (
	"bytes"
	"testing"

	"github.com/crytic/medusa-geth/common"
	"github.com/crytic/medusa-geth/core/vm"
	"github.com/stretchr/testify/require"
)

func TestPrankCallCallerOverride(t *testing.T) {
	original := common.HexToAddress("0x100")
	actor := common.HexToAddress("0xbeef")
	target := common.HexToAddress("0x200")

	t.Run("one-shot skips cheatcodes and is consumed by target call", func(t *testing.T) {
		tracer, contract, owner := newPrankCheatCodeTest(t)
		runPrankCheatCodeForTest(t, contract, "prank(address)", actor)
		require.Len(t, owner.callCallerOverrides, 1)
		require.Len(t, owner.onNextFrameEnterHooks, 1)

		tracer.callFrames = []*cheatCodeTracerCallFrame{owner}
		tracer.callDepth = 0
		require.Equal(t, original, tracer.overrideCallCaller(vm.CallCallerOverrideContext{
			From: original,
			To:   StandardCheatcodeContractAddress,
		}))
		require.Len(t, owner.callCallerOverrides, 1)

		require.Equal(t, actor, tracer.overrideCallCaller(vm.CallCallerOverrideContext{
			From: original,
			To:   target,
		}))
		require.Empty(t, owner.callCallerOverrides)
		require.Equal(t, original, tracer.overrideCallCaller(vm.CallCallerOverrideContext{
			From: original,
			To:   target,
		}))
	})

	t.Run("startPrank persists until stopPrank", func(t *testing.T) {
		tracer, contract, owner := newPrankCheatCodeTest(t)
		runPrankCheatCodeForTest(t, contract, "startPrank(address)", actor)

		tracer.callFrames = []*cheatCodeTracerCallFrame{owner}
		tracer.callDepth = 0
		for range 2 {
			require.Equal(t, actor, tracer.overrideCallCaller(vm.CallCallerOverrideContext{
				From: original,
				To:   target,
			}))
			require.Len(t, owner.callCallerOverrides, 1)
		}

		cheatFrame := newCheatCodeTracerCallFrame()
		tracer.callFrames = []*cheatCodeTracerCallFrame{owner, cheatFrame}
		tracer.callDepth = 1
		runPrankCheatCodeForTest(t, contract, "stopPrank()")

		tracer.callFrames = []*cheatCodeTracerCallFrame{owner}
		tracer.callDepth = 0
		require.Equal(t, original, tracer.overrideCallCaller(vm.CallCallerOverrideContext{
			From: original,
			To:   target,
		}))
		require.Empty(t, owner.callCallerOverrides)
	})

	t.Run("last armed override wins and one-shot is consumed", func(t *testing.T) {
		tracer, contract, owner := newPrankCheatCodeTest(t)
		persistentActor := common.HexToAddress("0xaaaa")
		runPrankCheatCodeForTest(t, contract, "startPrank(address)", persistentActor)

		cheatFrame := newCheatCodeTracerCallFrame()
		tracer.callFrames = []*cheatCodeTracerCallFrame{owner, cheatFrame}
		tracer.callDepth = 1
		runPrankCheatCodeForTest(t, contract, "prank(address)", actor)

		tracer.callFrames = []*cheatCodeTracerCallFrame{owner}
		tracer.callDepth = 0
		require.Equal(t, actor, tracer.overrideCallCaller(vm.CallCallerOverrideContext{
			From: original,
			To:   target,
		}))
		require.Len(t, owner.callCallerOverrides, 1)
		require.Equal(t, persistentActor, tracer.overrideCallCaller(vm.CallCallerOverrideContext{
			From: original,
			To:   target,
		}))
	})
}

func newPrankCheatCodeTest(t *testing.T) (*cheatCodeTracer, *CheatCodeContract, *cheatCodeTracerCallFrame) {
	t.Helper()
	tracer := newCheatCodeTracer()
	owner := newCheatCodeTracerCallFrame()
	cheatFrame := newCheatCodeTracerCallFrame()
	tracer.callFrames = []*cheatCodeTracerCallFrame{owner, cheatFrame}
	tracer.callDepth = 1
	contract, err := getStandardCheatCodeContract(tracer)
	require.NoError(t, err)
	return tracer, contract, owner
}

func runPrankCheatCodeForTest(t *testing.T, contract *CheatCodeContract, signature string, arguments ...any) {
	t.Helper()
	method, ok := contract.Abi().Methods[signature]
	require.True(t, ok)
	packed, err := method.Inputs.Pack(arguments...)
	require.NoError(t, err)
	input := append(bytes.Clone(method.ID), packed...)
	output, err := contract.Run(input)
	require.NoError(t, err)
	require.Empty(t, output)
}
