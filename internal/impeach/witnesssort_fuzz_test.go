package impeach

import (
	"sort"
	"strings"
	"testing"

	"github.com/jyang234/golang-code-graph/ir"
)

// FuzzWitnessSortStrictWeakOrder pins the determinism the candidate sort rests on as a
// SELF-CHECKING invariant rather than a property of any committed fixture (tenet 5;
// CLAUDE.md "new ordering paths ship with a determinism test or a fuzz"). lessWitness
// is the witness sort's comparator (§5); Go's sort.Slice requires it to be a STRICT
// WEAK ORDERING, and a deterministic, arrival-order-independent candidate order
// requires it to be a strict TOTAL order over the witness identity tuple (Effect,
// Flow, Service, Entry, Op, pathSig). A pair of DISTINCT-identity witnesses that
// compares "equal" (neither less) would be ordered by arrival — the §12.5
// non-determinism the dedup exists to prevent. Until now this was exercised only by
// hand-picked fixtures (TestSeverancePathsDistinctByTag, the multi-DELETE corpus).
//
// Over GENERATED witnesses the fuzz asserts the four axioms that characterize a strict
// weak ordering — irreflexivity, asymmetry, transitivity, and transitivity of
// incomparability — plus totality on distinct identities (incomparable ⟺ identical
// identity). It then asserts permutation-invariance of the PRODUCTION sort (sort.Slice,
// non-stable) on inputs whose identities are all distinct — the invariant the dedup
// guarantees in production, and the end-user property the whole exercise is about.
func FuzzWitnessSortStrictWeakOrder(f *testing.F) {
	// Two witnesses identical but for the path (the deepest tie-break key, pathSig).
	f.Add("db DELETE ledger\tPOST /x\tsvc\tHTTP\tDB DELETE\tp\n" +
		"db DELETE ledger\tPOST /x\tsvc\tHTTP\tDB DELETE\tq")
	// A spread that ties on various prefixes of the key tuple.
	f.Add("a\tb\tc\td\te\tf\nA\tb\tc\td\te\tf\na\tB\tc\td\te\tf\na\tb\tc\td\te\tg")
	f.Fuzz(func(t *testing.T, raw string) {
		ws := decodeFuzzWitnesses(raw)
		if len(ws) < 2 {
			return
		}
		id := func(w Witness) [6]string {
			return [6]string{w.Effect, w.Observed.Flow, w.Observed.Service, w.Observed.Entry, w.Observed.Op, pathSig(w.chain)}
		}
		incomparable := func(a, b Witness) bool { return !lessWitness(a, b) && !lessWitness(b, a) }

		for _, a := range ws {
			if lessWitness(a, a) { // irreflexivity
				t.Fatalf("irreflexivity: less(a,a) true for %v", id(a))
			}
		}
		for _, a := range ws {
			for _, b := range ws {
				ab, ba := lessWitness(a, b), lessWitness(b, a)
				if ab && ba { // asymmetry
					t.Fatalf("asymmetry: less both ways: %v vs %v", id(a), id(b))
				}
				switch idA, idB := id(a), id(b); {
				case idA != idB && !ab && !ba:
					// distinct identities MUST be comparable, else the non-stable sort
					// orders them by arrival (the §12.5 non-determinism).
					t.Fatalf("totality: distinct identities incomparable: %v vs %v", idA, idB)
				case idA == idB && (ab || ba):
					t.Fatalf("equal identities must compare equal: %v vs %v", idA, idB)
				}
			}
		}
		for _, a := range ws {
			for _, b := range ws {
				for _, c := range ws {
					if lessWitness(a, b) && lessWitness(b, c) && !lessWitness(a, c) {
						t.Fatalf("transitivity broken: a<b<c but !a<c: %v %v %v", id(a), id(b), id(c))
					}
					// A strict weak ordering requires incomparability to be transitive.
					if incomparable(a, b) && incomparable(b, c) && !incomparable(a, c) {
						t.Fatalf("incomparability not transitive (not a strict weak order): %v %v %v", id(a), id(b), id(c))
					}
				}
			}
		}

		// Permutation-invariance of the production sort on all-distinct identities: a
		// strict total order makes even a non-stable sort yield the one sorted sequence
		// regardless of input order. (With ties present this need not hold, so it is
		// asserted only where dedup's no-tie invariant is met.)
		if allDistinctIdentities(ws, id) {
			fwd := append([]Witness(nil), ws...)
			rev := make([]Witness, len(ws))
			for i, w := range ws {
				rev[len(ws)-1-i] = w
			}
			sort.Slice(fwd, func(i, j int) bool { return lessWitness(fwd[i], fwd[j]) })
			sort.Slice(rev, func(i, j int) bool { return lessWitness(rev[i], rev[j]) })
			for i := range fwd {
				if id(fwd[i]) != id(rev[i]) {
					t.Fatalf("sort not permutation-invariant at %d: %v vs %v", i, id(fwd[i]), id(rev[i]))
				}
			}
		}
	})
}

// decodeFuzzWitnesses parses up to 6 witnesses from raw: records split on '\n', the six
// sort-relevant fields on '\t' (Effect, Flow, Service, Entry, Op, path-op). The path
// field becomes a one-span chain so pathSig — the final tie-break key — varies with it.
// 6 caps the O(n^3) transitivity loop. Records without exactly six fields are skipped.
func decodeFuzzWitnesses(raw string) []Witness {
	const maxN = 6
	var ws []Witness
	for _, rec := range strings.Split(raw, "\n") {
		f := strings.Split(rec, "\t")
		if len(f) != 6 {
			continue
		}
		ws = append(ws, Witness{
			Effect:   f[0],
			Observed: Observation{Flow: f[1], Service: f[2], Entry: f[3], Op: f[4]},
			chain:    []*ir.CanonicalSpan{{Op: f[5]}},
		})
		if len(ws) == maxN {
			break
		}
	}
	return ws
}

func allDistinctIdentities(ws []Witness, id func(Witness) [6]string) bool {
	seen := make(map[[6]string]bool, len(ws))
	for _, w := range ws {
		k := id(w)
		if seen[k] {
			return false
		}
		seen[k] = true
	}
	return true
}
