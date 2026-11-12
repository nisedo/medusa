package fuzzing

import (
	"math/big"
	"sync/atomic"

	"github.com/crytic/medusa/fuzzing/calls"
	"github.com/crytic/medusa/fuzzing/reverts"
)

// FuzzerMetrics represents a struct tracking metrics for a Fuzzer run. Its methods are safe to call from any goroutine
// while workers are running.
type FuzzerMetrics struct {
	// workerMetrics describes the metrics for each individual worker. It has one slot per worker index, as it
	// corresponds to the indexes in Fuzzer.workers.
	workerMetrics []fuzzerWorkerMetrics
}

// fuzzerWorkerMetrics represents metrics for a single FuzzerWorker instance. Counters are atomic because they are
// written by the worker and read concurrently by the metrics printer.
type fuzzerWorkerMetrics struct {
	// sequencesTested is the amount of sequences of transactions which tests were run against.
	sequencesTested atomic.Uint64

	// failedTests is the amount of test cases this worker finished shrinking a failing call sequence for.
	failedTests atomic.Uint64

	// callsTested is the amount of transactions/calls the fuzzer executed and ran tests against.
	callsTested atomic.Uint64

	// assumeRejects is the amount of fuzz inputs rejected by vm.assume.
	assumeRejects atomic.Uint64

	// revertMetricsChan is the channel for sending revert metrics updates to the revert reporter.
	// Note that the channel can be nil here if revert metrics are not enabled
	revertMetricsChan chan reverts.RevertMetricsUpdate

	// gasUsed is the amount of gas the fuzzer executed and ran tests against.
	gasUsed atomic.Uint64

	// workerStartupCount is the amount of times the worker was generated, or re-generated for this index.
	workerStartupCount atomic.Uint64

	// shrinking indicates whether the fuzzer worker is currently shrinking.
	shrinking atomic.Bool
}

// newFuzzerMetrics obtains a new FuzzerMetrics struct for a given number of workers specified by workerCount.
// An optional channel for sending revert metrics updates to the revert reporter is also provided.
// Returns the new FuzzerMetrics object.
func newFuzzerMetrics(workerCount int, revertMetricsCh chan reverts.RevertMetricsUpdate) *FuzzerMetrics {
	// Create a new metrics struct and return it with as many slots as required.
	metrics := FuzzerMetrics{
		workerMetrics: make([]fuzzerWorkerMetrics, workerCount),
	}
	for i := range metrics.workerMetrics {
		metrics.workerMetrics[i].revertMetricsChan = revertMetricsCh
	}
	return &metrics
}

// sum adds up one counter across all workers.
func (m *FuzzerMetrics) sum(counter func(*fuzzerWorkerMetrics) *atomic.Uint64) *big.Int {
	total := new(big.Int)
	for i := range m.workerMetrics {
		total.Add(total, new(big.Int).SetUint64(counter(&m.workerMetrics[i]).Load()))
	}
	return total
}

// FailedTests returns the number of test cases that failed and finished shrinking across all workers.
func (m *FuzzerMetrics) FailedTests() *big.Int {
	return m.sum(func(w *fuzzerWorkerMetrics) *atomic.Uint64 { return &w.failedTests })
}

// SequencesTested returns the amount of sequences of transactions the fuzzer executed and ran tests against.
func (m *FuzzerMetrics) SequencesTested() *big.Int {
	return m.sum(func(w *fuzzerWorkerMetrics) *atomic.Uint64 { return &w.sequencesTested })
}

// CallsTested returns the amount of transactions/calls the fuzzer executed and ran tests against.
func (m *FuzzerMetrics) CallsTested() *big.Int {
	return m.sum(func(w *fuzzerWorkerMetrics) *atomic.Uint64 { return &w.callsTested })
}

// AssumeRejects returns the number of fuzz inputs rejected by vm.assume across all workers.
func (m *FuzzerMetrics) AssumeRejects() *big.Int {
	return m.sum(func(w *fuzzerWorkerMetrics) *atomic.Uint64 { return &w.assumeRejects })
}

// GasUsed returns the amount of gas used by calls the fuzzer executed and ran tests against.
func (m *FuzzerMetrics) GasUsed() *big.Int {
	return m.sum(func(w *fuzzerWorkerMetrics) *atomic.Uint64 { return &w.gasUsed })
}

// WorkerStartupCount describes the amount of times the worker was spawned for this index. Workers are periodically
// reset.
func (m *FuzzerMetrics) WorkerStartupCount() *big.Int {
	return m.sum(func(w *fuzzerWorkerMetrics) *atomic.Uint64 { return &w.workerStartupCount })
}

// WorkersShrinkingCount returns the amount of workers currently performing shrinking operations.
func (m *FuzzerMetrics) WorkersShrinkingCount() uint64 {
	shrinkingCount := uint64(0)
	for i := range m.workerMetrics {
		if m.workerMetrics[i].shrinking.Load() {
			shrinkingCount++
		}
	}
	return shrinkingCount
}

// updateRevertMetrics updates the revert metrics for the fuzzer worker based on the call sequence element.
func (m *fuzzerWorkerMetrics) updateRevertMetrics(callSequenceElement *calls.CallSequenceElement) {
	// The channel will be nil if revert metrics are not enabled
	if callSequenceElement == nil || m.revertMetricsChan == nil {
		return
	}

	// Calls without a resolved contract or ABI method (e.g. fallback calls or raw calldata) are not tracked.
	if callSequenceElement.Contract == nil || callSequenceElement.Call.DataAbiValues == nil ||
		callSequenceElement.Call.DataAbiValues.Method == nil {
		return
	}

	// Send the revert metrics update to the revert reporter
	m.revertMetricsChan <- reverts.RevertMetricsUpdate{
		ContractName:    callSequenceElement.Contract.Name(),
		FunctionName:    callSequenceElement.Call.DataAbiValues.Method.Name,
		ExecutionResult: callSequenceElement.ChainReference.MessageResults().ExecutionResult,
	}
}
