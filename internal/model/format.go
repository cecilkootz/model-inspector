package model

import "fmt"

// HumanBytes formats n with binary units.
func HumanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for v := n / unit; v >= unit; v /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.2f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}

// HumanDecimalGB formats n as decimal gigabytes (10^9), for readers
// comparing a figure with a GPU spec sheet.
func HumanDecimalGB(n int64) string {
	return fmt.Sprintf("%.1f GB", float64(n)/1e9)
}

// Comma inserts thousands separators into n.
func Comma(n int64) string {
	s := fmt.Sprintf("%d", n)
	sign := ""
	if n < 0 {
		// -n wraps for MinInt64, which recursed forever.
		sign, s = "-", s[1:]
	}
	return sign + commaDigits(s)
}

func commaDigits(s string) string {
	if len(s) <= 3 {
		return s
	}
	var b []byte
	lead := len(s) % 3
	if lead == 0 {
		lead = 3
	}
	b = append(b, s[:lead]...)
	for i := lead; i < len(s); i += 3 {
		b = append(b, ',')
		b = append(b, s[i:i+3]...)
	}
	return string(b)
}

// HumanCount formats a parameter count (K/M/B/T).
func HumanCount(n int64) string {
	switch {
	case n >= 1e12:
		return fmt.Sprintf("%.2fT", float64(n)/1e12)
	case n >= 1e9:
		return fmt.Sprintf("%.2fB", float64(n)/1e9)
	case n >= 1e6:
		return fmt.Sprintf("%.2fM", float64(n)/1e6)
	case n >= 1e3:
		return fmt.Sprintf("%.1fK", float64(n)/1e3)
	}
	return fmt.Sprintf("%d", n)
}
