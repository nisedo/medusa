package chain

import (
	"bytes"

	"github.com/crytic/medusa-geth/accounts/abi"
	"github.com/crytic/medusa-geth/common"
	"github.com/crytic/medusa-geth/core/vm"
)

const expectRevertDummyCallOutputSize = 8192

var (
	expectRevertDummyCreateAddress = common.HexToAddress("0x1")
	errorStringSelector            = [4]byte{0x08, 0xc3, 0x79, 0xa0}
	cheatcodeErrorStringSelector   = [4]byte{0xee, 0xaa, 0x9e, 0x6f}
	expectRevertStringArguments    = func() abi.Arguments {
		stringType, err := abi.NewType("string", "", nil)
		if err != nil {
			panic(err)
		}
		return abi.Arguments{{Type: stringType}}
	}()
)

type expectRevertExpectation struct {
	data       []byte
	hasData    bool
	partial    bool
	reverter   *common.Address
	remaining  uint64
	allowCount bool
}

func (e *expectRevertExpectation) clone() *expectRevertExpectation {
	cloned := &expectRevertExpectation{
		data:       bytes.Clone(e.data),
		hasData:    e.hasData,
		partial:    e.partial,
		remaining:  e.remaining,
		allowCount: e.allowCount,
	}
	if e.reverter != nil {
		reverter := *e.reverter
		cloned.reverter = &reverter
	}
	return cloned
}

func (e *expectRevertExpectation) matches(result vm.CallFrameResult, revertedBy *common.Address) bool {
	if e.allowCount && e.remaining == 0 {
		return result.Err == nil
	}
	if result.Err == nil {
		return false
	}
	if e.reverter != nil && (revertedBy == nil || *e.reverter != *revertedBy) {
		return false
	}
	if !e.hasData {
		return true
	}
	if e.partial {
		return len(e.data) >= 4 &&
			len(result.Output) >= 4 &&
			bytes.Equal(e.data[:4], result.Output[:4])
	}
	if bytes.Equal(e.data, result.Output) {
		return true
	}
	if decoded, ok := decodeExpectedRevertString(result.Output); ok && bytes.Equal(e.data, decoded) {
		return true
	}
	if decoded, ok := decodeExpectedRevertString(e.data); ok {
		if actual, actualOK := decodeExpectedRevertString(result.Output); actualOK {
			return bytes.Equal(decoded, actual)
		}
	}
	return false
}

func decodeExpectedRevertString(data []byte) ([]byte, bool) {
	if len(data) < 4 {
		return nil, false
	}
	selector := [4]byte(data[:4])
	if selector != errorStringSelector && selector != cheatcodeErrorStringSelector {
		return nil, false
	}
	values, err := expectRevertStringArguments.Unpack(data[4:])
	if err != nil || len(values) != 1 {
		return nil, false
	}
	value, ok := values[0].(string)
	if !ok {
		return nil, false
	}
	return []byte(value), true
}

func isCheatCodeAddress(address common.Address) bool {
	return address == StandardCheatcodeContractAddress || address == ConsoleLogContractAddress
}

func (t *cheatCodeTracer) armExpectRevert(expectation *expectRevertExpectation) *cheatCodeRawReturnData {
	owner := t.PreviousCallFrame()
	if owner == nil || t.expectRevertFailed || t.hasPendingExpectRevert() {
		t.expectRevertFailed = true
		return cheatCodeAssertionPanic()
	}
	owner.expectRevert = expectation.clone()
	return nil
}

func (t *cheatCodeTracer) hasPendingExpectRevert() bool {
	for _, frame := range t.callFrames {
		if frame.expectRevert != nil {
			return true
		}
	}
	return false
}

func (t *cheatCodeTracer) onExpectedRevertFrameEnter(frame, parent *cheatCodeTracerCallFrame, address common.Address) {
	if parent == nil || parent.expectRevert == nil || isCheatCodeAddress(address) {
		return
	}
	frame.expectRevertOwner = parent
	frame.isExpectedRevertTarget = true
}

