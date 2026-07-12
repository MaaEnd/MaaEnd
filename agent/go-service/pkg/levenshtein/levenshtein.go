package levenshtein

// Distance 计算两个字符串之间的 Levenshtein 编辑距离。
// 支持多字节字符（rune 级别比较）。
func Distance(a, b string) int {
	runesA := []rune(a)
	runesB := []rune(b)

	lenA := len(runesA)
	lenB := len(runesB)

	prev := make([]int, lenB+1)
	curr := make([]int, lenB+1)

	for j := 0; j <= lenB; j++ {
		prev[j] = j
	}

	for i := 1; i <= lenA; i++ {
		curr[0] = i

		for j := 1; j <= lenB; j++ {
			cost := 0
			if runesA[i-1] != runesB[j-1] {
				cost = 1
			}

			deletion := prev[j] + 1
			insertion := curr[j-1] + 1
			substitution := prev[j-1] + cost

			curr[j] = min(deletion, insertion, substitution)
		}

		prev, curr = curr, prev
	}

	return prev[lenB]
}
