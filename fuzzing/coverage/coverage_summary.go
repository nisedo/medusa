package coverage

import (
	"sort"

	"github.com/crytic/medusa/compilation/types"
)

// Summary is the machine-readable form of a SourceAnalysis: which lines and functions were reached, per source file.
// A line counts as covered if it was executed at all, including by a call which later reverted.
type Summary struct {
	// LinesCovered is the number of executable lines which were executed, across all files.
	LinesCovered int `json:"linesCovered"`

	// LinesTotal is the number of executable lines, across all files.
	LinesTotal int `json:"linesTotal"`

	// Files describes the coverage of each source file, sorted by path.
	Files []FileSummary `json:"files"`
}

// FileSummary describes the coverage of one source file.
type FileSummary struct {
	// Path is the path of the source file.
	Path string `json:"path"`

	// LinesCovered is the number of executable lines which were executed.
	LinesCovered int `json:"linesCovered"`

	// LinesTotal is the number of executable lines.
	LinesTotal int `json:"linesTotal"`

	// UncoveredLines are the executable lines which were never executed.
	UncoveredLines []LineRange `json:"uncoveredLines"`

	// RevertedOnlyLines are the lines which were only ever executed by calls that reverted.
	RevertedOnlyLines []LineRange `json:"revertedOnlyLines"`

	// Functions describes each named function defined in the file, in source order.
	Functions []FunctionSummary `json:"functions"`
}

// FunctionSummary describes the coverage of one function.
type FunctionSummary struct {
	// Name is the name of the function.
	Name string `json:"name"`

	// Lines is the range of lines the function definition spans.
	Lines LineRange `json:"lines"`

	// Reached indicates whether any line of the function was executed.
	Reached bool `json:"reached"`
}

// LineRange is an inclusive range of 1-based line numbers.
type LineRange struct {
	// Start is the first line of the range.
	Start int `json:"start"`

	// End is the last line of the range.
	End int `json:"end"`
}

// Summary returns the machine-readable form of the source analysis.
func (s *SourceAnalysis) Summary() *Summary {
	summary := &Summary{Files: make([]FileSummary, 0, len(s.Files))}
	for _, file := range s.SortedFiles() {
		fileSummary := FileSummary{
			Path:              file.Path,
			LinesCovered:      file.CoveredLineCount(),
			LinesTotal:        file.ActiveLineCount(),
			UncoveredLines:    file.lineRanges(func(line *SourceLineAnalysis) bool { return !line.IsCovered && !line.IsCoveredReverted }),
			RevertedOnlyLines: file.lineRanges(func(line *SourceLineAnalysis) bool { return !line.IsCovered && line.IsCoveredReverted }),
			Functions:         make([]FunctionSummary, 0, len(file.Functions)),
		}
		for _, function := range file.Functions {
			// Constructors, fallback and receive functions have no name.
			if function.Name == "" {
				continue
			}
			lines := file.functionLines(function)
			fileSummary.Functions = append(fileSummary.Functions, FunctionSummary{
				Name:    function.Name,
				Lines:   lines,
				Reached: file.anyLineCovered(lines, true),
			})
		}
		summary.LinesCovered += fileSummary.LinesCovered
		summary.LinesTotal += fileSummary.LinesTotal
		summary.Files = append(summary.Files, fileSummary)
	}
	return summary
}

// lineNumberAt returns the 1-based number of the line which contains the provided byte offset.
func (s *SourceFileAnalysis) lineNumberAt(offset int) int {
	// The line containing the offset is the last one which starts at or before it.
	return sort.Search(len(s.CumulativeOffsetByLine), func(i int) bool {
		return s.CumulativeOffsetByLine[i] > offset
	})
}

// functionLines returns the range of lines the provided function definition spans.
func (s *SourceFileAnalysis) functionLines(function *types.FunctionDefinition) LineRange {
	start := types.GetSrcMapStart(function.Src)
	return LineRange{
		Start: s.lineNumberAt(start),
		End:   s.lineNumberAt(start + types.GetSrcMapLength(function.Src)),
	}
}

// anyLineCovered indicates whether any executable line in the provided range was executed. If includeReverted is
// set, lines which were only executed by calls that reverted count as well.
func (s *SourceFileAnalysis) anyLineCovered(lines LineRange, includeReverted bool) bool {
	for number := max(lines.Start, 1); number <= lines.End && number <= len(s.Lines); number++ {
		line := s.Lines[number-1]
		if line.IsActive && (line.IsCovered || (includeReverted && line.IsCoveredReverted)) {
			return true
		}
	}
	return false
}

// lineRanges returns the ranges of consecutive executable lines which match the provided condition. Lines which
// are not executable (e.g. comments and blank lines) do not split a range.
func (s *SourceFileAnalysis) lineRanges(matches func(line *SourceLineAnalysis) bool) []LineRange {
	ranges := make([]LineRange, 0)
	open := false
	for i, line := range s.Lines {
		if !line.IsActive {
			continue
		}
		number := i + 1
		switch {
		case !matches(line):
			open = false
		case open:
			ranges[len(ranges)-1].End = number
		default:
			ranges = append(ranges, LineRange{Start: number, End: number})
			open = true
		}
	}
	return ranges
}