func (t *cheatCodeTracer) overrideCallFrameResult(context vm.CallFrameContext, result vm.CallFrameResult) vm.CallFrameResult {
	frame := t.CurrentCallFrame()
	if frame == nil {
		return result
	}

	if result.Err != nil {
		t.recordExpectedReverter(context)
	}

	// A pending expectation on the exiting owner frame never received a
	// non-cheat external call.
	if frame.expectRevert != nil {
		frame.expectRevert = nil
		t.expectRevertFailed = true
	}

	if t.expectRevertFailed {
		if frame.expectRevertOwner != nil {
			frame.expectRevertOwner.expectRevert = nil
		}
		return expectRevertAssertionFailure(result)
	}

	if !frame.isExpectedRevertTarget || frame.expectRevertOwner == nil {
		return result
	}
	expectation := frame.expectRevertOwner.expectRevert
	if expectation == nil {
		return result
	}

	revertedBy := frame.expectedRevertRevertedBy
	if result.Err != nil && revertedBy == nil {
		address := callFrameExecutionAddress(context)
		revertedBy = &address
	}
	if !expectation.matches(result, revertedBy) {
		frame.expectRevertOwner.expectRevert = nil
		t.expectRevertFailed = true
		return expectRevertAssertionFailure(result)
	}

	// A zero count means that the selected call must succeed. Keep its real
	// output and create address unchanged.
	if expectation.allowCount && expectation.remaining == 0 {
		frame.expectRevertOwner.expectRevert = nil
		return result
	}

	expectation.remaining--
	if expectation.remaining == 0 {
		frame.expectRevertOwner.expectRevert = nil
	}
	if context.OpCode == vm.CREATE || context.OpCode == vm.CREATE2 {
		return vm.CallFrameResult{
			Output:        nil,
			CreateAddress: expectRevertDummyCreateAddress,
			Err:           nil,
		}
	}
	return vm.CallFrameResult{
		Output: make([]byte, expectRevertDummyCallOutputSize),
		Err:    nil,
	}
}

func (t *cheatCodeTracer) recordExpectedReverter(context vm.CallFrameContext) {
	for index := len(t.callFrames) - 1; index >= 0; index-- {
		frame := t.callFrames[index]
		if !frame.isExpectedRevertTarget ||
			frame.expectRevertOwner == nil ||
			frame.expectRevertOwner.expectRevert == nil ||
			frame.expectRevertOwner.expectRevert.reverter == nil {
			continue
		}
		if context.Depth >= frame.expectedRevertFailureDepth {
			address := callFrameExecutionAddress(context)
			frame.expectedRevertRevertedBy = &address
			frame.expectedRevertFailureDepth = context.Depth
		}
		return
	}
}

func callFrameExecutionAddress(context vm.CallFrameContext) common.Address {
	if context.OpCode == vm.DELEGATECALL || context.OpCode == vm.CALLCODE {
		return context.From
	}
	return context.To
}

func expectRevertAssertionFailure(result vm.CallFrameResult) vm.CallFrameResult {
	panicResult := cheatCodeAssertionPanic()
	result.Output = bytes.Clone(panicResult.ReturnData)
	result.Err = panicResult.Err
	return result
}

