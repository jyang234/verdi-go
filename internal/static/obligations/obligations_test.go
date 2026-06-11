package obligations

import (
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"strings"
	"testing"

	"golang.org/x/tools/go/ssa"
	"golang.org/x/tools/go/ssa/ssautil"

	"github.com/jyang234/golang-code-graph/internal/config"
)

// buildSSA compiles one inline source file into SSA and returns every function
// in the package (methods included).
func buildSSA(t *testing.T, src string) []*ssa.Function {
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
		if fn.Pkg == spkg {
			fns = append(fns, fn)
		}
	}
	return fns
}

const txSrc = `package fix

type Tx struct{ closed bool }
type Store struct{ tx *Tx }

func (s *Store) BeginTx() (*Tx, error) { return &Tx{}, nil }
func (t *Tx) Commit() error            { t.closed = true; return nil }
func (t *Tx) Rollback()                { t.closed = true }

func debit(t *Tx) error  { return nil }
func credit(t *Tx) error { return nil }

// The worked example: the debit-failure return leaks; the acquire's own
// failure branch must NOT count as a leak (a failed acquire holds nothing).
func Transfer(s *Store) error {
	tx, err := s.BeginTx()
	if err != nil {
		return err
	}
	if err := debit(tx); err != nil {
		return err
	}
	if err := credit(tx); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

func TransferDefer(s *Store) error {
	tx, err := s.BeginTx()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := debit(tx); err != nil {
		return err
	}
	return tx.Commit()
}

// Ownership leaves the function: the open tx is returned.
func TransferOwn(s *Store) (*Tx, error) {
	tx, err := s.BeginTx()
	if err != nil {
		return nil, err
	}
	return tx, nil
}

func TransferRecover(s *Store) error {
	defer func() { _ = recover() }()
	tx, err := s.BeginTx()
	if err != nil {
		return err
	}
	return tx.Commit()
}

func TransferLoop(s *Store, n int) error {
	for i := 0; i < n; i++ {
		tx, err := s.BeginTx()
		if err != nil {
			return err
		}
		if err := debit(tx); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}
`

func txRule() config.ObligationRule {
	return config.ObligationRule{
		Name:    "tx-must-close",
		Acquire: "example.com/fix#BeginTx",
		Release: []string{"example.com/fix#Commit", "example.com/fix#Rollback"},
	}
}

// one returns the single finding for the free function named name in the
// fixture package, by exact FQN.
func one(t *testing.T, fs []Finding, name string) Finding {
	t.Helper()
	fqn := "example.com/fix." + name
	var got []Finding
	for _, f := range fs {
		if f.Fn == fqn {
			got = append(got, f)
		}
	}
	if len(got) != 1 {
		t.Fatalf("%s: want 1 finding, got %v (all: %v)", fqn, got, fs)
	}
	return got[0]
}

func TestMustReleaseVerdicts(t *testing.T) {
	fns := buildSSA(t, txSrc)
	fs := Check([]config.ObligationRule{txRule()}, fns, "")

	leak := one(t, fs, "Transfer")
	if leak.Status != Violated {
		t.Errorf("Transfer = %s (%s), want VIOLATED", leak.Status, leak.Detail)
	}
	if !strings.Contains(leak.Detail, "fixture.go:") {
		t.Errorf("Transfer detail = %q, want an exit site", leak.Detail)
	}
	if !strings.HasPrefix(leak.Site, "fixture.go:") {
		t.Errorf("Transfer site = %q, want fixture.go:NN", leak.Site)
	}

	if f := one(t, fs, "TransferDefer"); f.Status != Satisfied {
		t.Errorf("TransferDefer = %s (%s), want SATISFIED (defer covers all exits)", f.Status, f.Detail)
	}
	if f := one(t, fs, "TransferOwn"); f.Status != CantProve || !strings.Contains(f.Detail, "returned") {
		t.Errorf("TransferOwn = %s (%s), want CANT-PROVE/returned", f.Status, f.Detail)
	}
	if f := one(t, fs, "TransferRecover"); f.Status != CantProve || !strings.Contains(f.Detail, "recover") {
		t.Errorf("TransferRecover = %s (%s), want CANT-PROVE/recover", f.Status, f.Detail)
	}
	if f := one(t, fs, "TransferLoop"); f.Status != Satisfied {
		t.Errorf("TransferLoop = %s (%s), want SATISFIED (release on every arm, loop back-edge is not an exit)", f.Status, f.Detail)
	}
}

