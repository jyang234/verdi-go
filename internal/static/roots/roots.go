// Package roots discovers the entry points a call graph must be rooted at. For a
// service, main is not enough: the real entry points are the HTTP handlers and
// bus consumers that frameworks register through dynamic dispatch
// (router.HandleFunc, bus.Subscribe), leaving them reachable-but-disconnected in
// a naive graph. roots finds those registration calls and resolves the
// func-value argument of each to a concrete function — a synthetic root tagged
// with its route or topic.
//
//	roots = mains ∪ HTTP handlers ∪ bus consumers ∪ (library) exports
//
// A registration whose handler cannot be resolved to a concrete function is not
// dropped: it is recorded as a blind spot, mirroring the rest of the static
// pipeline's disclose-where-blind discipline.
package roots

import (
	"go/constant"
	"sort"

	"golang.org/x/tools/go/ssa"
	"golang.org/x/tools/go/ssa/ssautil"

	"github.com/jyang234/golang-code-graph/internal/static/ssabuild"
)

// Kind classifies a discovered root.
type Kind string

const (
	KindMain     Kind = "main"     // a command's main function
	KindHTTP     Kind = "http"     // an HTTP handler registered on a router
	KindConsumer Kind = "consumer" // a bus consumer registered via subscribe
	KindExport   Kind = "export"   // an exported function (library fallback)
)

// Root is a synthetic entry point for call-graph construction.
type Root struct {
	Func *ssa.Function
	Kind Kind
	// Name is the route ("POST /loan-application") or topic ("payment.settled")
	// the handler was registered under, or "" for mains and exports.
	Name string
}

// FQN is the root's fully-qualified identity.
func (r Root) FQN() string { return r.Func.RelString(nil) }

// Registrar names a framework registration call whose func-value argument is a
// synthetic root. Arg indices are LOGICAL parameter positions (excluding any
// receiver); root discovery adds the receiver offset when the registrar is a
// method, so the same hint shape works for methods and free functions.
type Registrar struct {
	PkgPath    string // import path of the registrar's package, e.g. "net/http"
	Name       string // function/method name, e.g. "HandleFunc" or "Subscribe"
	Kind       Kind   // the kind of root its handler argument becomes
	NameArg    int    // logical position of the route/topic string, or -1 if none
	HandlerArg int    // logical position of the func-value argument
}

// BlindSpot records a registration whose handler could not be resolved to a
// concrete function — an unresolved dynamic dispatch at an entry point.
type BlindSpot struct {
	Registrar string // the registrar that was called
	Pos       string // source position of the call
	Detail    string // why the handler could not be resolved
}

// Result is the discovered root set plus disclosed gaps, both sorted for
// determinism.
type Result struct {
	Roots      []Root
	BlindSpots []BlindSpot
}

// Funcs returns just the root functions, in Result.Roots order — the input RTA
// and the other algorithms expect.
func (r *Result) Funcs() []*ssa.Function {
	fns := make([]*ssa.Function, len(r.Roots))
	for i, rt := range r.Roots {
		fns[i] = rt.Func
	}
	return fns
}

// HTTPRegistrars are the built-in HTTP registration hints (stdlib's ServeMux).
// Bus registrars are service-specific and come from the classification hints, so
// callers append them.
func HTTPRegistrars() []Registrar {
	return []Registrar{
		{PkgPath: "net/http", Name: "HandleFunc", Kind: KindHTTP, NameArg: 0, HandlerArg: 1},
	}
}

// Discover finds the synthetic roots of prog given the registration hints. When
// the unit has no mains and no registered handlers, it falls back to the unit's
// exported functions (library mode).
func Discover(prog *ssabuild.Program, registrars []Registrar) *Result {
	res := &Result{}
	seen := make(map[*ssa.Function]bool)

	add := func(fn *ssa.Function, kind Kind, name string) {
		if fn == nil || seen[fn] {
			return
		}
		seen[fn] = true
		res.Roots = append(res.Roots, Root{Func: fn, Kind: kind, Name: name})
	}

	// mains.
	for _, p := range ssautil.MainPackages(prog.ServicePkgs) {
		if mainFn := p.Func("main"); mainFn != nil {
			add(mainFn, KindMain, "")
		}
	}

	// HTTP handlers and bus consumers, from registration calls in first-party code.
	byKey := indexRegistrars(registrars)
	for fn := range ssautil.AllFunctions(prog.Prog) {
		if fn.Pkg == nil || !prog.IsFirstParty(fn.Pkg) {
			continue
		}
		for _, b := range fn.Blocks {
			for _, instr := range b.Instrs {
				call, ok := instr.(ssa.CallInstruction)
				if !ok {
					continue
				}
				cc := call.Common()
				callee := cc.StaticCallee()
				if callee == nil {
					continue
				}
				reg, ok := byKey[regKey{pkgPath(callee), callee.Name()}]
				if !ok {
					continue
				}
				isMethod := callee.Signature.Recv() != nil
				handler, name := resolveRegistration(cc, reg, isMethod)
				if handler == nil {
					res.BlindSpots = append(res.BlindSpots, BlindSpot{
						Registrar: callee.RelString(nil),
						Pos:       posOf(call),
						Detail:    "handler argument is not a statically resolvable function",
					})
					continue
				}
				add(handler, reg.Kind, name)
			}
		}
	}

	// Library fallback: nothing to root from, so root at every exported function.
	if len(res.Roots) == 0 {
		for _, p := range prog.ServicePkgs {
			for _, m := range p.Members {
				if fn, ok := m.(*ssa.Function); ok && fn.Object() != nil && fn.Object().Exported() {
					add(fn, KindExport, "")
				}
			}
		}
	}

	sortResult(res)
	return res
}

