package fuzzing

import (
	"fmt"
	"strings"

	"github.com/crytic/medusa/logging"
	"github.com/crytic/medusa/logging/colors"

	"github.com/crytic/medusa-geth/accounts/abi"
	fuzzerTypes "github.com/crytic/medusa/fuzzing/contracts"
)

// AssertionTestCase describes a test being run by a AssertionTestCaseProvider.
type AssertionTestCase struct {
	// testCaseState holds the status and failing call sequence of the test case.
	testCaseState
	// targetContract describes the target contract where the test case was found
	targetContract *fuzzerTypes.Contract
	// targetMethod describes the target method for the test case
	targetMethod abi.Method
}

// Name describes the name of the test case.
func (t *AssertionTestCase) Name() string {
	return fmt.Sprintf("Assertion Test: %s.%s", t.targetContract.Name(), t.targetMethod.Sig)
}

// LogMessage obtains a buffer that represents the result of the AssertionTestCase. This buffer can be passed to a logger for
// console or file logging.
func (t *AssertionTestCase) LogMessage() *logging.LogBuffer {
	// If the test failed, return a failure message.
	buffer := logging.NewLogBuffer()
	status, callSequence, _ := t.snapshot()
	if status == TestCaseStatusFailed {
		buffer.Append(colors.RedBold, fmt.Sprintf("[%s] ", status), colors.Bold, t.Name(), colors.Reset, "\n")
		buffer.Append(fmt.Sprintf("Test for method \"%s.%s\" resulted in an assertion failure after the following call sequence:\n", t.targetContract.Name(), t.targetMethod.Sig))
		buffer.Append(colors.Bold, "[Call Sequence]", colors.Reset, "\n")
		buffer.Append(callSequence.Log().Elements()...)
		return buffer
	}

	buffer.Append(colors.GreenBold, fmt.Sprintf("[%s] ", status), colors.Bold, t.Name(), colors.Reset)
	return buffer
}

// Message obtains a text-based printable message which describes the result of the AssertionTestCase.
func (t *AssertionTestCase) Message() string {
	// Internally, we just call log message and convert it to a string. This can be useful for 3rd party apps
	return t.LogMessage().String()
}

// ID obtains a unique identifier for a test result.
func (t *AssertionTestCase) ID() string {
	return strings.ReplaceAll(fmt.Sprintf("ASSERTION-%s-%s", t.targetContract.Name(), t.targetMethod.Sig), "_", "-")
}
