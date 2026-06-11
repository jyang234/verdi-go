// Package obligations evaluates domain-declared path obligations over each
// function's SSA control-flow graph — the intraprocedural-but-domain-specific
// slice no off-the-shelf analyzer can know (path-obligations plan). flowmap
// already holds every function's CFG (ssa.Function.Blocks); this package reads
// the intraprocedural structure the call graph discards.
//
// Two obligation kinds, both pure functions of (rules, SSA):
//
//   - must-release: after an acquire, every path to function exit must hit a
//     release. Not a dominance query — a forward CFG walk asking "is any exit
//     reachable from the acquire without passing a release site?", with two
//     refinements: a `defer` of a release covers every later exit on its path,
//     and the acquire's own failure branch (`if err != nil { return err }` on
//     the acquire's error result) is pruned — a failed acquire holds nothing.
//   - must-precede: every Before site must be dominated by a Require site —
//     a straight dominator-tree query (same block: earlier index; otherwise
//     strict block dominance).
//
// The check is value-blind by design: it proves the SHAPE of the lifecycle
// (a release call appears on every path), not that the right value was
// released — the mode-2 wall. Consequently a release performed inside an
// unlisted helper reports VIOLATED; the fix is naming the helper as a release
// ref, keeping the rule vocabulary explicit. Abstention (CANT-PROVE) fires
// when the shape claim itself would be unsound: the acquired value's ownership
// leaves the function (returned, stored, captured by a closure, handed to a
// goroutine), or the function uses recover (control can rejoin invisibly).
// A CANT-PROVE is a disclosed blind spot, never a silent pass. Arbitrary
// (even irreducible) CFGs are fine: the walk does not rely on reducibility,
// and SSA's dominator tree is defined for any CFG.
//
// Verdicts are three-valued plus one disclosure: SATISFIED / VIOLATED /
// CANT-PROVE per anchored site, and UNMATCHED for a rule whose anchor matches
// no call site anywhere — an inert guardrail the reviewer must see.
package obligations

import (
	"fmt"
	"go/token"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/tools/go/ssa"

	"github.com/jyang234/golang-code-graph/internal/config"
)

// Status is the verdict for one anchored site (or, for Unmatched, one rule).
type Status string

const (
	// Satisfied: the obligation holds on every modeled path — the universal
	// proof, the class a test suite cannot produce.
	Satisfied Status = "SATISFIED"
	// Violated: a path exists in the CFG where the obligation fails. Syntactic:
	// the path is present; feasibility is not proven.
	Violated Status = "VIOLATED"
	// CantProve: the shape claim would be unsound here; the reason is disclosed.
	CantProve Status = "CANT-PROVE"
	// Unmatched: the rule's anchor matches no call site in the analyzed unit —
	// the guardrail is inert and must not be mistaken for protection.
	Unmatched Status = "UNMATCHED"
)

// Finding is one obligation verdict. Identity is (Rule, Fn, Site); Detail is
// presentation only (D-OB6). Sites are emitted relative to the service
// directory with forward slashes so output is byte-identical across machines.
type Finding struct {
	Rule   string `json:"rule"`
	Kind   string `json:"kind"`
	Fn     string `json:"fn,omitempty"`
	Site   string `json:"site,omitempty"`
	Status Status `json:"status"`
	Detail string `json:"detail,omitempty"`
}

// Check evaluates every rule against every function, returning findings sorted
// by (rule, fn, site). baseDir anchors site paths (the service directory);
// empty means bare file names. The result is a pure function of its inputs.
func Check(rules []config.ObligationRule, fns []*ssa.Function, baseDir string) []Finding {
	var out []Finding
	for i := range rules {
		rule := &rules[i]
		matched := false
		for _, fn := range fns {
			if fn == nil || len(fn.Blocks) == 0 {
				continue
			}
			var fs []Finding
			if rule.Kind() == config.KindMustRelease {
				fs = checkRelease(rule, fn, baseDir)
			} else {
				fs = checkPrecede(rule, fn, baseDir)
			}
			if len(fs) > 0 {
				matched = true
				out = append(out, fs...)
			}
		}
		if !matched {
			out = append(out, Finding{
				Rule: rule.Name, Kind: rule.Kind(), Status: Unmatched,
				Detail: fmt.Sprintf("anchor %s matches no call site — the rule is inert", anchor(rule)),
			})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Rule != b.Rule {
			return a.Rule < b.Rule
		}
		if a.Fn != b.Fn {
			return a.Fn < b.Fn
		}
		return a.Site < b.Site
	})
	return out
}

