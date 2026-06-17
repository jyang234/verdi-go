package impeach

import (
	"strconv"
	"strings"

	"github.com/jyang234/golang-code-graph/internal/canon/opkey"
	"github.com/jyang234/golang-code-graph/internal/groundwork/graph"
)

// The downgrade ladder (plan §4): the five ordered rungs that turn a Phase-0
// CANDIDATE into either a true IMPEACHMENT or one specific weaker disclosure. A
// naive impeachment is usually a false alarm, so a sound one requires ruling out
// every benign explanation FIRST. The conjunction is what keeps impeachments rare,
// and rare is what keeps them trusted (anti-fatigue): a single failed rung is a
// concrete, named reason the contradiction is not (yet) sound, never a generic
// "rejected".
const (
	// VerdictImpeachment is the apex: every gating rung ruled out its benign
	// explanation, so the static negative is behaviorally impeached (§3).
	VerdictImpeachment = "IMPEACHMENT"

	// The four downgrades, one per gating rung, named in §4. The verdict is the
	// FIRST failing rung's downgrade (the ladder is ordered), so the disclosure is
	// the precise reason — not a generic rejection.
	DowngradeNotAContradiction = "NOT-A-CONTRADICTION" // rung 1: static abstains here
	DowngradeVersionSkew       = "VERSION-SKEW"        // rung 2: code identity unestablished/mismatched
	DowngradeLabelMismatch     = "LABEL-MISMATCH"      // rung 3: effect outside the one-source vocabulary
	DowngradeCrossService      = "CROSS-SERVICE"       // rung 4: effect on a foreign service's span
	DowngradeCaptureUntrusted  = "CAPTURE-UNTRUSTED"   // rung 5: capture not production/integration
)

// Rung names — the canonical ordered set (§4). The order IS the evaluation order
// and the verdict priority: a candidate that fails rung k downgrades to rung k's
// disclosure regardless of how later rungs evaluate (they are still recorded).
const (
	RungStaticAssertsNoPath = "static-asserts-no-path"
	RungCodeIdentity        = "code-identity"
	RungLabel               = "label"
	RungServiceScope        = "service-scope"
	RungCaptureFidelity     = "capture-fidelity"
)

// Capture-fidelity labels (§5). Only the first two clear the capture-fidelity
// rung; an absent or synthetic label caps a witness at CAPTURE-UNTRUSTED.
const (
	CaptureProduction  = "production"
	CaptureIntegration = "integration"
	CaptureSynthetic   = "synthetic"
)

// Provenance is the corpus-level capture-side identity the ladder's rungs 2 and 5
// consume (§4). It is supplied by the CALLER and never inferred from the trace:
// the canonical trace model carries neither a deployed-commit stamp nor a capture
// label yet (§14-D), so the honest default is the zero value — which fails BOTH
// capture-side rungs closed, capping every candidate at a downgrade. An
// impeachment may only ever be promoted on provenance the caller can attest, so
// absence biases toward abstention, never a false IMPEACHMENT (tenet 2).
type Provenance struct {
	// TraceIdentity is the code identity (typically the deployed commit) of the
	// code that produced the corpus, matched against the graph's Stamp by the
	// code-identity rung. "" is "unestablished" and forces VERSION-SKEW by
	// representation (§5): a negative cannot be impeached against code we cannot
	// prove the trace ran.
	TraceIdentity string
	// Capture is the self-declared capture fidelity: production | integration |
	// synthetic. Only production/integration clears capture-fidelity; "" and any
	// other value cap the witness at CAPTURE-UNTRUSTED. The weakest, human-asserted
	// rung (§4): recorded, never inferred, and the cap that keeps a mislabeled mock
	// from minting a false impeachment until provenance can be attested (§12.6).
	Capture string
}

// Rung is one evaluated step of the downgrade ladder. Passed == true means the
// benign explanation was RULED OUT (the rung is satisfied toward impeachment), so
// an IMPEACHMENT is exactly "every rung Passed". Evidence records WHY, so a
// partial rule-out is auditable without re-running the ladder (§4: recorded whole).
type Rung struct {
	Name     string `json:"name"`
	Passed   bool   `json:"passed"`
	Evidence string `json:"evidence"`
}

// classify runs the five-rung downgrade ladder (§4) over one candidate, returning
// the FULL ordered ladder and the verdict. Every rung is evaluated and recorded
// even after an earlier one fails — the ladder is recorded WHOLE, so a partial
// rule-out ("label and service-scope clear, only code-identity is missing") is
// itself actionable. The verdict is the first failing rung's specific downgrade
// (rungs are ordered by §4 priority), or IMPEACHMENT when every gating rung passed.
//
// Every rung fails CLOSED: a rung whose substrate is absent does NOT pass. So the
// cell's own worst failure on a bare reachability negative is a downgrade to
// abstention, never a confident wrong IMPEACHMENT (tenet 2, §3).
func classify(w Witness, ix *graph.Index, service string, prov Provenance) ([]Rung, string) {
	rungs := []Rung{
		rungStaticAssertsNoPath(w),
		rungCodeIdentity(ix, prov),
		rungLabel(w),
		rungServiceScope(w, service),
		rungCaptureFidelity(prov),
	}
	for _, r := range rungs {
		if !r.Passed {
			return rungs, downgradeFor(r.Name)
		}
	}
	return rungs, VerdictImpeachment
}

