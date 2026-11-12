package fuzzing

import (
	"fmt"
	"math/big"
	"strings"

	"github.com/crytic/medusa-geth/accounts/abi"
	"github.com/crytic/medusa/fuzzing/calls"
	"github.com/crytic/medusa/fuzzing/contracts"
	"github.com/crytic/medusa/fuzzing/executiontracer"
	"github.com/crytic/medusa/logging"
	"github.com/crytic/medusa/logging/colors"
)

// OptimizationTestCase describes a test being run by a OptimizationTestCaseProvider.
type OptimizationTestCase struct {
	// testCaseState holds the status, the call sequence that maximized the value, and the execution trace of the
	// optimization test after that call sequence. Its lock also guards value and shrinkCallSequenceRequest.
	testCaseState
	// targetContract describes the target contract where the test case was found
	targetContract *contracts.Contract
	// targetMethod describes the target method for the test case
	targetMethod abi.Method
	// shrinkCallSequenceRequest is the shrink request that will be executed to identify the optimal call sequence
	// that maximizes the value
	shrinkCallSequenceRequest *ShrinkCallSequenceRequest
	// value is used to store the maximum value returned by the test method
	value *big.Int
}

// Name describes the name of the test case.
func (t *OptimizationTestCase) Name() string {
	return fmt.Sprintf("Optimization Test: %s.%s", t.targetContract.Name(), t.targetMethod.Sig)
}

// LogMessage obtains a buffer that represents the result of the OptimizationTestCase. This buffer can be passed to a logger for
// console or file logging.
func (t *OptimizationTestCase) LogMessage() *logging.LogBuffer {
	buffer := logging.NewLogBuffer()
	status, callSequence, trace := t.snapshot()
	value := t.Value()

	// A failed test case never found a value greater than the minimum, so there is no call sequence or trace to show.
	if status == TestCaseStatusFailed {
		buffer.Append(colors.RedBold, fmt.Sprintf("[%s] ", status), colors.Bold, t.Name(), colors.Reset, "\n")
		buffer.Append(fmt.Sprintf("Test for method \"%s.%s\" failed to identify a value greater than the minimum"+
			" value of an int256: ", t.targetContract.Name(), t.targetMethod.Sig))
		return buffer
	}

	// If no call sequence has produced a value yet, just log the status and name of the test case.
	buffer.Append(colors.GreenBold, fmt.Sprintf("[%s] ", status), colors.Bold, t.Name(), colors.Reset, "\n")
	if callSequence == nil {
		return buffer
	}

	// Otherwise report the maximum value found so far and the call sequence which produced it.
	buffer.Append(fmt.Sprintf("Test for method \"%s.%s\" resulted in the maximum value: ", t.targetContract.Name(), t.targetMethod.Sig))
	buffer.Append(colors.Bold, value, colors.Reset, "\n")
	buffer.Append(colors.Bold, "[Call Sequence]", colors.Reset, "\n")
	buffer.Append(callSequence.Log().Elements()...)

	// If an execution trace is attached then add it to the message
	if trace != nil {
		buffer.Append(colors.Bold, "[Optimization Test Execution Trace]", colors.Reset, "\n")
		buffer.Append(trace.Log().Elements()...)
	}
	return buffer
}

// Message obtains a text-based printable message which describes the result of the OptimizationTestCase.
func (t *OptimizationTestCase) Message() string {
	// Internally, we just call log message and convert it to a string. This can be useful for 3rd party apps
	return t.LogMessage().String()
}

// ID obtains a unique identifier for a test result.
func (t *OptimizationTestCase) ID() string {
	return strings.ReplaceAll(fmt.Sprintf("OPTIMIZATION-%s-%s", t.targetContract.Name(), t.targetMethod.Sig), "_", "-")
}

// Value obtains the maximum value returned by the test method found till now
func (t *OptimizationTestCase) Value() *big.Int {
	t.lock.Lock()
	defer t.lock.Unlock()
	return new(big.Int).Set(t.value)
}

// exceedsValue indicates whether the provided value is greater than the maximum value found till now.
func (t *OptimizationTestCase) exceedsValue(value *big.Int) bool {
	t.lock.Lock()
	defer t.lock.Unlock()
	return value.Cmp(t.value) > 0
}

// recordHigherValue records a new maximum value, the call sequence which produced it and the request to shrink that
// call sequence. Nothing is recorded if another worker recorded an equal or higher value in the meantime.
// Returns true if the value was recorded.
func (t *OptimizationTestCase) recordHigherValue(value *big.Int, callSequence calls.CallSequence, shrinkRequest *ShrinkCallSequenceRequest) bool {
	t.lock.Lock()
	defer t.lock.Unlock()
	if value.Cmp(t.value) <= 0 {
		return false
	}
	t.value = new(big.Int).Set(value)
	t.callSequence = &callSequence
	t.trace = nil
	t.shrinkCallSequenceRequest = shrinkRequest
	return true
}

// recordShrunkSequence records the shrunken call sequence, the execution trace of the optimization test after it, and
// the value it produced, which may be higher than the value of the sequence that was shrunk.
func (t *OptimizationTestCase) recordShrunkSequence(callSequence calls.CallSequence, trace *executiontracer.ExecutionTrace, value *big.Int) {
	t.lock.Lock()
	defer t.lock.Unlock()
	t.callSequence = &callSequence
	t.trace = trace
	if value.Cmp(t.value) > 0 {
		t.value = new(big.Int).Set(value)
	}
}

// takeShrinkRequest returns the pending shrink request, if any, and clears it so it is only handled once.
func (t *OptimizationTestCase) takeShrinkRequest() *ShrinkCallSequenceRequest {
	t.lock.Lock()
	defer t.lock.Unlock()
	request := t.shrinkCallSequenceRequest
	t.shrinkCallSequenceRequest = nil
	return request
}

// finalize settles the status of a test case that was running when the campaign ended: it failed if no value greater
// than the minimum int256 was ever found, and passed otherwise.
func (t *OptimizationTestCase) finalize() {
	t.lock.Lock()
	defer t.lock.Unlock()
	if t.status != TestCaseStatusRunning {
		return
	}
	if t.callSequence == nil {
		t.status = TestCaseStatusFailed
	} else {
		t.status = TestCaseStatusPassed
	}
}
