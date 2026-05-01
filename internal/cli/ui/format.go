package ui

import (
	"fmt"
	"time"
)

// FormatCount formats a number with K/M suffixes for readability.
//
// Examples:
//
//	FormatCount(42)      → "42"
//	FormatCount(1_500)   → "1.5K"
//	FormatCount(25_000)  → "25.0K"
//	FormatCount(1_750_000) → "1.8M"
func FormatCount(n int) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	case n >= 1_000:
		return fmt.Sprintf("%.1fK", float64(n)/1_000)
	default:
		return fmt.Sprintf("%d", n)
	}
}

// FormatBytes formats a byte count using IEC binary units (KB = 1024 B, etc.).
//
// Examples:
//
//	FormatBytes(512)           → "512 B"
//	FormatBytes(2048)          → "2.0 KB"
//	FormatBytes(5 * 1 << 20)   → "5.0 MB"
func FormatBytes(b int64) string {
	switch {
	case b >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(b)/(1<<30))
	case b >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(b)/(1<<20))
	case b >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(b)/(1<<10))
	default:
		return fmt.Sprintf("%d B", b)
	}
}

// FormatAge returns a coarse human-friendly "X minutes/hours/days ago"
// label for the given timestamp.
func FormatAge(t time.Time) string {
	age := time.Since(t)
	switch {
	case age < time.Hour:
		return fmt.Sprintf("%d minutes ago", int(age.Minutes()))
	case age < 24*time.Hour:
		return fmt.Sprintf("%d hours ago", int(age.Hours()))
	default:
		return fmt.Sprintf("%d days ago", int(age.Hours()/24))
	}
}