// downgradeFor maps a rung name to the disclosure emitted when it fails. Every
// rung name maps; an unknown name returns "" so a missing case surfaces loudly
// rather than silently passing.
func downgradeFor(rung string) string {
	switch rung {
	case RungStaticAssertsNoPath:
		return DowngradeNotAContradiction
	case RungCodeIdentity:
		return DowngradeVersionSkew
	case RungLabel:
		return DowngradeLabelMismatch
	case RungServiceScope:
		return DowngradeCrossService
	case RungCaptureFidelity:
		return DowngradeCaptureUntrusted
	}
	return ""
}

// rungStaticAssertsNoPath (§4 rung 1) clears when static asserts a real NEGATIVE
// — unreachable or absent — rather than abstaining (blind). A blind cell is
// already excluded from candidates as RECLAIMED-LIVE, so this passes by
// construction for every Phase-0 candidate; the rung makes the precondition
// explicit and fails closed if a future caller ever hands a blind claim in.
func rungStaticAssertsNoPath(w Witness) Rung {
	r := w.Claim.Reachability
	passed := r == ReachUnreachable || r == ReachAbsent
	ev := "static asserts a real negative (" + r + ")"
	if !passed {
		ev = "static abstains here (" + r + "): not a contradiction"
	}
	return Rung{Name: RungStaticAssertsNoPath, Passed: passed, Evidence: ev}
}

// rungCodeIdentity (§4 rung 2) clears only when the graph was built from the same
// code that produced the trace: a non-empty graph Stamp equal to a non-empty
// supplied TraceIdentity. An absent stamp or identity is "unestablished" and
// fails closed by representation (§5) — a production trace may only impeach the
// production-stamped graph, never a graph of unknown vintage. The most demanding
// rung on today's substrate: the trace model carries no commit stamp (§14-D), so
// without caller-supplied provenance every candidate stops here at VERSION-SKEW.
func rungCodeIdentity(ix *graph.Index, prov Provenance) Rung {
	gs := ix.Stamp()
	switch {
	case prov.TraceIdentity == "" || gs == "":
		return Rung{Name: RungCodeIdentity, Passed: false,
			Evidence: "code identity unestablished (graph stamp " + q(gs) + ", trace identity " + q(prov.TraceIdentity) + ")"}
	case prov.TraceIdentity != gs:
		return Rung{Name: RungCodeIdentity, Passed: false,
			Evidence: "version skew: trace " + q(prov.TraceIdentity) + " != graph " + q(gs)}
	default:
		return Rung{Name: RungCodeIdentity, Passed: true,
			Evidence: "same code: graph stamp == trace identity " + q(gs)}
	}
}

// rungLabel (§4 rung 3) clears when the effect key belongs to the ONE-SOURCE label
// vocabulary both sides reduce through — a bus PUBLISH/CONSUME key or a canonical
// "db <OP> <table>" key. The Phase-0 join key space IS that vocabulary
// (DBEffectKey / the bus op key), so this passes by construction; the rung is the
// explicit guard that the comparison is like-with-like, failing closed
// (LABEL-MISMATCH) if a key ever bypassed the reconciliation.
func rungLabel(w Witness) Rung {
	passed := recognizedLabel(w.Effect)
	ev := "effect " + q(w.Effect) + " is in the one-source bus/DB vocabulary"
	if !passed {
		ev = "effect " + q(w.Effect) + " is outside the reconciled label vocabulary"
	}
	return Rung{Name: RungLabel, Passed: passed, Evidence: ev}
}

// recognizedLabel reports whether key round-trips through the one-source label
// vocabulary: a bus PUBLISH/CONSUME op key with a destination, or a canonical DB
// key carrying a verb. An empty destination/verb is opaque and never recognized.
func recognizedLabel(key string) bool {
	if isBusKey(key) {
		return opkey.BusDestination(key) != ""
	}
	if isDBKey(key) {
		return len(strings.Fields(strings.TrimPrefix(key, "db "))) >= 1
	}
	return false
}

// rungServiceScope (§4 rung 4) clears when the observed effect is on the impeached
// service's OWN spans. The join walks every span in a (possibly cross-service)
// trace, so an effect attributed to a foreign service must not impeach THIS
// service's graph — that is CROSS-SERVICE, the other service's audit to run.
func rungServiceScope(w Witness, service string) Rung {
	got := w.Observed.Service
	passed := got == service
	ev := "effect on the impeached service's own spans (" + q(service) + ")"
	if !passed {
		ev = "effect on a foreign service's span (" + q(got) + " != " + q(service) + ")"
	}
	return Rung{Name: RungServiceScope, Passed: passed, Evidence: ev}
}

// rungCaptureFidelity (§4 rung 5) clears only for a production or integration
// capture — a real run of the code. A synthetic/mock capture (or an unrecorded
// one) caps the witness at CAPTURE-UNTRUSTED, because a test double emitting a
// boundary span the real code gates out would otherwise mint a false impeachment.
// The weakest rung: it rests on a self-declared label until provenance can be
// attested (§4, §12.6), so it fails closed on anything but the two trusted labels.
func rungCaptureFidelity(prov Provenance) Rung {
	passed := prov.Capture == CaptureProduction || prov.Capture == CaptureIntegration
	ev := "capture is " + q(prov.Capture) + " (real run)"
	if !passed {
		ev = "capture not trusted (" + q(prov.Capture) + "): production|integration required"
	}
	return Rung{Name: RungCaptureFidelity, Passed: passed, Evidence: ev}
}

// q quotes a value for rung evidence, rendering "" as a visible empty literal so an
// unestablished input reads as "" rather than vanishing into the surrounding text.
func q(s string) string { return strconv.Quote(s) }
