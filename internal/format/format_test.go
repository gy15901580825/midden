package format_test

import (
	"testing"
	"time"

	"aism/internal/format"
)

func TestSize(t *testing.T) {
	cases := map[int64]string{217: "217 B", 402 * 1024: "402.0 KB", 1780000: "1.7 MB", 2 << 30: "2.0 GB"}
	for in, want := range cases {
		if got := format.Size(in); got != want {
			t.Errorf("Size(%d)=%q want %q", in, got, want)
		}
	}
}

func TestAgo(t *testing.T) {
	now := time.Date(2026, 7, 3, 12, 0, 0, 0, time.UTC)
	cases := map[string]string{
		"just now": format.Ago(now.Add(-30*time.Second), now),
		"5m ago":   format.Ago(now.Add(-5*time.Minute), now),
		"2h ago":   format.Ago(now.Add(-2*time.Hour), now),
		"3d ago":   format.Ago(now.Add(-3*24*time.Hour), now),
		"—":        format.Ago(time.Time{}, now),
	}
	for want, got := range cases {
		if got != want {
			t.Errorf("want %q got %q", want, got)
		}
	}
}