type regKey struct{ pkgPath, name string }

func indexRegistrars(rs []Registrar) map[regKey]Registrar {
	m := make(map[regKey]Registrar, len(rs))
	for _, r := range rs {
		m[regKey{r.PkgPath, r.Name}] = r
	}
	return m
}

// resolveRegistration extracts the handler function and the registered name from
// one registration call. A method call carries its receiver at Args[0], so
// logical positions shift by one.
func resolveRegistration(cc *ssa.CallCommon, reg Registrar, isMethod bool) (*ssa.Function, string) {
	off := 0
	if isMethod {
		off = 1
	}
	handlerIdx := reg.HandlerArg + off
	if handlerIdx < 0 || handlerIdx >= len(cc.Args) {
		return nil, ""
	}
	handler := resolveHandler(cc.Args[handlerIdx])
	if handler == nil {
		return nil, ""
	}
	name := ""
	if reg.NameArg >= 0 {
		nameIdx := reg.NameArg + off
		if nameIdx >= 0 && nameIdx < len(cc.Args) {
			name = constString(cc.Args[nameIdx])
		}
	}
	return handler, name
}

// resolveHandler peels framework wrappers off a func-value argument and returns
// the concrete function it denotes, or nil if it is not statically resolvable. It
// unwraps named-type conversions (a method value converted to the registrar's
// handler type) and resolves bound-method and thunk wrappers to the underlying
// method, while leaving genuine closures intact.
func resolveHandler(v ssa.Value) *ssa.Function {
	switch t := v.(type) {
	case *ssa.MakeClosure:
		if fn, ok := t.Fn.(*ssa.Function); ok {
			return realFunc(fn)
		}
	case *ssa.Function:
		return realFunc(t)
	case *ssa.ChangeType:
		return resolveHandler(t.X)
	case *ssa.Convert:
		return resolveHandler(t.X)
	case *ssa.MakeInterface:
		return resolveHandler(t.X)
	}
	return nil
}

// realFunc resolves a synthetic bound-method or thunk wrapper to the method it
// wraps, so the root carries the real handler's identity rather than a
// "$bound"/"$thunk" name. Non-synthetic functions (declared funcs, closures) are
// returned unchanged.
func realFunc(fn *ssa.Function) *ssa.Function {
	if fn.Synthetic == "" {
		return fn
	}
	for _, b := range fn.Blocks {
		for _, instr := range b.Instrs {
			if call, ok := instr.(ssa.CallInstruction); ok {
				if callee := call.Common().StaticCallee(); callee != nil {
					return callee
				}
			}
		}
	}
	return fn
}

func resultLess(a, b Root) bool {
	if a.Kind != b.Kind {
		return a.Kind < b.Kind
	}
	if a.Name != b.Name {
		return a.Name < b.Name
	}
	return a.FQN() < b.FQN()
}

func sortResult(res *Result) {
	sort.Slice(res.Roots, func(i, j int) bool { return resultLess(res.Roots[i], res.Roots[j]) })
	sort.Slice(res.BlindSpots, func(i, j int) bool {
		a, b := res.BlindSpots[i], res.BlindSpots[j]
		if a.Registrar != b.Registrar {
			return a.Registrar < b.Registrar
		}
		if a.Pos != b.Pos {
			return a.Pos < b.Pos
		}
		return a.Detail < b.Detail
	})
}

// pkgPath returns the import path of fn's package, or "" for synthetic functions
// with no package.
func pkgPath(fn *ssa.Function) string {
	if fn.Pkg == nil || fn.Pkg.Pkg == nil {
		return ""
	}
	return fn.Pkg.Pkg.Path()
}

// constString returns the Go string value of v if it is a constant string, else
// "" — a non-constant name is not statically knowable.
func constString(v ssa.Value) string {
	c, ok := v.(*ssa.Const)
	if !ok || c.Value == nil || c.Value.Kind() != constant.String {
		return ""
	}
	return constant.StringVal(c.Value)
}

func posOf(instr ssa.Instruction) string {
	if instr.Parent() == nil {
		return ""
	}
	fset := instr.Parent().Prog.Fset
	return fset.Position(instr.Pos()).String()
}
