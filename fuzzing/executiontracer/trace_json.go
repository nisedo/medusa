package executiontracer

import (
	"encoding/json"
	"errors"

	"github.com/crytic/medusa-geth/accounts/abi"
	"github.com/crytic/medusa-geth/common"
	"github.com/crytic/medusa-geth/common/hexutil"
	coreTypes "github.com/crytic/medusa-geth/core/types"
	"github.com/crytic/medusa-geth/core/vm"
	"github.com/crytic/medusa/compilation/abiutils"
	"github.com/crytic/medusa/fuzzing/valuegeneration"
)

// The kinds of call a TraceCall can describe.
const (
	// TraceCallKindCall describes a regular call to a contract.
	TraceCallKindCall = "call"
	// TraceCallKindProxyCall describes a call which executes another address' code (e.g. a delegate call).
	TraceCallKindProxyCall = "proxyCall"
	// TraceCallKindCreation describes a contract creation.
	TraceCallKindCreation = "creation"
)

// The outcomes a TraceCall can have.
const (
	// TraceCallStatusSuccess describes a call which returned without an error.
	TraceCallStatusSuccess = "success"
	// TraceCallStatusRevert describes a call which reverted, with or without a reason.
	TraceCallStatusRevert = "revert"
	// TraceCallStatusPanic describes a call which reverted with a Solidity panic (e.g. a failed assertion).
	TraceCallStatusPanic = "panic"
	// TraceCallStatusError describes a call which failed with any other EVM error (e.g. out of gas).
	TraceCallStatusError = "error"
)

// TraceCall is the machine-readable form of a CallFrame: one EVM call scope, with the calls and events inside it.
type TraceCall struct {
	// Kind is the kind of call: TraceCallKindCall, TraceCallKindProxyCall or TraceCallKindCreation.
	Kind string `json:"kind"`

	// From is the address which made the call.
	From common.Address `json:"from"`

	// To is the address which was called, or the address the contract was created at.
	To common.Address `json:"to"`

	// CodeAddress is the address of the code which was executed. It is only set if it differs from To.
	CodeAddress *common.Address `json:"codeAddress,omitempty"`

	// Contract is the name of the contract whose code was executed, if it could be resolved.
	Contract string `json:"contract,omitempty"`

	// Method is the signature of the method which was called (or "constructor"), if it could be resolved.
	Method string `json:"method,omitempty"`

	// Arguments is a display string of the decoded input arguments, if they could be decoded.
	Arguments *string `json:"arguments,omitempty"`

	// Input is the raw message data the call was made with.
	Input hexutil.Bytes `json:"input"`

	// Value is the amount of wei sent with the call, as a decimal string.
	Value string `json:"value"`

	// Status is the outcome of the call: one of the TraceCallStatus constants.
	Status string `json:"status"`

	// Error describes why the call failed: the panic reason, revert reason, custom error or EVM error. It is empty
	// for a successful call, and for a revert without any reason.
	Error string `json:"error,omitempty"`

	// ReturnValues is a display string of the decoded return values, if the call succeeded and they could be decoded.
	ReturnValues *string `json:"returnValues,omitempty"`

	// Output is the raw data returned by the call.
	Output hexutil.Bytes `json:"output"`

	// SelfDestructed indicates whether the call executed a SELFDESTRUCT operation.
	SelfDestructed bool `json:"selfDestructed,omitempty"`

	// Steps are the calls made and events emitted within this call, in the order they happened.
	Steps []TraceStep `json:"steps,omitempty"`
}

// TraceStep is one thing which happened within a TraceCall. Exactly one of its fields is set.
type TraceStep struct {
	// Call is a call made from within the parent call.
	Call *TraceCall `json:"call,omitempty"`

	// Event is an event emitted by the parent call.
	Event *TraceEvent `json:"event,omitempty"`
}

// TraceEvent is the machine-readable form of an event log emitted during a call.
type TraceEvent struct {
	// Name is the name of the event, if it could be resolved.
	Name string `json:"name,omitempty"`

	// Arguments is a display string of the decoded event arguments, if they could be decoded.
	Arguments *string `json:"arguments,omitempty"`

	// Topics are the raw topics of the event log.
	Topics []common.Hash `json:"topics"`

	// Data is the raw data of the event log.
	Data hexutil.Bytes `json:"data"`
}

// MarshalJSON encodes the execution trace as its top level TraceCall.
func (t *ExecutionTrace) MarshalJSON() ([]byte, error) {
	return json.Marshal(t.Call())
}

// Call returns the machine-readable form of the execution trace: its top level call, with every nested call and
// event. It carries the same information as Log, without any formatting.
// Returns nil if nothing was traced.
func (t *ExecutionTrace) Call() *TraceCall {
	if t == nil || t.TopLevelCallFrame == nil {
		return nil
	}
	return t.traceCall(t.TopLevelCallFrame)
}

