// Package glob implements flowmap's identity-glob matching, used by the tier-map
// for pins and rules (for example "*ledger#Post" or "*decisioning#Evaluate").
//
// The only metacharacter is '*', which matches any run of characters INCLUDING
// separators such as '/' and '#'. This is deliberately different from
// path.Match / filepath.Match, which stop at '/': an identity glob must be able
// to match a fully-qualified symbol like
// "example.com/loansvc/internal/ledger#Post" with the pattern "*ledger#Post".
package glob

// Match reports whether s matches pattern, where '*' matches any (possibly empty)
// run of characters including separators. All other characters match literally.
// Matching is byte-wise; identities are ASCII. The algorithm is the standard
// linear-time wildcard matcher with backtracking to the most recent '*'.
func Match(pattern, s string) bool {
	sx, px := 0, 0
	starPx, starSx := -1, 0
	for sx < len(s) {
		switch {
		case px < len(pattern) && pattern[px] == '*':
			starPx, starSx = px, sx
			px++
		case px < len(pattern) && pattern[px] == s[sx]:
			px++
			sx++
		case starPx != -1:
			px = starPx + 1
			starSx++
			sx = starSx
		default:
			return false
		}
	}
	for px < len(pattern) && pattern[px] == '*' {
		px++
	}
	return px == len(pattern)
}
