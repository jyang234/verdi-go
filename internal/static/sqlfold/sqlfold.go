// Package sqlfold is the SQL const-accumulation fold (docs/design/
// sql-constfold-reclaim-plan.md): a sound, opt-in LABEL reclaimer that recovers
// the verb (and, when constant, the table) of a SQL statement assembled at
// runtime from compile-time-constant fragments — the dominant shape of the B2
// "opaque SQL" frontier (a constant statement laundered through a
// strings.Builder), not genuinely dynamic SQL.
//
// Soundness is asymmetric (plan §3, L1), and the asymmetry is the whole design:
//
//   - WRITE promotion needs only the constant LEADING verb. A statement whose
//     constant prefix proves a mutating verb (INSERT/UPDATE/DELETE/…) is a write
//     no matter what its variable tail appends — appending cannot un-write a
//     write — so the recovered verb can only ADD to the write surface, never hide
//     a write. The table is named only when it too is in the constant prefix.
//   - READ classification needs the WHOLE statement to be a single compile-time
//     constant (every fragment constant, every placeholder a bound `$N`/`?`, no
//     conditional or variable-cardinality fragment). Only then is there no
//     unconstrained text splice through which a second, mutating statement could
//     be smuggled (`"SELECT 1; " + var`). A read claim further requires a
//     RECOGNIZED read verb (SELECT), so a data-modifying CTE (WITH … RETURNING),
//     whose leading token canon/sql does not recognize, fails closed.
//
// Anything the fold cannot prove to either standard returns ok=false: the caller
// leaves the effect exactly as opaque as it is today. The fold can only ever
// match or improve on the current label — never weaken a verdict.
package sqlfold

import (
	"go/token"
	"go/types"
	"sort"
	"strings"

	"golang.org/x/tools/go/ssa"

	"github.com/jyang234/golang-code-graph/internal/canon/sql"
	"github.com/jyang234/golang-code-graph/internal/sqlverb"
	"github.com/jyang234/golang-code-graph/internal/static/features"
)

// Via is the provenance tag carried on a boundary edge whose verb the fold
// recovered (plan §3, L3). It names the const-accumulation fold so a verdict that
// leaned on a reclaimed verb is auditable and a reviewer can diff folded vs base.
const Via = "sql-constfold"

// Recover returns the SQL operation and the table(s) the fold can soundly read
// off the query value q, plus whether it recovered anything. q is the string
// argument the labeler could NOT fold at the call site (a non-constant value);
// Recover traces it back through string concatenation and a fluent strings.Builder
// accumulator to the constant fragments behind it.
//
// It emits a verb under exactly two disciplines (see the package doc): a mutating
// verb proven by the constant leading prefix (a write, tail irrelevant), or a
// SELECT proven by a wholly-constant statement (a read). Everything else abstains.
//
// tables holds zero, one, or several table names. Empty means the verb is known
// but the table is dynamic (a fold-promoted write to an unnamed target). More than
// one means the table is a finite, provably-complete set of constants (phase 2):
// each is a real possible write target, so the caller emits one edge per table —
// an over-approximation in the safe direction (it can only over-list targets,
// never hide a write). Table naming is verdict-NEUTRAL: read/write keys on op, not
// the table, so a naming miss never changes a verdict.
func Recover(q ssa.Value) (op string, tables []string, ok bool) {
	frags, complete := assemble(q, map[ssa.Value]bool{})
	prefix := render(frags)
	if strings.TrimSpace(prefix) == "" {
		return "", nil, false
	}
	n := sql.Normalize(prefix)
	// Write promotion: the leading prefix proves a mutating verb. Sound regardless
	// of any variable tail — appending cannot turn a mutation into a non-mutation.
	if sqlverb.Mutating(n.Operation) {
		if n.Table != "" {
			return n.Operation, []string{n.Table}, true // table was in the constant prefix
		}
		// The table is dynamic. Phase 2: name it only if it resolves to a finite,
		// provably-complete set of constants; otherwise leave it unnamed. This runs
		// ONLY in the write branch, where classification is already settled by the
		// verb — so the resolution affects target NAMES only, never read/write.
		if tbls, ok := resolveTable(frags, prefix, n.Operation); ok {
			return n.Operation, tbls, true
		}
		return n.Operation, nil, true
	}
	// Read classification: only a wholly-constant statement with a recognized read
	// verb. `complete` guarantees no unconstrained text splice (no smuggling); the
	// explicit SELECT check keeps an unrecognized leading token (e.g. a WITH … CTE)
	// from being laundered into a read.
	if complete && n.Operation == "SELECT" {
		if n.Table != "" {
			return n.Operation, []string{n.Table}, true
		}
		return n.Operation, nil, true
	}
	return "", nil, false
}