func anchor(rule *config.ObligationRule) string {
	if rule.Kind() == config.KindMustRelease {
		return rule.Acquire
	}
	return rule.Before
}

// ---- reference matching -----------------------------------------------------

// ref is a parsed "import/path#Symbol" function reference. It matches by
// package path and name (receiver-agnostic), on both static call targets and
// interface-method (invoke) call sites — acquiring through an interface is
// idiomatic Go, and a rule must bind either way.
type ref struct{ pkg, name string }

func parseRef(s string) ref {
	i := strings.IndexByte(s, '#')
	return ref{pkg: s[:i], name: s[i+1:]}
}

func parseRefs(ss []string) []ref {
	out := make([]ref, len(ss))
	for i, s := range ss {
		out[i] = parseRef(s)
	}
	return out
}

func (r ref) matchesCall(c ssa.CallInstruction) bool {
	common := c.Common()
	if common.IsInvoke() {
		m := common.Method
		return m.Pkg() != nil && m.Pkg().Path() == r.pkg && m.Name() == r.name
	}
	callee := common.StaticCallee()
	if callee == nil {
		return false
	}
	return pkgPathOf(callee) == r.pkg && callee.Name() == r.name
}

func anyRef(rs []ref, c ssa.CallInstruction) bool {
	for _, r := range rs {
		if r.matchesCall(c) {
			return true
		}
	}
	return false
}

func pkgPathOf(fn *ssa.Function) string {
	if fn.Pkg != nil {
		return fn.Pkg.Pkg.Path()
	}
	if obj := fn.Object(); obj != nil && obj.Pkg() != nil {
		return obj.Pkg().Path()
	}
	return ""
}

// ---- must-release -----------------------------------------------------------

func checkRelease(rule *config.ObligationRule, fn *ssa.Function, baseDir string) []Finding {
	acquire := parseRef(rule.Acquire)
	releases := parseRefs(rule.Release)

	var acquires []*ssa.Call
	for _, b := range fn.Blocks {
		for _, in := range b.Instrs {
			if call, ok := in.(*ssa.Call); ok && acquire.matchesCall(call) {
				acquires = append(acquires, call)
			}
		}
	}
	if len(acquires) == 0 {
		return nil
	}

	var out []Finding
	for _, acq := range acquires {
		f := Finding{
			Rule: rule.Name, Kind: config.KindMustRelease,
			Fn: fn.RelString(nil), Site: site(fn, acq.Pos(), baseDir),
		}
		switch {
		case usesRecover(fn):
			f.Status, f.Detail = CantProve, "function uses recover; control flow after a panic is invisible to the CFG"
		default:
			if why, escaped := ownershipEscapes(acq); escaped {
				f.Status, f.Detail = CantProve, why
			} else if exitPos, leaked := leakPath(fn, acq, releases); leaked {
				f.Status = Violated
				f.Detail = fmt.Sprintf("exit at %s reachable without release", site(fn, exitPos, baseDir))
			} else {
				f.Status = Satisfied
			}
		}
		out = append(out, f)
	}
	return out
}

// usesRecover reports whether fn or any of its anonymous functions (deferred
// closures run in fn's frame) calls the recover builtin.
func usesRecover(fn *ssa.Function) bool {
	check := func(f *ssa.Function) bool {
		for _, b := range f.Blocks {
			for _, in := range b.Instrs {
				if c, ok := in.(ssa.CallInstruction); ok {
					if bi, ok := c.Common().Value.(*ssa.Builtin); ok && bi.Name() == "recover" {
						return true
					}
				}
			}
		}
		return false
	}
	if check(fn) {
		return true
	}
	for _, anon := range fn.AnonFuncs {
		if check(anon) {
			return true
		}
	}
	return false
}

