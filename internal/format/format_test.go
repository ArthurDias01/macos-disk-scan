package format

import (
	"testing"
	"time"
)

// Expected values produced by running shared/format.ts, which the SPA uses.
// The CLI and the app print the same numbers, and two implementations of "how
// big is this" drift the moment one is tweaked.
func TestBytesMatchesTheTypeScriptFormatter(t *testing.T) {
	cases := []struct {
		bytes int64
		want  string
	}{
		{0, "0 B"},
		{1, "1 B"},
		{512, "512 B"},
		{1023, "1023 B"},
		{1024, "1.0 KB"},
		{1536, "1.5 KB"},
		// At ten of a unit the decimal stops being informative.
		{10240, "10 KB"},
		{1048576, "1.0 MB"},
		{5033164800, "4.7 GB"},
		{1099511627776, "1.0 TB"},
		{-2048, "-2.0 KB"},
	}

	for _, c := range cases {
		if got := Bytes(c.bytes); got != c.want {
			t.Errorf("Bytes(%d) = %q, want %q", c.bytes, got, c.want)
		}
	}
}

func TestCountMatchesTheTypeScriptFormatter(t *testing.T) {
	cases := []struct {
		count int64
		want  string
	}{
		{0, "0"},
		{1, "1"},
		{999, "999"},
		{1000, "1,000"},
		{1234567, "1,234,567"},
		{-1234, "-1,234"},
	}

	for _, c := range cases {
		if got := Count(c.count); got != c.want {
			t.Errorf("Count(%d) = %q, want %q", c.count, got, c.want)
		}
	}
}

func TestDurationMatchesTheTypeScriptFormatter(t *testing.T) {
	cases := []struct {
		ms   int64
		want string
	}{
		{0, "0ms"},
		{999, "999ms"},
		{1000, "1.0s"},
		{1500, "1.5s"},
		{59999, "60.0s"},
		{60000, "1m 0s"},
		{98000, "1m 38s"},
		{3661000, "61m 1s"},
	}

	for _, c := range cases {
		got := Duration(time.Duration(c.ms) * time.Millisecond)
		if got != c.want {
			t.Errorf("Duration(%dms) = %q, want %q", c.ms, got, c.want)
		}
	}
}