// fragKind is how a fragment entered the statement text.
type fragKind int

const (
	fConst       fragKind = iota // a compile-time-constant text run
	fPlaceholder                 // a bound, separator-free placeholder ($N / ?)
	fHole                        // an unconstrained runtime value (text we cannot see)
)

type frag struct {
	kind fragKind
	text string
	val  ssa.Value // for fHole: the runtime value, so phase 2 can try to resolve it
}

// render joins the leading fragments into a normalizer-ready skeleton. A
// placeholder renders as a spaced `?` so canon/sql tokenizes it as one; a hole
// never reaches render (assemble stops the prefix at the first hole).
func render(frags []frag) string {
	var b strings.Builder
	for _, f := range frags {
		switch f.kind {
		case fConst:
			b.WriteString(f.text)
		case fPlaceholder:
			b.WriteString(" ? ")
		}
	}
	return b.String()
}

// assemble returns the leading contiguous run of fragments of the statement text
// behind v, and whether that run is the COMPLETE statement (no hole, no excluded
// conditional fragment). The run stops at the first hole or the first
// conditionally-appended fragment, so its leading tokens (the verb, often the
// table) are always something execution is guaranteed to produce.
func assemble(v ssa.Value, seen map[ssa.Value]bool) ([]frag, bool) {
	if v == nil || seen[v] {
		return []frag{{kind: fHole, val: v}}, false
	}
	// Path-based cycle detection: mark v for the duration of THIS subtree and unmark
	// on return, so a value reachable from itself (a builder whose own Build()/
	// String() result is written back into an append — `w.Write(w.Build())`) is
	// caught as a hole instead of recursing forever, while a value legitimately
	// reused across sibling fragments still contributes each time. `seen` is threaded
	// through assembleBuilder/contribution so the guard survives the builder hop.
	seen[v] = true
	defer delete(seen, v)
	if s, ok := features.ConstString(v); ok {
		return []frag{{kind: fConst, text: s}}, true
	}
	switch x := v.(type) {
	case *ssa.BinOp:
		if x.Op == token.ADD && isStringType(x.Type()) {
			lf, lc := assemble(x.X, seen)
			if !lc {
				return lf, false // a hole in the left operand ends the prefix
			}
			rf, rc := assemble(x.Y, seen)
			return append(lf, rf...), rc
		}
	case *ssa.Call:
		if isStrconvIntegral(x.Common().StaticCallee()) {
			return []frag{{kind: fPlaceholder}}, true
		}
		if inst, term, ok := builderTerminal(x, 0); ok {
			return assembleBuilder(inst, term, seen)
		}
	case *ssa.Extract:
		if call, ok := x.Tuple.(*ssa.Call); ok {
			if inst, term, ok := builderTerminal(call, x.Index); ok {
				return assembleBuilder(inst, term, seen)
			}
		}
	}
	return []frag{{kind: fHole, val: v}}, false
}

// builderTerminal reports whether call is the accumulator's terminal — a
// strings.Builder.String() (directly, or a user method that returns
// recv.<builder>.String() at result index idx) — and if so returns the builder
// instance (the call's receiver) and the terminal call.
func builderTerminal(call *ssa.Call, idx int) (inst ssa.Value, term *ssa.Call, ok bool) {
	callee := call.Common().StaticCallee()
	if callee == nil || call.Common().IsInvoke() {
		return nil, nil, false
	}
	args := call.Common().Args
	if len(args) == 0 {
		return nil, nil, false
	}
	if isStringsBuilderMethod(callee) && callee.Name() == "String" {
		return args[0], call, true
	}
	if summarize(callee).terminalIndex == idx && summarize(callee).kind == smTerminal {
		return args[0], call, true
	}
	return nil, nil, false
}

