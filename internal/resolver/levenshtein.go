package resolver

// ComputeDistance calculates the Levenshtein distance between strings a and b.
// It is Unicode-aware (using []rune) and optimized to use only two rows of
// memory during calculation.
func ComputeDistance(a, b string) int {
	r1 := []rune(a)
	r2 := []rune(b)
	n := len(r1)
	m := len(r2)

	if n == 0 {
		return m
	}
	if m == 0 {
		return n
	}

	// Ensure r1 is the longer string to minimize memory usage for rows.
	if n < m {
		r1, r2 = r2, r1
		n, m = m, n
	}

	prev := make([]int, m+1)
	curr := make([]int, m+1)

	// Initialize the first row (0 to m).
	for j := 0; j <= m; j++ {
		prev[j] = j
	}

	for i := 1; i <= n; i++ {
		curr[0] = i
		for j := 1; j <= m; j++ {
			cost := 1
			if r1[i-1] == r2[j-1] {
				cost = 0
			}

			// Find minimum of (insertion, deletion, substitution).
			// Go 1.21+ builtin min accepts variadic int args.
			curr[j] = min(
				curr[j-1]+1,    // insertion
				prev[j]+1,      // deletion
				prev[j-1]+cost, // substitution
			)
		}
		// Swap rows for the next iteration.
		copy(prev, curr)
	}

	return curr[m]
}
