package claude

import "testing"

func TestTitleFallbackTruncation(t *testing.T) {
	long := ""
	for i := 0; i < 40; i++ {
		long += "字ab "
	}
	got := cleanTitle(long)
	if r := []rune(got); len(r) != 61 { // 60 + ellipsis
		t.Errorf("want 61 runes, got %d: %q", len(r), got)
	}
}