// assembleBuilder walks the builder instance's method calls in program order and
// concatenates their fragment contributions, stopping at the first call that does
// not dominate the terminal (a conditional append), the first hole, or the first
// call that does not dominate every point where the builder ESCAPES to an unseen
// append (a closure capture, a non-receiver argument). The leading run it returns
// is what every execution path produces before any variability OR any append we
// cannot see — so its head tokens (the verb, often the table) are sound even when
// a later fragment is invisible (Tier A: the const-prefix fold).
//
// The builder is found whether it lives in an SSA register (the fluent-chain case)
// or in a memory cell (an `&w`/closure-captured builder, where each method call
// loads the pointer afresh and the register def-use chain is broken). See
// gatherBuilderCalls.
func assembleBuilder(inst ssa.Value, term *ssa.Call, seen map[ssa.Value]bool) ([]frag, bool) {
	termBlock := term.Block()
	// A builder loaded through a pointer we cannot resolve to a concrete local cell
	// (a copy/Phi of the address, a struct field) means we cannot enumerate every
	// load — a sibling load could carry appends we never see — so fail closed rather
	// than trust a partial view that could launder an unseen mutating tail into a read.
	if u, ok := inst.(*ssa.UnOp); ok && u.Op == token.MUL && cellOf(inst) == nil {
		return nil, false
	}
	calls, escapes := gatherBuilderCalls(inst)
	// The dominance reasoning below assumes ACYCLIC flow: it treats "c dominates the
	// escape" as "c happens before the escape", which a loop back-edge breaks — a
	// prior iteration's unseen append can precede this iteration's append/terminal.
	// So when the builder has any escape AND its terminal or an escape sits in a
	// cycle, fail closed; the const-prefix and read-completeness arguments do not
	// hold across iterations. (A no-escape looped builder cannot smuggle hidden text,
	// so it does not need this guard.) Mirrors writerTemplate's entry-block-only rule.
	if len(escapes) > 0 {
		if blockInCycle(termBlock) {
			return nil, false
		}
		for _, e := range escapes {
			if blockInCycle(e.Block()) {
				return nil, false
			}
		}
	}
	// Only escapes that can run BEFORE the terminal reads the string matter: an
	// append after the read cannot change the read value. relevantEscapes drops the
	// rest, so a builder reused after Build (a harmless later escape) does not block
	// a clean read of the value Build produced. Sound here because the cycle guard
	// above has already ruled out a back-edge re-entry.
	rel := relevantEscapes(escapes, term)
	sort.Slice(calls, func(i, j int) bool {
		bi, ii := position(calls[i])
		bj, ij := position(calls[j])
		if bi != bj {
			return bi < bj
		}
		return ii < ij
	})
	var out []frag
	for _, c := range calls {
		if c == term {
			continue
		}
		if c.Block() == nil || termBlock == nil || !c.Block().Dominates(termBlock) {
			return out, false // a conditional append — the prefix ends here
		}
		// Soundness of the leading run under an escape: an append is part of the
		// trustworthy prefix only if it provably executes BEFORE every unseen-append
		// point (it dominates each). Otherwise an invisible write could precede it on
		// some path, so it (and everything after) is no longer guaranteed leading.
		if !dominatesAll(c, rel) {
			return out, false
		}
		cf, cc := contribution(c, seen)
		out = append(out, cf...)
		if !cc {
			return out, false
		}
	}
	// `complete` must mean we provably saw EVERY write to this builder: if the
	// instance escaped to somewhere we cannot model (a helper or closure that might
	// append more), an unseen write could change the statement, so the read direction
	// — which trusts a wholly-constant statement — must not fire. Write promotion is
	// unaffected: an append can never un-write the leading verb, only follow it.
	return out, len(rel) == 0
}

