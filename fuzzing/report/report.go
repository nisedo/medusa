// Package report defines the machine-readable report of a fuzzing campaign: its outcome, metrics, test results with
// structured execution traces, and coverage. The same report describes a campaign which is still running and one
// which has ended, and is what the results file contains.
package report

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/crytic/medusa/fuzzing/calls"
	"github.com/crytic/medusa/fuzzing/coverage"
	"github.com/crytic/medusa/fuzzing/executiontracer"
)

// SchemaVersion is the version of the report format. It is raised whenever a field is removed, renamed or changes
// meaning, so consumers can detect a format they do not understand. Adding a field does not raise it.
const SchemaVersion = 1

// Outcome describes how a campaign is going or how it ended.
type Outcome string

const (
	// OutcomeRunning describes a campaign which has not ended yet.
	OutcomeRunning Outcome = "running"
	// OutcomePassed describes a campaign which ended without any failed test or error.
	OutcomePassed Outcome = "passed"
	// OutcomeFailed describes a campaign which ended with at least one failed test.
	OutcomeFailed Outcome = "failed"
	// OutcomeError describes a campaign which ended because of an error, e.g. a compilation or deployment failure.
	OutcomeError Outcome = "error"
)

// TestStatus describes the state of a test.
type TestStatus string

const (
	// TestStatusNotStarted describes a test whose conditions have not been tested yet.
	TestStatusNotStarted TestStatus = "notStarted"
	// TestStatusRunning describes a test which is being tested and has no result yet.
	TestStatusRunning TestStatus = "running"
	// TestStatusPassed describes a test which passed.
	TestStatusPassed TestStatus = "passed"
	// TestStatusFailed describes a test which failed.
	TestStatusFailed TestStatus = "failed"
)

// TestType describes the kind of a test.
type TestType string

const (
	// TestTypeAssertion describes a test which fails when a method triggers a configured panic (e.g. assert).
	TestTypeAssertion TestType = "assertion"
	// TestTypeProperty describes a test which fails when a property method returns false or reverts.
	TestTypeProperty TestType = "property"
	// TestTypeOptimization describes a test which maximizes the value returned by an optimization method.
	TestTypeOptimization TestType = "optimization"
	// TestTypeCustom describes a test provided through the Go API.
	TestTypeCustom TestType = "custom"
)

// Campaign is the report of a fuzzing campaign.
type Campaign struct {
	// SchemaVersion is the version of the report format, see SchemaVersion.
	SchemaVersion int `json:"schemaVersion"`

	// Outcome describes how the campaign is going or how it ended.
	Outcome Outcome `json:"outcome"`

	// Error is the error the campaign ended with, if Outcome is OutcomeError.
	Error string `json:"error,omitempty"`

	// StartedAt is the time the campaign started.
	StartedAt time.Time `json:"startedAt"`

	// EndedAt is the time the campaign ended. It is not set while the campaign is running.
	EndedAt *time.Time `json:"endedAt,omitempty"`

	// DurationSeconds is how long the campaign has been running, or ran for.
	DurationSeconds float64 `json:"durationSeconds"`

	// Metrics describes how much work the campaign has done.
	Metrics Metrics `json:"metrics"`

	// Tests describes every test of the campaign.
	Tests []Test `json:"tests"`

	// Coverage describes which lines and functions were reached. It is not set if coverage is disabled, or the
	// campaign ended before any coverage could be collected.
	Coverage *coverage.Summary `json:"coverage,omitempty"`
}

// Metrics describes how much work a campaign has done.
type Metrics struct {
	// CallsTested is the number of calls which were executed and tested.
	CallsTested uint64 `json:"callsTested"`

	// SequencesTested is the number of call sequences which were executed and tested.
	SequencesTested uint64 `json:"sequencesTested"`

	// FailedTests is the number of tests which failed and had their call sequence shrunk.
	FailedTests uint64 `json:"failedTests"`

	// AssumeRejects is the number of fuzz inputs rejected by vm.assume.
	AssumeRejects uint64 `json:"assumeRejects"`

	// GasUsed is the total gas used by the calls which were tested.
	GasUsed uint64 `json:"gasUsed"`

	// CorpusSequences is the number of coverage-increasing call sequences in the corpus.
	CorpusSequences int `json:"corpusSequences"`

	// BranchesHit is the number of distinct branches which were executed.
	BranchesHit uint64 `json:"branchesHit"`

	// LastNewCoverageAt is the time a call sequence last reached new code. It is not set if none has since the
	// campaign started. A long time without new coverage suggests the campaign has stalled.
	LastNewCoverageAt *time.Time `json:"lastNewCoverageAt,omitempty"`
}

// Test describes one test of a campaign.
type Test struct {
	// ID is a stable identifier of the test. The same test has the same ID across campaigns.
	ID string `json:"id"`

	// Type is the kind of the test.
	Type TestType `json:"type"`

	// Name is the display name of the test.
	Name string `json:"name"`

	// Contract is the name of the contract the tested method belongs to. It is not set for custom tests.
	Contract string `json:"contract,omitempty"`

	// Method is the signature of the tested method. It is not set for custom tests.
	Method string `json:"method,omitempty"`

	// Status is the state of the test.
	Status TestStatus `json:"status"`

	// Value is the highest value an optimization test has returned, as a decimal string. It is only set for
	// optimization tests which have returned a value.
	Value *string `json:"value,omitempty"`

	// CallSequence is the call sequence which failed the test, or which produced Value for an optimization test.
	CallSequence []Call `json:"callSequence,omitempty"`

	// Trace is the execution trace of the test method itself, run after CallSequence. It is set for property and
	// optimization tests which have a call sequence.
	Trace *executiontracer.ExecutionTrace `json:"trace,omitempty"`

	// Message is the human-readable description of the test result, as printed at the end of a campaign.
	Message string `json:"message"`
}

// Call describes one call of a call sequence. Its fields are those of a corpus call sequence element, so a call
// sequence can be copied from a report into a corpus file, plus the execution trace of the call.
type Call struct {
	*calls.CallSequenceElement

	// Trace is the execution trace of the call, if one was recorded. By default only the last call of a failing
	// call sequence is traced; the verbosity setting controls this.
	Trace *executiontracer.ExecutionTrace `json:"trace,omitempty"`
}

// NewCalls describes the calls of the provided call sequence.
func NewCalls(callSequence calls.CallSequence) []Call {
	sequenceCalls := make([]Call, len(callSequence))
	for i, element := range callSequence {
		sequenceCalls[i] = Call{CallSequenceElement: element, Trace: element.ExecutionTrace}
	}
	return sequenceCalls
}

// WriteFile writes the report as JSON to the provided path, creating its directory if needed. The file is replaced
// in one step, so a reader never sees a partially written report.
func (c *Campaign) WriteFile(path string) error {
	data, err := json.MarshalIndent(c, "", "\t")
	if err != nil {
		return err
	}

	// Write to a temporary file in the same directory, then rename it over the target.
	directory := filepath.Dir(path)
	if err = os.MkdirAll(directory, 0o755); err != nil {
		return err
	}
	temporaryFile, err := os.CreateTemp(directory, "."+filepath.Base(path)+"-*")
	if err != nil {
		return err
	}
	temporaryPath := temporaryFile.Name()
	_, err = temporaryFile.Write(data)
	if closeErr := temporaryFile.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Chmod(temporaryPath, 0o644)
	}
	if err == nil {
		err = os.Rename(temporaryPath, path)
	}
	if err != nil {
		_ = os.Remove(temporaryPath)
	}
	return err
}
