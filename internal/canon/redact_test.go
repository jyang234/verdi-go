package canon

import "testing"

// TestPlaceholderRedactsZonelessTimestamp pins that a timestamp WITHOUT an
// explicit timezone is still recognized as a volatile value and templated. A
// zone-less RFC3339-style value (a MySQL DATETIME, a zone-less time.Time format,
// many app logs) is per-run volatile exactly like a zoned one; if it is not
// templated it leaks raw into the canonical IR and two runs over the same flow
// are no longer byte-identical.
func TestPlaceholderRedactsZonelessTimestamp(t *testing.T) {
	ts := []string{
		"2026-06-16T12:00:00",        // zone-less, the gap
		"2026-06-16T12:00:00.123456", // zone-less with fraction
		"2026-06-16 12:00:00",        // MySQL DATETIME (space separator)
		"2026-06-16T12:00:00Z",       // zoned — must still work
		"2026-06-16T12:00:00+02:00",  // offset — must still work
	}
	for _, v := range ts {
		got, ok := placeholder(v)
		if !ok || got != "<ts>" {
			t.Errorf("placeholder(%q) = (%q,%v), want (\"<ts>\", true)", v, got, ok)
		}
	}
	// A bare date with no time component is not a timestamp shape and must not be
	// swept up as one.
	if _, ok := placeholder("2026-06-16"); ok {
		t.Errorf("placeholder(date-only) = true, want false")
	}
}
