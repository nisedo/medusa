package coverage

import (
	"math/rand"
	"testing"

	"github.com/crytic/medusa/compilation/types"
	"github.com/stretchr/testify/assert"
)

// filterSourceMapsReference is the straightforward quadratic definition of filterSourceMaps: an entry is dropped if
// its source file is unknown, or if its range fully contains a different range of the same source file.
func filterSourceMapsReference(compilation types.Compilation, sourceMap types.SourceMap) types.SourceMap {
	filteredMap := make(types.SourceMap, 0)
	for i, element := range sourceMap {
		if _, exists := compilation.SourceIdToPath[element.SourceUnitID]; !exists {
			continue
		}
		encapsulatesOtherMapping := false
		for x, other := range sourceMap {
			if i != x && element.SourceUnitID == other.SourceUnitID &&
				(element.Offset != other.Offset || element.Length != other.Length) &&
				other.Offset >= element.Offset && other.Offset+other.Length <= element.Offset+element.Length {
				encapsulatesOtherMapping = true
				break
			}
		}
		if !encapsulatesOtherMapping {
			filteredMap = append(filteredMap, element)
		}
	}
	return filteredMap
}

// TestFilterSourceMapsMatchesReference verifies that filterSourceMaps keeps exactly the entries its quadratic
// definition keeps, in the same order, across randomly generated source maps.
func TestFilterSourceMapsMatchesReference(t *testing.T) {
	t.Parallel()

	compilation := types.Compilation{SourceIdToPath: map[int]string{0: "a.sol", 1: "b.sol"}}
	random := rand.New(rand.NewSource(1))
	for run := 0; run < 500; run++ {
		// Small offsets and lengths make duplicates, nesting, shared starts, shared ends and empty ranges common.
		// Source unit 2 is unknown to the compilation and -1 marks generated code.
		sourceMap := make(types.SourceMap, random.Intn(40))
		for i := range sourceMap {
			sourceMap[i] = types.SourceMapElement{
				Index:        i,
				Offset:       random.Intn(12),
				Length:       random.Intn(8),
				SourceUnitID: random.Intn(4) - 1,
			}
		}

		assert.Equal(t, filterSourceMapsReference(compilation, sourceMap), filterSourceMaps(compilation, sourceMap))
	}
}