// gatherBuilderCalls returns every concrete method call applied to the builder
// instance, plus every instruction at which the builder escapes to a context that
// could append text out of view. It handles two SSA shapes:
//
//   - register builder (the fluent chain): `inst` is the builder value itself and
//     every append is a method call on it or on a chain result. chainCalls follows
//     the def-use chain.
//   - memory-cell builder: `inst` is a load of an `*ssa.Alloc` cell (the builder's
//     address was taken — captured by a closure or passed by reference), so each
//     append loads the pointer afresh and the register chain is broken. cellCalls
//     gathers the appends across every load of the cell and treats the cell's
//     non-load uses (a closure capture, a pass-by-reference) as escapes.
func gatherBuilderCalls(inst ssa.Value) (calls []*ssa.Call, escapes []ssa.Instruction) {
	if cell := cellOf(inst); cell != nil {
		return cellCalls(cell)
	}
	return chainCalls(inst)
}

// cellOf returns the local cell a memory-resident builder lives in — the
// `*ssa.Alloc` behind a `*p` load — or nil when inst is an ordinary register
// builder. Only a direct load of an Alloc qualifies; anything else stays on the
// register path.
func cellOf(inst ssa.Value) *ssa.Alloc {
	u, ok := inst.(*ssa.UnOp)
	if !ok || u.Op != token.MUL {
		return nil
	}
	a, _ := u.X.(*ssa.Alloc)
	return a
}

// cellCalls gathers the builder appends across every load of a memory cell, and
// the points where the cell escapes. A load (`*cell`) is a use of the builder, so
// its method calls are appends (chainCalls). The cell's other referrers are: the
// single constructor store of the builder pointer (ignored), a closure capture, or
// a pass-by-reference — each non-load, non-constructor use an escape past which an
// unseen append could occur.
//
// A cell written more than once (or never) is ambiguous: different loads can see
// different builders, and merging their append sets would mint a phantom statement
// (e.g. builder A's DELETE verb attributed to builder B's terminal). So a cell
// without EXACTLY ONE store abstains — empty calls, which fails the fold closed —
// rather than guess which builder a load belongs to.
func cellCalls(cell *ssa.Alloc) (calls []*ssa.Call, escapes []ssa.Instruction) {
	if cell.Referrers() == nil {
		return nil, nil
	}
	stores := 0
	for _, ref := range *cell.Referrers() {
		if st, ok := ref.(*ssa.Store); ok && st.Addr == ssa.Value(cell) {
			stores++
		}
	}
	if stores != 1 {
		return nil, nil // reassigned or uninitialized cell — too ambiguous to fold
	}
	for _, ref := range *cell.Referrers() {
		switch r := ref.(type) {
		case *ssa.UnOp:
			if r.Op == token.MUL {
				cc, ce := chainCalls(r)
				calls = append(calls, cc...)
				escapes = append(escapes, ce...)
				continue
			}
			escapes = append(escapes, r)
		case *ssa.Store:
			if r.Addr == ssa.Value(cell) {
				continue // the single constructor store of the builder pointer
			}
			escapes = append(escapes, r) // the cell's address stored elsewhere
		default:
			escapes = append(escapes, ref)
		}
	}
	return calls, escapes
}

// chainCalls returns every concrete method call whose receiver is the builder
// value root — following the fluent chain, since each append method returns the
// receiver, so `w.Write(a).Arg(b)` and a later `w.Build()` all act on one builder.
// Any referrer that uses a chain value as something OTHER than a method receiver
// (a non-receiver argument, a store, a return, a closure capture) is an escape: a
// write could happen out of view through it.
func chainCalls(root ssa.Value) (calls []*ssa.Call, escapes []ssa.Instruction) {
	seen := map[ssa.Value]bool{}
	work := []ssa.Value{root}
	for len(work) > 0 {
		v := work[len(work)-1]
		work = work[:len(work)-1]
		if seen[v] || v.Referrers() == nil {
			continue
		}
		seen[v] = true
		for _, ref := range *v.Referrers() {
			call, ok := ref.(*ssa.Call)
			if !ok || call.Common().IsInvoke() || len(call.Common().Args) == 0 || call.Common().Args[0] != v {
				escapes = append(escapes, ref) // v is used as something other than a method receiver
				continue
			}
			calls = append(calls, call)
			// Chain on only when the method returns the SAME builder type. Compare
			// with types.Identical, not ==: distinct *types.Pointer instances for the
			// same *T are not pointer-equal, and == would silently drop the rest of a
			// fluent chain (an UNSOUND under-read — the dynamic tail goes unseen).
			if types.Identical(call.Type(), root.Type()) {
				work = append(work, call)
			}
		}
	}
	return calls, escapes
}

