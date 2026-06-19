package features

import (
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"testing"

	"golang.org/x/tools/go/ssa"
	"golang.org/x/tools/go/ssa/ssautil"

	"github.com/jyang234/golang-code-graph/internal/model"
)

// buildInline compiles one inline source file to SSA and returns every function.
func buildInline(t *testing.T, src string) []*ssa.Function {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "fixture.go", src, 0)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	pkg := types.NewPackage("example.com/fix", "")
	spkg, _, err := ssautil.BuildPackage(
		&types.Config{Importer: importer.Default()}, fset, pkg, []*ast.File{f},
		ssa.SanityCheckFunctions|ssa.InstantiateGenerics)
	if err != nil {
		t.Fatalf("build SSA: %v", err)
	}
	var fns []*ssa.Function
	for fn := range ssautil.AllFunctions(spkg.Prog) {
		fns = append(fns, fn)
	}
	return fns
}

func findInline(t *testing.T, fns []*ssa.Function, name string) *ssa.Function {
	t.Helper()
	for _, fn := range fns {
		if fn.Name() == name {
			return fn
		}
	}
	t.Fatalf("function %q not found", name)
	return nil
}

func firstCall(fn *ssa.Function) (*ssa.Function, ssa.CallInstruction) {
	for _, b := range fn.Blocks {
		for _, in := range b.Instrs {
			c, ok := in.(ssa.CallInstruction)
			if !ok {
				continue
			}
			if sc := c.Common().StaticCallee(); sc != nil {
				return sc, c
			}
		}
	}
	return nil, nil
}

// TestDBEffectClassifiesByVerbNotMethod pins that a DB call's effect is decided
// by the SQL VERB (when the statement is constant), not by the driver method
// name. `INSERT ... RETURNING` rides QueryContext in Postgres, so classifying it
// by method name silently demotes a mutation to a read (ext-read tier) — a
// confidently-wrong, lower-salience attribute. A non-constant statement must
// fail closed to io (never asserted as a read).
func TestDBEffectClassifiesByVerbNotMethod(t *testing.T) {
	const src = `package fix
type DB struct{}
func (DB) QueryContext(q string) {}
func (DB) ExecContext(q string) {}
func UseReturning(db DB) { db.QueryContext("INSERT INTO t (a) VALUES ($1) RETURNING id") }
func UseSelect(db DB)    { db.QueryContext("SELECT a FROM t WHERE id = $1") }
func UseExecDelete(db DB){ db.ExecContext("DELETE FROM t WHERE id = $1") }
func UseDynamic(db DB, q string) { db.QueryContext(q) }
`
	fns := buildInline(t, src)
	cases := []struct {
		fn   string
		want model.Effect
	}{
		{"UseReturning", model.EffectMutate}, // Query* but the verb mutates
		{"UseSelect", model.EffectRead},      // genuine read
		{"UseExecDelete", model.EffectMutate},
		{"UseDynamic", model.EffectIO}, // unknown verb: never asserted a read
	}
	for _, c := range cases {
		callee, site := firstCall(findInline(t, fns, c.fn))
		if site == nil {
			t.Fatalf("%s: no call site", c.fn)
		}
		if got := dbEffect(callee, site); got != c.want {
			t.Errorf("%s: dbEffect = %q, want %q", c.fn, got, c.want)
		}
	}
}

// TestIsConcurrentSiteDeferNotConcurrent pins that a `defer` call is NOT treated
// as a concurrent dispatch. A deferred call runs synchronously at function exit
// on the SAME goroutine — it is not a race — so tagging it concurrent feeds the
// no_concurrent_reach gate a false racy edge and can produce a spurious
// Violation. Only `go` is a concurrent site.
func TestIsConcurrentSiteDeferNotConcurrent(t *testing.T) {
	const src = `package fix
func sink() {}
func WithGo()    { go sink() }
func WithDefer() { defer sink() }
func Direct()    { sink() }
`
	fns := buildInline(t, src)
	cases := []struct {
		fn   string
		want bool
	}{
		{"WithGo", true},
		{"WithDefer", false},
		{"Direct", false},
	}
	for _, c := range cases {
		_, site := firstCall(findInline(t, fns, c.fn))
		if site == nil {
			t.Fatalf("%s: no call site", c.fn)
		}
		if got := IsConcurrentSite(site); got != c.want {
			t.Errorf("%s: IsConcurrentSite = %v, want %v", c.fn, got, c.want)
		}
	}
}

// TestReturnsErrorConcreteType pins that a function returning a CONCRETE error
// type (e.g. *TxError) is classified fallible, matching the obligations/
// effect-order surface (types.Implements). Matching only the exact `error`
// interface under-reports fallibility and makes the two trusted surfaces
// disagree on whether the same function can fail.
func TestReturnsErrorConcreteType(t *testing.T) {
	const src = `package fix
type TxError struct{}
func (*TxError) Error() string { return "" }
func Save() *TxError      { return nil }
func ReadIface() error    { return nil }
func Pure() int           { return 0 }
`
	fns := buildInline(t, src)
	cases := []struct {
		fn   string
		want bool
	}{
		{"Save", true},      // concrete error type
		{"ReadIface", true}, // the error interface itself
		{"Pure", false},
	}
	for _, c := range cases {
		fn := findInline(t, fns, c.fn)
		if got := returnsError(fn.Signature); got != c.want {
			t.Errorf("returnsError(%s) = %v, want %v", c.fn, got, c.want)
		}
	}
}
