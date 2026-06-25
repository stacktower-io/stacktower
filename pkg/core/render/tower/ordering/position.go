package ordering

import (
	"slices"
)

func medianPosition(pos []int) (float64, bool) {
	if len(pos) == 0 {
		return 0, false
	}
	sorted := slices.Clone(pos)
	slices.Sort(sorted)
	n := len(sorted)
	if n&1 == 0 {
		// Mean of the two middle positions: using only the left median
		// systematically biases even-degree nodes leftward.
		return float64(sorted[n/2-1]+sorted[n/2]) / 2.0, true
	}
	return float64(sorted[n/2]), true
}
