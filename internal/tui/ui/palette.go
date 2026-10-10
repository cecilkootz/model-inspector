package ui

import (
	"strings"
	"unicode"
)

// Match is one palette hit. Marks are rune indexes in Title to emphasise.
type Match struct {
	ID     string
	Title  Safe
	Key    Safe
	Where  Safe
	Score  int
	Marks  []int
	Order  int
	Enable bool
}

// ScoreMatch scores a case-insensitive subsequence.
// Prefix +8, word start +5, consecutive +3, gap −1.
// The second return is false when query is not a subsequence.
func ScoreMatch(query, title string) (int, []int, bool) {
	q := []rune(strings.ToLower(query))
	t := []rune(title)
	low := []rune(strings.ToLower(title))
	if len(q) == 0 {
		return 1, nil, true
	}
	score := 0
	var marks []int
	qi := 0
	prev := -2
	for i, r := range low {
		if qi >= len(q) {
			break
		}
		if r != q[qi] {
			continue
		}
		marks = append(marks, i)
		if i == 0 {
			score += 8
		} else if wordStart(t, i) {
			score += 5
		}
		if i == prev+1 {
			score += 3
		} else if prev >= 0 {
			score -= 1
		}
		prev = i
		qi++
	}
	if qi < len(q) {
		return 0, nil, false
	}
	return score, marks, true
}

func wordStart(t []rune, i int) bool {
	if i <= 0 {
		return true
	}
	prev := t[i-1]
	return prev == ' ' || prev == '-' || prev == '/' || prev == ':' || unicode.IsPunct(prev)
}

// Rank filters and sorts items by score, then registry order.
func Rank(query string, items []Match) []Match {
	var out []Match
	for _, it := range items {
		s, marks, ok := ScoreMatch(query, it.Title.String())
		if !ok {
			continue
		}
		it.Score = s
		it.Marks = marks
		out = append(out, it)
	}
	for i := 1; i < len(out); i++ {
		j := i
		for j > 0 && worse(out[j-1], out[j]) {
			out[j-1], out[j] = out[j], out[j-1]
			j--
		}
	}
	return out
}

func worse(a, b Match) bool {
	if a.Score != b.Score {
		return a.Score < b.Score
	}
	return a.Order > b.Order
}