// traceCall converts a call frame and everything within it into a TraceCall.
func (t *ExecutionTrace) traceCall(callFrame *CallFrame) *TraceCall {
	call := &TraceCall{
		Kind:           TraceCallKindCall,
		From:           callFrame.SenderAddress,
		To:             callFrame.ToAddress,
		Input:          callFrame.InputData,
		Value:          "0",
		Status:         TraceCallStatusSuccess,
		Output:         callFrame.ReturnData,
		SelfDestructed: callFrame.SelfDestructed,
	}
	if callFrame.CallValue != nil {
		call.Value = callFrame.CallValue.String()
	}
	if callFrame.IsContractCreation() {
		call.Kind = TraceCallKindCreation
	} else if callFrame.IsProxyCall() {
		call.Kind = TraceCallKindProxyCall
		codeAddress := callFrame.CodeAddress
		call.CodeAddress = &codeAddress
	}

	// Resolve the contract and method, then decode the input arguments.
	var method *abi.Method
	if callFrame.CodeContractAbi != nil {
		call.Contract = callFrame.CodeContractName
		if callFrame.IsContractCreation() {
			method = &callFrame.CodeContractAbi.Constructor
			call.Method = "constructor"
		} else if resolvedMethod, err := callFrame.CodeContractAbi.MethodById(callFrame.InputData); err == nil {
			method = resolvedMethod
			call.Method = method.Sig
		}
	}
	if method != nil {
		// Constructor arguments follow the init bytecode, while call arguments follow the 4-byte selector.
		var argumentData []byte
		if callFrame.IsContractCreation() {
			argumentData = callFrame.ConstructorArgsData
		} else if len(callFrame.InputData) >= 4 {
			argumentData = callFrame.InputData[4:]
		}
		call.Arguments = t.decodeArguments(method.Inputs, argumentData)
	}

	// Describe how the call ended.
	if callFrame.ReturnError == nil {
		if method != nil {
			call.ReturnValues = t.decodeArguments(method.Outputs, callFrame.ReturnData)
		}
	} else {
		call.Status, call.Error = t.describeFailure(callFrame)
	}

	// Add every call and event within this call, in order.
	for _, operation := range callFrame.Operations {
		switch operation := operation.(type) {
		case *CallFrame:
			call.Steps = append(call.Steps, TraceStep{Call: t.traceCall(operation)})
		case *coreTypes.Log:
			call.Steps = append(call.Steps, TraceStep{Event: t.traceEvent(callFrame, operation)})
		}
	}
	return call
}

// decodeArguments unpacks ABI encoded data and returns a display string of the values.
// Returns nil if the data could not be decoded.
func (t *ExecutionTrace) decodeArguments(arguments abi.Arguments, data []byte) *string {
	values, err := arguments.Unpack(data)
	if err != nil {
		return nil
	}
	display, err := valuegeneration.EncodeABIArgumentsToString(arguments, values, t.labels)
	if err != nil {
		return nil
	}
	return &display
}

// describeFailure classifies the error a call frame ended with.
// Returns the TraceCallStatus of the call and a description of the error, which is empty for a revert without a reason.
func (t *ExecutionTrace) describeFailure(callFrame *CallFrame) (string, string) {
	// A Solidity panic, e.g. a failed assertion or an arithmetic overflow.
	if panicCode := abiutils.GetSolidityPanicCode(callFrame.ReturnError, callFrame.ReturnData, true); panicCode != nil {
		return TraceCallStatusPanic, abiutils.GetPanicReason(panicCode.Uint64())
	}

	// A revert with a reason string.
	if reason := abiutils.GetSolidityRevertErrorString(callFrame.ReturnError, callFrame.ReturnData); reason != nil {
		return TraceCallStatusRevert, *reason
	}

	// A revert with a custom error.
	customError, customErrorArgs := abiutils.GetSolidityCustomRevertError(callFrame.CodeContractAbi, callFrame.ReturnError, callFrame.ReturnData)
	if customError != nil {
		display, err := valuegeneration.EncodeABIArgumentsToString(customError.Inputs, customErrorArgs, t.labels)
		if err == nil {
			return TraceCallStatusRevert, customError.Name + "(" + display + ")"
		}
	}

	// A revert without a reason.
	if errors.Is(callFrame.ReturnError, vm.ErrExecutionReverted) {
		return TraceCallStatusRevert, ""
	}

	// Any other EVM error.
	return TraceCallStatusError, callFrame.ReturnError.Error()
}

// traceEvent converts an event log emitted by a call frame into a TraceEvent.
func (t *ExecutionTrace) traceEvent(callFrame *CallFrame, eventLog *coreTypes.Log) *TraceEvent {
	traceEvent := &TraceEvent{
		Topics: eventLog.Topics,
		Data:   eventLog.Data,
	}
	if traceEvent.Topics == nil {
		traceEvent.Topics = []common.Hash{}
	}

	// Resolve the event from the contract which emitted it. If that fails, it may come from a library, so try
	// every known contract definition.
	event, values := abiutils.UnpackEventAndValues(callFrame.CodeContractAbi, eventLog)
	if event == nil {
		for _, contract := range t.contractDefinitions {
			event, values = abiutils.UnpackEventAndValues(&contract.CompiledContract().Abi, eventLog)
			if event != nil {
				break
			}
		}
	}
	if event != nil {
		traceEvent.Name = event.Name
		if display, err := valuegeneration.EncodeABIArgumentsToString(event.Inputs, values, t.labels); err == nil {
			traceEvent.Arguments = &display
		}
	}
	return traceEvent
}