// ownershipEscapes reports whether the acquired value's lifecycle leaves the
// function: returned, stored, captured by a closure, or handed to a goroutine.
// Passing the value as a plain call argument is NOT an escape — the check is
// value-blind and a callee that releases it must be listed as a release ref.
// The walk follows value-preserving forwarding (tuple extracts, phis,
// interface boxing, conversions) so an escape through an alias is still seen.
func ownershipEscapes(acq *ssa.Call) (string, bool) {
	seen := map[ssa.Value]bool{}
	work := resourceValues(acq)
	for len(work) > 0 {
		v := work[len(work)-1]
		work = work[:len(work)-1]
		if seen[v] {
			continue
		}
		seen[v] = true
		refs := v.Referrers()
		if refs == nil {
			continue
		}
		for _, in := range *refs {
			switch r := in.(type) {
			case *ssa.Extract, *ssa.Phi, *ssa.MakeInterface, *ssa.ChangeType,
				*ssa.ChangeInterface, *ssa.Convert, *ssa.TypeAssert:
				work = append(work, r.(ssa.Value))
			case *ssa.Return:
				return "acquired value is returned — its lifecycle leaves the function", true
			case *ssa.Store:
				if r.Val == v {
					return "acquired value is stored — its lifecycle leaves the function", true
				}
			case *ssa.MakeClosure:
				return "acquired value is captured by a closure — releases there are invisible", true
			case *ssa.Go:
				return "acquired value is handed to a goroutine — concurrent release is out of scope", true
			}
		}
	}
	return "", false
}

// resourceValues returns the SSA values holding the acquire's resource — the
// non-error result components. The error result is deliberately excluded:
// `return err` on the failure branch is not the resource escaping.
func resourceValues(acq *ssa.Call) []ssa.Value {
	sig := acq.Call.Signature()
	if sig == nil || sig.Results().Len() == 0 {
		return nil
	}
	if sig.Results().Len() == 1 {
		if isErrorType(sig.Results().At(0).Type()) {
			return nil
		}
		return []ssa.Value{acq}
	}
	refs := acq.Referrers()
	if refs == nil {
		return nil
	}
	var out []ssa.Value
	for _, in := range *refs {
		if ex, ok := in.(*ssa.Extract); ok && !isErrorType(sig.Results().At(ex.Index).Type()) {
			out = append(out, ex)
		}
	}
	return out
}

func isErrorType(t interface{ String() string }) bool { return t.String() == "error" }

// leakPath walks the CFG forward from the acquire, looking for a function exit
// reachable without passing a release site. A release via plain call or defer
// covers the path from that point on; an explicit panic is an exit (an
// uncovered explicit panic leaks); implicit runtime panics are ignored, as
// lifecycle checkers conventionally do. The acquire's own failure branch is
// pruned: an If whose condition tests the acquire's error result against nil
// only has its success arm followed.
func leakPath(fn *ssa.Function, acq *ssa.Call, releases []ref) (token.Pos, bool) {
	errVals := acquireErrValues(acq)
	visited := map[*ssa.BasicBlock]bool{}

	var walk func(b *ssa.BasicBlock, from int) (token.Pos, bool)
	walk = func(b *ssa.BasicBlock, from int) (token.Pos, bool) {
		for i := from; i < len(b.Instrs); i++ {
			switch in := b.Instrs[i].(type) {
			case *ssa.Call:
				if anyRef(releases, in) {
					return token.NoPos, false // released: this path is covered
				}
			case *ssa.Defer:
				if anyRef(releases, in) {
					return token.NoPos, false // deferred release covers every later exit
				}
			case *ssa.Return:
				return exitPos(fn, in), true
			case *ssa.Panic:
				return exitPos(fn, in), true
			case *ssa.If:
				if skip, ok := failureBranch(in, errVals); ok {
					next := in.Block().Succs[1-skip]
					if !visited[next] {
						visited[next] = true
						return walk(next, 0)
					}
					return token.NoPos, false
				}
			}
		}
		for _, next := range b.Succs {
			if visited[next] {
				continue
			}
			visited[next] = true
			if pos, leaked := walk(next, 0); leaked {
				return pos, true
			}
		}
		return token.NoPos, false
	}

	blk := acq.Block()
	start := 0
	for i, in := range blk.Instrs {
		if in == acq {
			start = i + 1
			break
		}
	}
	return walk(blk, start)
}

