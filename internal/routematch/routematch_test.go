package routematch

import "testing"

// TestMatch pins the segment-wise tolerance contract shared by the impact route
// lens and the assert entrypoint claim kind. entry is the graph
// entrypoints[].name registration literal; query is the hand-authored side.
func TestMatch(t *testing.T) {
	cases := []struct {
		name  string
		entry string
		query string
		want  bool
	}{
		// Method comparison: case-insensitive, and only when both sides carry one.
		{"method matches case-insensitively", "POST /loans", "post /loans", true},
		{"method mismatch does not match", "POST /loans", "GET /loans", false},
		{"method-less registration matches any queried method", "/transfer", "POST /transfer", true},
		{"method-less query matches a method-bearing registration", "POST /transfer", "/transfer", true},

		// Param wildcards on the registration side.
		{"registration {id} matches observed segment", "GET /loans/{id}", "GET /loans/42", true},
		{"registration :id matches observed segment", "GET /loans/:id", "GET /loans/42", true},
		{"registration <id> matches observed segment", "GET /loans/<id>", "GET /loans/42", true},
		{"registration $id matches observed segment", "GET /loans/$id", "GET /loans/42", true},
		{"registration * matches observed segment", "GET /loans/*", "GET /loans/42", true},
		{"registration ... matches observed segment", "GET /loans/...", "GET /loans/42", true},

		// Param wildcard on the QUERY side too (symmetric).
		{"query {id} matches concrete registration", "GET /loans/42", "GET /loans/{id}", true},

		// Mount-prefix tolerance in both directions (tail alignment).
		{"query carries mount prefix", "GET /loans/{id}/status", "GET /api/v1/loans/8842/status", true},
		{"registration carries fuller path", "GET /api/v1/loans/8842/status", "GET /loans/{id}/status", true},

		// A non-segment-aligned suffix must NOT match.
		{"non-segment-aligned suffix does not match", "/loans", "/v2/loans-archive", false},

		// Bare "/" root behavior.
		{"empty vs empty matches", "GET /", "GET /", true},
		{"empty vs non-empty does not match", "GET /", "GET /loans", false},

		// Consumer-topic names degrade to whole-string equality.
		{"topic exact match", "payment.settled", "payment.settled", true},
		{"different topic fails", "payment.settled", "payment.refunded", false},
		{"topic is not a wildcard for another topic", "payment.settled", "order.created", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Match(tc.entry, tc.query); got != tc.want {
				t.Errorf("Match(%q, %q) = %v, want %v", tc.entry, tc.query, got, tc.want)
			}
		})
	}
}