// blockInCycle reports whether b lies on a cycle — it can reach itself by
// following successor edges (a loop back-edge). The dominance-as-happens-before
// reasoning in assembleBuilder is only valid for blocks NOT on a cycle, so this is
// the guard that keeps a back-edge from defeating the const-prefix/read soundness
// argument. Bounded by a visited set.
func blockInCycle(b *ssa.BasicBlock) bool {
	if b == nil {
		return false
	}
	seen := map[*ssa.BasicBlock]bool{}
	stack := append([]*ssa.BasicBlock{}, b.Succs...)
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if n == b {
			return true
		}
		if seen[n] {
			continue
		}
		seen[n] = true
		stack = append(stack, n.Succs...)
	}
	return false
}

// relevantEscapes drops escapes that provably run only AFTER the terminal (the
// terminal dominates them), since an append there cannot inject text into the
// value the terminal reads. What remains is every point where an unseen append
// could execute before the statement is read. Sound only for acyclic flow; the
// caller (assembleBuilder) fails closed on cycles before relying on it.
func relevantEscapes(escapes []ssa.Instruction, term *ssa.Call) []ssa.Instruction {
	var out []ssa.Instruction
	for _, e := range escapes {
		if instrDominates(term, e) {
			continue
		}
		out = append(out, e)
	}
	return out
}

// dominatesAll reports whether instruction c dominates every escape point — i.e.,
// c provably executes before any of them, so no unseen append can precede c.
func dominatesAll(c ssa.Instruction, escapes []ssa.Instruction) bool {
	for _, e := range escapes {
		if !instrDominates(c, e) {
			return false
		}
	}
	return true
}

// contribution returns the fragments a single builder-method call appends, and
// whether they are fully constant/placeholder (no hole).
func contribution(call *ssa.Call, seen map[ssa.Value]bool) ([]frag, bool) {
	callee := call.Common().StaticCallee()
	if callee == nil {
		return []frag{{kind: fHole}}, false
	}
	args := call.Common().Args
	if isStringsBuilderMethod(callee) {
		switch callee.Name() {
		case "WriteString", "Write":
			if len(args) >= 2 {
				return assemble(args[1], seen)
			}
		case "WriteByte", "WriteRune":
			if len(args) >= 2 {
				if c, ok := constByteOrRune(args[1]); ok {
					return []frag{{kind: fConst, text: c}}, true
				}
			}
		case "String":
			return nil, true // the terminal contributes no text
		}
		return []frag{{kind: fHole}}, false
	}
	// A user accumulator method: instantiate its summarized template with the
	// call's arguments. A parameter slot takes whatever the call passes (constant
	// text, a nested concatenation, or a hole).
	sm := summarize(callee)
	switch sm.kind {
	case smTerminal:
		return nil, true
	case smWriter:
		var out []frag
		for _, tf := range sm.template {
			switch tf.kind {
			case tfConst:
				out = append(out, frag{kind: fConst, text: tf.text})
			case tfPlaceholder:
				out = append(out, frag{kind: fPlaceholder})
			case tfParam:
				if tf.param >= len(args) {
					return out, false
				}
				af, ac := assemble(args[tf.param], seen)
				out = append(out, af...)
				if !ac {
					return out, false
				}
			}
		}
		return out, true
	default:
		return []frag{{kind: fHole}}, false
	}
}