// acquireErrValues collects the SSA values holding the acquire's error result
// (the Extract of its last tuple component when that is an error), so the
// failure branch testing it can be recognized and pruned.
func acquireErrValues(acq *ssa.Call) map[ssa.Value]bool {
	out := map[ssa.Value]bool{}
	refs := acq.Referrers()
	if refs == nil {
		return out
	}
	sig := acq.Call.Signature()
	if sig == nil || sig.Results().Len() < 2 {
		return out
	}
	last := sig.Results().Len() - 1
	if sig.Results().At(last).Type().String() != "error" {
		return out
	}
	for _, in := range *refs {
		if ex, ok := in.(*ssa.Extract); ok && ex.Index == last {
			out[ex] = true
		}
	}
	return out
}

// failureBranch recognizes `if <acquireErr> != nil` (or == nil) and returns
// the index of the failed-acquire successor (the arm where the error is
// non-nil), which the walk must not follow: a failed acquire holds no resource.
func failureBranch(ifInstr *ssa.If, errVals map[ssa.Value]bool) (skipSucc int, ok bool) {
	bin, isBin := ifInstr.Cond.(*ssa.BinOp)
	if !isBin || len(errVals) == 0 {
		return 0, false
	}
	xErr, yNil := errVals[bin.X], isNil(bin.Y)
	yErr, xNil := errVals[bin.Y], isNil(bin.X)
	if !(xErr && yNil) && !(yErr && xNil) {
		return 0, false
	}
	switch bin.Op.String() {
	case "!=":
		return 0, true // true arm (Succs[0]) is the failure path
	case "==":
		return 1, true // false arm (Succs[1]) is the failure path
	}
	return 0, false
}

func isNil(v ssa.Value) bool {
	c, ok := v.(*ssa.Const)
	return ok && c.Value == nil
}

// ---- must-precede -----------------------------------------------------------

func checkPrecede(rule *config.ObligationRule, fn *ssa.Function, baseDir string) []Finding {
	require := parseRef(rule.Require)
	before := parseRef(rule.Before)

	type sited struct {
		instr ssa.CallInstruction
		block *ssa.BasicBlock
		index int
	}
	var aSites, bSites []sited
	for _, b := range fn.Blocks {
		for i, in := range b.Instrs {
			call, ok := in.(*ssa.Call) // only a plain call counts: a deferred A runs at exit, after B
			if !ok {
				continue
			}
			if require.matchesCall(call) {
				aSites = append(aSites, sited{call, b, i})
			}
			if before.matchesCall(call) {
				bSites = append(bSites, sited{call, b, i})
			}
		}
	}
	if len(bSites) == 0 {
		return nil
	}

	var out []Finding
	for _, bs := range bSites {
		f := Finding{
			Rule: rule.Name, Kind: config.KindMustPrecede,
			Fn: fn.RelString(nil), Site: site(fn, bs.instr.Common().Pos(), baseDir),
		}
		dominated := false
		for _, as := range aSites {
			if (as.block == bs.block && as.index < bs.index) ||
				(as.block != bs.block && as.block.Dominates(bs.block)) {
				dominated = true
				break
			}
		}
		if dominated {
			f.Status = Satisfied
		} else {
			f.Status = Violated
			f.Detail = fmt.Sprintf("no call to %s dominates this call to %s", rule.Require, rule.Before)
		}
		out = append(out, f)
	}
	return out
}

// ---- positions ---------------------------------------------------------------

// site renders a position as "dir/file.go:NN", relative to baseDir with forward
// slashes — the first source positions ever emitted into graph.json, normalized
// so CI and local runs produce byte-identical output.
func site(fn *ssa.Function, pos token.Pos, baseDir string) string {
	p := fn.Prog.Fset.Position(pos)
	if !p.IsValid() {
		return ""
	}
	name := p.Filename
	if baseDir != "" {
		if rel, err := filepath.Rel(baseDir, name); err == nil && !strings.HasPrefix(rel, "..") {
			name = rel
		}
	} else {
		name = filepath.Base(name)
	}
	return filepath.ToSlash(name) + ":" + strconv.Itoa(p.Line)
}

// exitPos is the best position for an exit instruction: its own, else the
// enclosing function's.
func exitPos(fn *ssa.Function, in ssa.Instruction) token.Pos {
	if p := in.Pos(); p.IsValid() {
		return p
	}
	return fn.Pos()
}
