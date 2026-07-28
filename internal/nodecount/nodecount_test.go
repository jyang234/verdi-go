package nodecount

import "testing"

// TestRecordSuffix pins both halves of the disclosure rule: a collapsed count names the
// record multiplicity, and an uncollapsed one says NOTHING (so every duplicate-free
// artifact is byte-identical to one produced before the disclosure existed, and the
// suffix stays a signal rather than noise).
func TestRecordSuffix(t *testing.T) {
	cases := []struct {
		name               string
		functions, records int
		want               string
	}{
		{"collapsed", 5, 7, " (7 instance records)"},
		{"collapsed by one", 1, 2, " (2 instance records)"},
		{"nothing collapsed", 5, 5, ""},
		{"empty graph", 0, 0, ""},
		// records < functions cannot happen (a record list holds at least one record per
		// distinct FQN); a miscounting caller gets silence, never "5 nodes (3 instance
		// records)", which would read as a claim that records were LOST.
		{"impossible undercount", 5, 3, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := RecordSuffix(tc.functions, tc.records); got != tc.want {
				t.Errorf("RecordSuffix(%d, %d) = %q, want %q", tc.functions, tc.records, got, tc.want)
			}
		})
	}
}

// TestRecordSuffixIsPure pins determinism: the suffix is a function of its two ints and
// nothing else, so repeated calls are byte-identical (the artifacts embedding it are
// byte-pinned).
func TestRecordSuffixIsPure(t *testing.T) {
	first := RecordSuffix(5, 7)
	for i := 0; i < 100; i++ {
		if got := RecordSuffix(5, 7); got != first {
			t.Fatalf("call %d returned %q, want the stable %q", i, got, first)
		}
	}
}
