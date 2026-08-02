package chain

import (
	"errors"
	"testing"

	"github.com/crytic/medusa-geth/accounts/abi"
	"github.com/crytic/medusa-geth/core"
	"github.com/crytic/medusa-geth/core/vm"
	"github.com/stretchr/testify/require"
)

func TestAssumeCheatCodeResult(t *testing.T) {
	contract, err := getStandardCheatCodeContract(newCheatCodeTracer())
	require.NoError(t, err)

	method := contract.Abi().Methods["assume(bool)"]
	require.Equal(t, []byte{0x4c, 0x63, 0xe5, 0x62}, method.ID)

	trueArgs, err := method.Inputs.Pack(true)
	require.NoError(t, err)
	trueResult, err := contract.Run(append(method.ID, trueArgs...))
	require.NoError(t, err)
	require.Empty(t, trueResult)

	falseArgs, err := method.Inputs.Pack(false)
	require.NoError(t, err)
	falseResult, err := contract.Run(append(method.ID, falseArgs...))
	require.ErrorIs(t, err, vm.ErrExecutionReverted)
	require.Equal(t, []byte(assumeMagicReturnData), falseResult)
	require.True(t, IsAssumeResult(&core.ExecutionResult{
		Err:        err,
		ReturnData: falseResult,
	}))

	require.False(t, IsAssumeResult(nil))
	require.False(t, IsAssumeResult(&core.ExecutionResult{
		Err:        vm.ErrExecutionReverted,
		ReturnData: []byte("assume failed"),
	}))
	require.False(t, IsAssumeResult(&core.ExecutionResult{
		Err:        errors.New("not an EVM revert"),
		ReturnData: []byte(assumeMagicReturnData),
	}))

	stringType, err := abi.NewType("string", "", nil)
	require.NoError(t, err)
	encodedString, err := (abi.Arguments{{Type: stringType}}).Pack(assumeMagicReturnData)
	require.NoError(t, err)
	encodedError := append([]byte{0x08, 0xc3, 0x79, 0xa0}, encodedString...)
	require.False(t, IsAssumeResult(&core.ExecutionResult{
		Err:        vm.ErrExecutionReverted,
		ReturnData: encodedError,
	}))
}
