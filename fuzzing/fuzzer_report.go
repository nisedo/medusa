package fuzzing

import (
	"time"

	"github.com/crytic/medusa/fuzzing/coverage"
	"github.com/crytic/medusa/fuzzing/report"
)

// campaignState tracks when the current fuzzing campaign started and how it ended, for reporting.
type campaignState struct {
	// startedAt is the time the campaign started.
	startedAt time.Time
	// endedAt is the time the campaign ended. It is nil while the campaign is running.
	endedAt *time.Time
	// err is the error the campaign ended with, if any.
	err error
}

// beginCampaign records the start of a new fuzzing campaign.
func (f *Fuzzer) beginCampaign() {
	f.campaignLock.Lock()
	defer f.campaignLock.Unlock()
	f.campaign = campaignState{startedAt: time.Now()}
}

// endCampaign records the end of the current fuzzing campaign and the error it ended with, if any.
func (f *Fuzzer) endCampaign(err error) {
	f.campaignLock.Lock()
	defer f.campaignLock.Unlock()
	endedAt := time.Now()
	f.campaign.endedAt = &endedAt
	f.campaign.err = err
}

// Report returns a machine-readable report of the current fuzzing campaign: its outcome, metrics, test results and
// coverage. It can be called while the campaign is running, from any goroutine, to obtain a snapshot.
// Returns the report, or an error if source coverage could not be analyzed.
func (f *Fuzzer) Report() (*report.Campaign, error) {
	campaignReport := f.ReportWithoutCoverage()
	sourceAnalysis, err := f.SourceCoverage()
	if err != nil {
		return nil, err
	}
	if sourceAnalysis != nil {
		campaignReport.Coverage = sourceAnalysis.Summary()
	}
	return campaignReport, nil
}

// SourceCoverage analyzes which source lines the campaign has covered so far. It can be called while the campaign is
// running, from any goroutine.
// Returns nil if coverage is disabled or the campaign has no corpus yet, or an error if the analysis fails.
func (f *Fuzzer) SourceCoverage() (*coverage.SourceAnalysis, error) {
	f.campaignLock.Lock()
	campaignCorpus := f.corpus
	f.campaignLock.Unlock()
	if !f.config.Fuzzing.CoverageEnabled || campaignCorpus == nil {
		return nil, nil
	}
	return coverage.AnalyzeSourceCoverage(f.compilations, campaignCorpus.CoverageMaps(), f.config.Fuzzing.CoverageExclusions, f.logger)
}

// ReportWithoutCoverage returns the report of the current fuzzing campaign like Report, leaving out coverage, which
// is the costly part to compute. It can be called while the campaign is running, from any goroutine.
func (f *Fuzzer) ReportWithoutCoverage() *report.Campaign {
	f.campaignLock.Lock()
	campaign, metrics, campaignCorpus := f.campaign, f.metrics, f.corpus
	f.campaignLock.Unlock()

	campaignReport := &report.Campaign{
		SchemaVersion: report.SchemaVersion,
		Outcome:       report.OutcomeRunning,
		StartedAt:     campaign.startedAt,
		EndedAt:       campaign.endedAt,
		Tests:         make([]report.Test, 0),
	}

	// Describe every test. The list is copied so the lock is not held while describing them.
	f.testCasesLock.Lock()
	testCases := make([]TestCase, len(f.testCases))
	copy(testCases, f.testCases)
	f.testCasesLock.Unlock()
	anyTestFailed := false
	for _, testCase := range testCases {
		test := describeTestCase(testCase)
		anyTestFailed = anyTestFailed || test.Status == report.TestStatusFailed
		campaignReport.Tests = append(campaignReport.Tests, test)
	}

	// Describe how the campaign is going, or how it ended.
	if campaign.endedAt == nil {
		if !campaign.startedAt.IsZero() {
			campaignReport.DurationSeconds = time.Since(campaign.startedAt).Seconds()
		}
	} else {
		campaignReport.DurationSeconds = campaign.endedAt.Sub(campaign.startedAt).Seconds()
		switch {
		case campaign.err != nil:
			campaignReport.Outcome = report.OutcomeError
			campaignReport.Error = campaign.err.Error()
		case anyTestFailed:
			campaignReport.Outcome = report.OutcomeFailed
		default:
			campaignReport.Outcome = report.OutcomePassed
		}
	}

	// Describe the work done so far. The metrics and corpus do not exist if the campaign ended during setup.
	if metrics != nil {
		campaignReport.Metrics.CallsTested = metrics.CallsTested().Uint64()
		campaignReport.Metrics.SequencesTested = metrics.SequencesTested().Uint64()
		campaignReport.Metrics.FailedTests = metrics.FailedTests().Uint64()
		campaignReport.Metrics.AssumeRejects = metrics.AssumeRejects().Uint64()
		campaignReport.Metrics.GasUsed = metrics.GasUsed().Uint64()
	}
	if campaignCorpus != nil {
		campaignReport.Metrics.CorpusSequences, _ = campaignCorpus.CallSequenceEntryCount()
		campaignReport.Metrics.BranchesHit = campaignCorpus.CoverageMaps().BranchesHit()
		if lastCoverageIncrease := campaignCorpus.LastCoverageIncrease(); !lastCoverageIncrease.IsZero() {
			campaignReport.Metrics.LastNewCoverageAt = &lastCoverageIncrease
		}
	}
	return campaignReport
}