// Acquiring through an interface must bind the rule both ways: the rule names
// the interface's package#method and the call site is invoke-mode.
func TestMustReleaseThroughInterface(t *testing.T) {
	src := `package fix

type Tx struct{}
func (t *Tx) Commit() error { return nil }

type Beginner interface{ BeginTx() (*Tx, error) }

func Run(b Beginner) error {
	tx, err := b.BeginTx()
	if err != nil {
		return err
	}
	_ = tx
	return nil // leak: no release on this path
}
`
	fns := buildSSA(t, src)
	fs := Check([]config.ObligationRule{{
		Name:    "tx-must-close",
		Acquire: "example.com/fix#BeginTx",
		Release: []string{"example.com/fix#Commit"},
	}}, fns, "")
	if f := one(t, fs, "Run"); f.Status != Violated {
		t.Errorf("invoke-mode acquire = %s (%s), want VIOLATED", f.Status, f.Detail)
	}
}

const auditSrc = `package fix

func audit(event string)   {}
func publish(event string) {}

// SATISFIED: the audit dominates the publish.
func Disburse(ok bool) {
	audit("loan.approved")
	if ok {
		publish("loan.approved")
	}
}

// VIOLATED: one branch publishes without auditing.
func DisburseRacy(ok bool) {
	if ok {
		audit("loan.approved")
	}
	publish("loan.approved")
}

// Same block: order decides.
func SameBlockGood() { audit("x"); publish("x") }
func SameBlockBad()  { publish("x"); audit("x") }

// A deferred audit runs at exit, AFTER the publish: it must not satisfy.
func DeferredAudit() {
	defer audit("x")
	publish("x")
}
`

func auditRule() config.ObligationRule {
	return config.ObligationRule{
		Name:    "audit-before-publish",
		Require: "example.com/fix#audit",
		Before:  "example.com/fix#publish",
	}
}

func TestMustPrecedeVerdicts(t *testing.T) {
	fns := buildSSA(t, auditSrc)
	fs := Check([]config.ObligationRule{auditRule()}, fns, "")

	cases := map[string]Status{
		"Disburse":      Satisfied,
		"DisburseRacy":  Violated,
		"SameBlockGood": Satisfied,
		"SameBlockBad":  Violated,
		"DeferredAudit": Violated,
	}
	for fn, want := range cases {
		if f := one(t, fs, fn); f.Status != want {
			t.Errorf("%s = %s (%s), want %s", fn, f.Status, f.Detail, want)
		}
	}
}

func TestUnmatchedRuleDisclosed(t *testing.T) {
	fns := buildSSA(t, txSrc)
	fs := Check([]config.ObligationRule{{
		Name:    "renamed-away",
		Acquire: "example.com/fix#BeginTransaction", // no such symbol anymore
		Release: []string{"example.com/fix#Commit"},
	}}, fns, "")
	if len(fs) != 1 || fs[0].Status != Unmatched || fs[0].Fn != "" {
		t.Fatalf("want one UNMATCHED finding with no fn, got %v", fs)
	}
	if !strings.Contains(fs[0].Detail, "inert") {
		t.Errorf("detail = %q, want the inert-guardrail wording", fs[0].Detail)
	}
}

func TestCheckDeterministic(t *testing.T) {
	fns := buildSSA(t, txSrc)
	rules := []config.ObligationRule{txRule()}
	a, b := Check(rules, fns, ""), Check(rules, fns, "")
	if len(a) != len(b) {
		t.Fatalf("non-deterministic finding count: %d vs %d", len(a), len(b))
	}
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("non-deterministic finding %d: %v vs %v", i, a[i], b[i])
		}
	}
}