func addExpectRevertCheatCodes(
	contract *CheatCodeContract,
	typeBytes4 abi.Type,
	typeBytes abi.Type,
	typeAddress abi.Type,
	typeUint64 abi.Type,
) {
	handler := func(build func([]any) *expectRevertExpectation) cheatCodeMethodHandler {
		return func(tracer *cheatCodeTracer, inputs []any) ([]any, *cheatCodeRawReturnData) {
			return nil, tracer.armExpectRevert(build(inputs))
		}
	}
	exactData := func(data []byte) *expectRevertExpectation {
		return &expectRevertExpectation{data: data, hasData: true, remaining: 1}
	}
	anyRevert := func() *expectRevertExpectation {
		return &expectRevertExpectation{remaining: 1}
	}
	withReverter := func(expectation *expectRevertExpectation, value any) *expectRevertExpectation {
		reverter := value.(common.Address)
		expectation.reverter = &reverter
		return expectation
	}
	withCount := func(expectation *expectRevertExpectation, value any) *expectRevertExpectation {
		expectation.remaining = value.(uint64)
		expectation.allowCount = true
		return expectation
	}

	contract.addMethod(
		"expectRevert", abi.Arguments{}, abi.Arguments{},
		handler(func([]any) *expectRevertExpectation {
			return anyRevert()
		}),
	)
	contract.addMethod(
		"expectRevert", abi.Arguments{{Type: typeBytes4}}, abi.Arguments{},
		handler(func(inputs []any) *expectRevertExpectation {
			return exactData(expectRevertBytes4(inputs[0]))
		}),
	)
	contract.addMethod(
		"expectRevert", abi.Arguments{{Type: typeBytes}}, abi.Arguments{},
		handler(func(inputs []any) *expectRevertExpectation {
			return exactData(inputs[0].([]byte))
		}),
	)
	contract.addMethod(
		"expectRevert", abi.Arguments{{Type: typeAddress}}, abi.Arguments{},
		handler(func(inputs []any) *expectRevertExpectation {
			return withReverter(anyRevert(), inputs[0])
		}),
	)
	contract.addMethod(
		"expectRevert", abi.Arguments{{Type: typeBytes4}, {Type: typeAddress}}, abi.Arguments{},
		handler(func(inputs []any) *expectRevertExpectation {
			return withReverter(exactData(expectRevertBytes4(inputs[0])), inputs[1])
		}),
	)
	contract.addMethod(
		"expectRevert", abi.Arguments{{Type: typeBytes}, {Type: typeAddress}}, abi.Arguments{},
		handler(func(inputs []any) *expectRevertExpectation {
			return withReverter(exactData(inputs[0].([]byte)), inputs[1])
		}),
	)
	contract.addMethod(
		"expectRevert", abi.Arguments{{Type: typeUint64}}, abi.Arguments{},
		handler(func(inputs []any) *expectRevertExpectation {
			return withCount(anyRevert(), inputs[0])
		}),
	)
	contract.addMethod(
		"expectRevert", abi.Arguments{{Type: typeBytes4}, {Type: typeUint64}}, abi.Arguments{},
		handler(func(inputs []any) *expectRevertExpectation {
			return withCount(exactData(expectRevertBytes4(inputs[0])), inputs[1])
		}),
	)
	contract.addMethod(
		"expectRevert", abi.Arguments{{Type: typeBytes}, {Type: typeUint64}}, abi.Arguments{},
		handler(func(inputs []any) *expectRevertExpectation {
			return withCount(exactData(inputs[0].([]byte)), inputs[1])
		}),
	)
	contract.addMethod(
		"expectRevert", abi.Arguments{{Type: typeAddress}, {Type: typeUint64}}, abi.Arguments{},
		handler(func(inputs []any) *expectRevertExpectation {
			return withCount(withReverter(anyRevert(), inputs[0]), inputs[1])
		}),
	)
	contract.addMethod(
		"expectRevert",
		abi.Arguments{{Type: typeBytes4}, {Type: typeAddress}, {Type: typeUint64}},
		abi.Arguments{},
		handler(func(inputs []any) *expectRevertExpectation {
			return withCount(
				withReverter(exactData(expectRevertBytes4(inputs[0])), inputs[1]),
				inputs[2],
			)
		}),
	)
	contract.addMethod(
		"expectRevert",
		abi.Arguments{{Type: typeBytes}, {Type: typeAddress}, {Type: typeUint64}},
		abi.Arguments{},
		handler(func(inputs []any) *expectRevertExpectation {
			return withCount(
				withReverter(exactData(inputs[0].([]byte)), inputs[1]),
				inputs[2],
			)
		}),
	)
	contract.addMethod(
		"expectPartialRevert", abi.Arguments{{Type: typeBytes4}}, abi.Arguments{},
		handler(func(inputs []any) *expectRevertExpectation {
			expectation := exactData(expectRevertBytes4(inputs[0]))
			expectation.partial = true
			return expectation
		}),
	)
	contract.addMethod(
		"expectPartialRevert", abi.Arguments{{Type: typeBytes4}, {Type: typeAddress}}, abi.Arguments{},
		handler(func(inputs []any) *expectRevertExpectation {
			expectation := exactData(expectRevertBytes4(inputs[0]))
			expectation.partial = true
			return withReverter(expectation, inputs[1])
		}),
	)
}

func expectRevertBytes4(value any) []byte {
	bytes4 := value.([4]byte)
	return bytes4[:]
}
