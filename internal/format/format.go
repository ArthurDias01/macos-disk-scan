// Package format renders numbers for the CLI.
//
// A port of shared/format.ts, which the SPA also uses. Two implementations of
// "how big is this" drift the moment one is tweaked, so when this changes the
// TypeScript one changes with it.
package format

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

// Binary units, because that is what Finder and df report on macOS.
var units = []string{"B", "KB", "MB", "GB", "TB", "PB"}

// Bytes renders a byte count at its natural unit.
func Bytes(bytes int64) string {
	if bytes == 0 {
		return "0 B"
	}

	sign := ""
	if bytes < 0 {
		sign = "-"
	}

	value := math.Abs(float64(bytes))
	unit := 0
	for value >= 1024 && unit < len(units)-1 {
		value /= 1024
		unit++
	}

	// One decimal below 10 of a unit, where the difference between 1.2 GB and
	// 1.9 GB matters; none above it, where it is noise.
	digits := 0
	if value < 10 && unit > 0 {
		digits = 1
	}
	return fmt.Sprintf("%s%s %s", sign, strconv.FormatFloat(value, 'f', digits, 64), units[unit])
}

// Count renders a tally with thousands separators.
func Count(count int64) string {
	digits := strconv.FormatInt(count, 10)

	sign := ""
	if strings.HasPrefix(digits, "-") {
		sign, digits = "-", digits[1:]
	}

	var out strings.Builder
	for i, digit := range digits {
		if i > 0 && (len(digits)-i)%3 == 0 {
			out.WriteByte(',')
		}
		out.WriteRune(digit)
	}
	return sign + out.String()
}

// Duration renders an elapsed time at a readable resolution.
func Duration(d time.Duration) string {
	ms := d.Milliseconds()
	if ms < 1000 {
		return fmt.Sprintf("%dms", ms)
	}

	seconds := float64(ms) / 1000
	if seconds < 60 {
		return fmt.Sprintf("%.1fs", seconds)
	}

	minutes := int64(seconds) / 60
	return fmt.Sprintf("%dm %ds", minutes, int64(math.Round(seconds))%60)
}
