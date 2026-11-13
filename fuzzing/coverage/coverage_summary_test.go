package coverage

import (
	"testing"

	"github.com/crytic/medusa/compilation/types"
	"github.com/stretchr/testify/assert"
)

// TestSourceAnalysisSummary verifies the coverage summary of a source file: its line counts, the ranges of lines
// which were never executed or only executed by reverting calls, and which functions were reached.
func TestSourceAnalysisSummary(t *testing.T) {
	t.Parallel()

	// Describe a six line file. Each line is ten bytes long, and line 4 is not executable (e.g. a comment).
	active, covered, reverted := true, true, true
	line := func(isActive bool, isCovered bool, isCoveredReverted bool) *SourceLineAnalysis {
		return &SourceLineAnalysis{IsActive: isActive, IsCovered: isCovered, IsCoveredReverted: isCoveredReverted}
	}
	file := &SourceFileAnalysis{
		Path:                   "a.sol",
		CumulativeOffsetByLine: []int{0, 10, 20, 30, 40, 50},
		Lines: []*SourceLineAnalysis{
			line(active, covered, !reverted),   // 1: executed
			line(active, !covered, reverted),   // 2: only executed by a reverting call
			line(active, !covered, !reverted),  // 3: never executed
			line(!active, !covered, !reverted), // 4: not executable, does not split the range
			line(active, !covered, !reverted),  // 5: never executed
			line(active, !covered, !reverted),  // 6: never executed
		},
		Functions: []*types.FunctionDefinition{
			{Name: "reached", Src: "0:15:0"},      // lines 1-2
			{Name: "revertedOnly", Src: "10:5:0"}, // line 2
			{Name: "unreached", Src: "20:35:0"},   // lines 3-6
			{Name: "", Src: "0:5:0"},              // a constructor, which has no name
		},
	}
	summary := (&SourceAnalysis{Files: map[string]*SourceFileAnalysis{file.Path: file}}).Summary()

	assert.Equal(t, 2, summary.LinesCovered)
	assert.Equal(t, 5, summary.LinesTotal)
	assert.Len(t, summary.Files, 1)
	fileSummary := summary.Files[0]
	assert.Equal(t, "a.sol", fileSummary.Path)
	assert.Equal(t, 2, fileSummary.LinesCovered)
	assert.Equal(t, 5, fileSummary.LinesTotal)
	assert.Equal(t, []LineRange{{Start: 3, End: 6}}, fileSummary.UncoveredLines)
	assert.Equal(t, []LineRange{{Start: 2, End: 2}}, fileSummary.RevertedOnlyLines)
	assert.Equal(t, []FunctionSummary{
		{Name: "reached", Lines: LineRange{Start: 1, End: 2}, Reached: true},
		{Name: "revertedOnly", Lines: LineRange{Start: 2, End: 2}, Reached: true},
		{Name: "unreached", Lines: LineRange{Start: 3, End: 6}, Reached: false},
	}, fileSummary.Functions)
}