// describeTestCase returns the report of a single test case.
func describeTestCase(testCase TestCase) report.Test {
	test := report.Test{
		ID:      testCase.ID(),
		Type:    report.TestTypeCustom,
		Name:    testCase.Name(),
		Message: testCase.Message(),
	}

	// The built-in test cases are read in one step, so the status, call sequence and trace are consistent with
	// each other even if a worker updates the test case at the same time.
	var state *testCaseState
	switch testCase := testCase.(type) {
	case *AssertionTestCase:
		test.Type = report.TestTypeAssertion
		test.Contract, test.Method = testCase.targetContract.Name(), testCase.targetMethod.Sig
		state = &testCase.testCaseState
	case *PropertyTestCase:
		test.Type = report.TestTypeProperty
		test.Contract, test.Method = testCase.targetContract.Name(), testCase.targetMethod.Sig
		state = &testCase.testCaseState
	case *OptimizationTestCase:
		test.Type = report.TestTypeOptimization
		test.Contract, test.Method = testCase.targetContract.Name(), testCase.targetMethod.Sig
		state = &testCase.testCaseState
	}

	if state != nil {
		status, callSequence, trace := state.snapshot()
		test.Status = reportTestStatus(status)
		if callSequence != nil {
			test.CallSequence = report.NewCalls(*callSequence)
			test.Trace = trace
		}
	} else {
		test.Status = reportTestStatus(testCase.Status())
		if callSequence := testCase.CallSequence(); callSequence != nil {
			test.CallSequence = report.NewCalls(*callSequence)
		}
	}

	// An optimization test only has a value once a call sequence has produced one.
	if optimizationTestCase, ok := testCase.(*OptimizationTestCase); ok && test.CallSequence != nil {
		value := optimizationTestCase.Value().String()
		test.Value = &value
	}
	return test
}

// reportTestStatus converts a TestCaseStatus into the status used in reports.
func reportTestStatus(status TestCaseStatus) report.TestStatus {
	switch status {
	case TestCaseStatusRunning:
		return report.TestStatusRunning
	case TestCaseStatusPassed:
		return report.TestStatusPassed
	case TestCaseStatusFailed:
		return report.TestStatusFailed
	default:
		return report.TestStatusNotStarted
	}
}

// writeResultsFile writes the report of the campaign to the configured results file, if one is configured. The
// provided source analysis is used for the coverage section if it is not nil; otherwise coverage is analyzed here.
// Failures are logged rather than returned, as they should not change the outcome of the campaign.
func (f *Fuzzer) writeResultsFile(sourceAnalysis *coverage.SourceAnalysis) {
	path := f.config.Fuzzing.ResultsFile
	if path == "" {
		return
	}

	campaignReport := f.ReportWithoutCoverage()
	if sourceAnalysis == nil {
		var err error
		sourceAnalysis, err = f.SourceCoverage()
		if err != nil {
			f.logger.Error("Failed to analyze source coverage for the results file", err)
		}
	}
	if sourceAnalysis != nil {
		campaignReport.Coverage = sourceAnalysis.Summary()
	}

	if err := campaignReport.WriteFile(path); err != nil {
		f.logger.Error("Failed to write the results file", err)
		return
	}
	f.logger.Info("Results saved to: ", path)
}
