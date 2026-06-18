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
// is the witness sort's comparator (§5); for the non-stable candidate sort to be
// deterministic and arrival-order-independent it must be a strict TOTAL order over the
// witness identity tuple (witnessSortKey: Effect, Flow, Service, Entry, Op, pathSig).
//
// Over GENERATED witnesses the fuzz asserts the properties that make it exactly that:
// irreflexivity, asymmetry, transitivity, and TOTALITY on distinct identities — every
// two witnesses with different identity tuples must be comparable, so no pair ever
// falls back to arrival order (the §12.5 non-determinism the dedup exists to prevent);
// equal tuples compare equal, and the dedup (observedEffects) guarantees those never
// coexist as candidates. It then asserts permutation-invariance of the production sort
// (sort.Slice, non-stable) on all-distinct inputs — the end-user property. The chain
// carries a flowmap.fqn tag, so pathSig's tag-folding (the deepest tie-break: two paths
// to one effect distinguished only by their FQN tag) is exercised, not just bare ops.
//
// The identity tuple is witnessSortKey — the SAME helper lessWitness compares — so the
// comparator and this property test cannot drift on which fields constitute identity.
func FuzzWitnessSortStrictWeakOrder(f *testing.F) {
	// Two witnesses identical but for the chain's FQN TAG — the deepest tie-break key
	// (pathSig folds the tag), distinct identities the sort must still order.
	f.Add("db DELETE ledger\tPOST /x\tsvc\tHTTP\tDB DELETE\twork\ttagA\n" +
		"db DELETE ledger\tPOST /x\tsvc\tHTTP\tDB DELETE\twork\ttagB")
	// A spread that ties on various prefixes of the key tuple, mixing tagged/untagged.
	f.Add("a\tb\tc\td\te\tf\t\nA\tb\tc\td\te\tf\tt\na\tB\tc\td\te\tf\t\na\tb\tc\td\te\tg\tt")
	f.Fuzz(func(t *testing.T, raw string) {
		ws := decodeFuzzWitnesses(raw)
		if len(ws) < 2 {
			return
		}
		id := witnessSortKey // the one identity definition lessWitness also orders on

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
		for _, a := range ws { // transitivity
			for _, b := range ws {
				for _, c := range ws {
					if lessWitness(a, b) && lessWitness(b, c) && !lessWitness(a, c) {
						t.Fatalf("transitivity broken: a<b<c but !a<c: %v %v %v", id(a), id(b), id(c))
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

// decodeFuzzWitnesses parses up to 6 witnesses from raw: records split on '\n', SEVEN
// sort-relevant fields on '\t' (Effect, Flow, Service, Entry, Op, path-op, path-fqn-tag).
// The last two become a one-span chain — its Op plus, when the tag field is non-empty,
// a flowmap.fqn tag — so pathSig (the final tie-break key) varies with both, exercising
// its tag-folding branch as well as the no-tag path. 6 caps the O(n^3) transitivity
// loop. Records without exactly seven fields are skipped.
func decodeFuzzWitnesses(raw string) []Witness {
	const maxN = 6
	var ws []Witness
	for _, rec := range strings.Split(raw, "\n") {
		f := strings.Split(rec, "\t")
		if len(f) != 7 {
			continue
		}
		span := &ir.CanonicalSpan{Op: f[5]}
		if f[6] != "" { // exercise pathSig's FQN-tag folding (the path-identity tie-break)
			span.Attrs = map[string]string{FQNTagKey: f[6]}
		}
		ws = append(ws, Witness{
			Effect:   f[0],
			Observed: Observation{Flow: f[1], Service: f[2], Entry: f[3], Op: f[4]},
			chain:    []*ir.CanonicalSpan{span},
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
