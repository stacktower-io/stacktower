package perm

import "slices"

// Seq returns a slice containing the sequence [0, 1, 2, ..., n-1].
// This is useful for initializing permutation arrays or creating index sequences.
//
// For n <= 0, Seq returns an empty slice.
func Seq(n int) []int {
	result := make([]int, n)
	for i := range result {
		result[i] = i
	}
	return result
}

// Factorial returns n! (n factorial), the product 1 × 2 × ... × n.
// For n <= 1, Factorial returns 1.
//
// This function is useful for calculating the size of the full permutation space.
// Note that factorials grow extremely fast: 13! = 6,227,020,800 exceeds 32-bit int.
func Factorial(n int) int {
	result := 1
	for i := 2; i <= n; i++ {
		result *= i
	}
	return result
}

// Generate returns permutations of [0, 1, ..., n-1] using Heap's algorithm.
//
// If limit > 0, Generate returns at most limit permutations.
// If limit <= 0, Generate returns all n! permutations.
//
// Each returned slice is a separate allocation, safe to modify without affecting others.
//
// Generate handles edge cases gracefully:
//   - n = 0: returns [[]] (one empty permutation)
//   - n = 1: returns [[0]] (one single-element permutation)
//
// For n >= 13, the number of permutations exceeds billions. Always use a limit
// when n is large, or your program will exhaust memory.
//
// Heap's algorithm generates permutations in a non-lexicographic order, but
// efficiently produces each permutation exactly once.
func Generate(n, limit int) [][]int {
	if n == 0 {
		return [][]int{{}}
	}
	if n == 1 {
		return [][]int{{0}}
	}

	perm := Seq(n)
	state := make([]int, n)

	// Calculate capacity: use limit if specified, otherwise factorial (capped at 12!)
	var capacity int
	if limit > 0 {
		// When limit is specified, only allocate for what we need
		maxPossible := Factorial(min(n, 12))
		capacity = min(limit, maxPossible)
	} else {
		// No limit: allocate for all permutations (capped at 12! for safety)
		capacity = Factorial(min(n, 12))
	}
	result := make([][]int, 0, capacity)
	result = append(result, slices.Clone(perm))

	for i := 0; i < n && (limit <= 0 || len(result) < limit); {
		if state[i] < i {
			if i&1 == 0 {
				perm[0], perm[i] = perm[i], perm[0]
			} else {
				perm[state[i]], perm[i] = perm[i], perm[state[i]]
			}
			result = append(result, slices.Clone(perm))
			state[i]++
			i = 0
		} else {
			state[i] = 0
			i++
		}
	}
	return result
}
