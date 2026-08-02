package coverage

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestShouldExcludeFile(t *testing.T) {
	testCases := []struct {
		name     string
		filePath string
		patterns []string
		expected bool
	}{
		{
			name:     "forge standard library source",
			filePath: "lib/forge-std/src/Vm.sol",
			patterns: []string{"lib/forge-std/**"},
			expected: true,
		},
		{
			name:     "windows path with the default pattern",
			filePath: `lib\forge-std\src\Vm.sol`,
			patterns: []string{"lib/forge-std/**"},
			expected: true,
		},
		{
			name:     "unrelated dependency",
			filePath: "lib/openzeppelin-contracts/contracts/token/ERC20/ERC20.sol",
			patterns: []string{"lib/forge-std/**"},
			expected: false,
		},
		{
			name:     "similarly named dependency",
			filePath: "lib/forge-stdlib/src/Vm.sol",
			patterns: []string{"lib/forge-std/**"},
			expected: false,
		},
		{
			name:     "no exclusions",
			filePath: "lib/forge-std/src/Vm.sol",
			patterns: []string{},
			expected: false,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			if actual := shouldExcludeFile(testCase.filePath, testCase.patterns); actual != testCase.expected {
				t.Fatalf(
					"shouldExcludeFile(%q, %v) = %t, expected %t",
					testCase.filePath,
					testCase.patterns,
					actual,
					testCase.expected,
				)
			}
		})
	}
}

func TestFilterExcludedFiles(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("could not obtain the working directory: %v", err)
	}

	forgeStdPath := filepath.Join(cwd, "lib", "forge-std", "src", "Vm.sol")
	protocolPath := filepath.Join(cwd, "src", "Protocol.sol")
	newSourceAnalysis := func() *SourceAnalysis {
		return &SourceAnalysis{
			Files: map[string]*SourceFileAnalysis{
				forgeStdPath: {
					Path:  forgeStdPath,
					Lines: []*SourceLineAnalysis{{}},
				},
				protocolPath: {
					Path:  protocolPath,
					Lines: []*SourceLineAnalysis{{}},
				},
			},
		}
	}

	t.Run("default removes forge standard library from reports", func(t *testing.T) {
		sourceAnalysis := newSourceAnalysis()
		sourceAnalysis.filterExcludedFiles([]string{"lib/forge-std/**"})

		if _, exists := sourceAnalysis.Files[forgeStdPath]; exists {
			t.Fatalf("forge standard library source %q was not excluded", forgeStdPath)
		}
		if _, exists := sourceAnalysis.Files[protocolPath]; !exists {
			t.Fatalf("protocol source %q was unexpectedly excluded", protocolPath)
		}
		if sourceAnalysis.LineCount() != 1 {
			t.Fatalf("line count = %d, expected 1", sourceAnalysis.LineCount())
		}

		lcovReport := sourceAnalysis.GenerateLCOVReport()
		if strings.Contains(lcovReport, forgeStdPath) {
			t.Fatalf("LCOV report unexpectedly contains %q", forgeStdPath)
		}
		if !strings.Contains(lcovReport, protocolPath) {
			t.Fatalf("LCOV report does not contain %q", protocolPath)
		}
	})

	t.Run("empty exclusions retain forge standard library coverage", func(t *testing.T) {
		sourceAnalysis := newSourceAnalysis()
		sourceAnalysis.filterExcludedFiles([]string{})

		if _, exists := sourceAnalysis.Files[forgeStdPath]; !exists {
			t.Fatalf("forge standard library source %q was unexpectedly excluded", forgeStdPath)
		}
	})
}
