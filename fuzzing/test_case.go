package fuzzing

import (
	"sync"

	"github.com/crytic/medusa/fuzzing/calls"
	"github.com/crytic/medusa/fuzzing/executiontracer"
	"github.com/crytic/medusa/logging"
)

// TestCaseStatus defines the status of a TestCase as a string-represented enum.
type TestCaseStatus string

const (
	// TestCaseStatusNotStarted describes a test status where conditions have not yet been tested.
	TestCaseStatusNotStarted TestCaseStatus = "NOT STARTED"
	// TestCaseStatusRunning describes a test status where conditions have been tested for but no result
	// has been reported.
	TestCaseStatusRunning TestCaseStatus = "RUNNING"
	// TestCaseStatusPassed describes a test status where testing has concluded and the test passed.
	TestCaseStatusPassed TestCaseStatus = "PASSED"
	// TestCaseStatusFailed describes a test status where testing has concluded and the test failed.
	TestCaseStatusFailed TestCaseStatus = "FAILED"
)

// TestCase describes a test which is being conducted by a test provider attached to the Fuzzer.
type TestCase interface {
	// Status describes the TestCaseStatus used to define the current state of the test.
	Status() TestCaseStatus

	// CallSequence describes the types.CallSequence of calls sent to the EVM which resulted in this TestCase result.
	// This should be nil if the result is not related to the CallSequence.
	CallSequence() *calls.CallSequence

	// Name describes the name of the test case.
	Name() string

	// LogMessage obtains a logging.LogBuffer that represents the result of the TestCase. This buffer can be passed to a logger for
	// console or file logging.
	LogMessage() *logging.LogBuffer

	// Message obtains a text-based printable message which describes the result of the TestCase.
	Message() string

	// ID obtains a unique identifier for a test result. If the same test fails, this ID should match for both
	// TestCase instances (even if the CallSequence differs or has not been shrunk).
	ID() string
}

// testCaseState holds the mutable state shared by the built-in TestCase types. Workers update a test case while other
// goroutines (other workers, the fuzzer, API users) read it, so every access goes through its lock.
type testCaseState struct {
	// lock guards every field below, and any additional mutable field of the embedding test case.
	lock sync.Mutex
	// status describes the status of the test case
	status TestCaseStatus
	// callSequence describes the call sequence which produced the test case's result, if any.
	callSequence *calls.CallSequence
	// trace describes the execution trace of the test when run after callSequence, if one was recorded.
	trace *executiontracer.ExecutionTrace
	// failureClaimed indicates a worker found a call sequence which fails the test and is shrinking it. The status
	// only becomes failed once shrinking finishes, so this stops other workers from shrinking the same failure.
	failureClaimed bool
}

// Status describes the TestCaseStatus used to define the current state of the test.
func (s *testCaseState) Status() TestCaseStatus {
	s.lock.Lock()
	defer s.lock.Unlock()
	return s.status
}

// CallSequence describes the calls.CallSequence of calls sent to the EVM which resulted in this TestCase result.
// This should be nil if the result is not related to the CallSequence.
func (s *testCaseState) CallSequence() *calls.CallSequence {
	s.lock.Lock()
	defer s.lock.Unlock()
	return s.callSequence
}

// transitionStatus sets the status to the provided one if the current status is from.
// Returns true if the status was changed.
func (s *testCaseState) transitionStatus(from TestCaseStatus, to TestCaseStatus) bool {
	s.lock.Lock()
	defer s.lock.Unlock()
	if s.status != from {
		return false
	}
	s.status = to
	return true
}

// failureFound indicates whether a call sequence which fails the test has been found, whether or not it has finished
// shrinking.
func (s *testCaseState) failureFound() bool {
	s.lock.Lock()
	defer s.lock.Unlock()
	return s.failureClaimed || s.status == TestCaseStatusFailed
}

// claimFailure claims the failure of the test for the calling worker, which then shrinks the failing call sequence
// and calls setFailed with the result. Only one worker can claim the failure of a test.
// Returns true if the caller claimed the failure, or false if another worker already did.
func (s *testCaseState) claimFailure() bool {
	s.lock.Lock()
	defer s.lock.Unlock()
	if s.failureClaimed || s.status == TestCaseStatusFailed {
		return false
	}
	s.failureClaimed = true
	return true
}

// setFailed marks the test case as failed with the call sequence that failed it, and an optional execution trace.
func (s *testCaseState) setFailed(callSequence calls.CallSequence, trace *executiontracer.ExecutionTrace) {
	s.lock.Lock()
	defer s.lock.Unlock()
	s.status = TestCaseStatusFailed
	s.callSequence = &callSequence
	s.trace = trace
}

// snapshot returns a consistent view of the status, call sequence and execution trace.
func (s *testCaseState) snapshot() (TestCaseStatus, *calls.CallSequence, *executiontracer.ExecutionTrace) {
	s.lock.Lock()
	defer s.lock.Unlock()
	return s.status, s.callSequence, s.trace
}
