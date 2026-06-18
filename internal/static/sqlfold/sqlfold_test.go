package sqlfold_test

import (
	"path/filepath"
	"runtime"
	"testing"

	"golang.org/x/tools/go/ssa"
	"golang.org/x/tools/go/ssa/ssautil"

	"github.com/jyang234/golang-code-graph/internal/static/analyze"
	"github.com/jyang234/golang-code-graph/internal/static/features"
	"github.com/jyang234/golang-code-graph/internal/static/sqlfold"
)

// dbSite is a recovered DB call site: the enclosing method and the fold's verdict.
type dbSite struct {
	method    string
	op, table string
	ok        bool
}

// foldFixture analyzes the builder fixture and runs the fold over every
// database/sql query call (ExecContext/QueryContext/QueryRowContext), keyed by the
// enclosing method name so a test can assert one site without depending on order.
func foldFixture(t *testing.T) map[string]dbSite {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	dir := filepath.Join(filepath.Dir(file), "..", "..", "..", "testdata", "fixtures", "sqlbuildersvc")
	res, err := analyze.Analyze(dir)
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}
	out := map[string]dbSite{}
	for fn := range ssautil.AllFunctions(res.Program.Prog) {
		if fn.Blocks == nil {
			continue
		}
		for _, b := range fn.Blocks {
			for _, instr := range b.Instrs {
				call, ok := instr.(*ssa.Call)
				if !ok {
					continue
				}
				callee := call.Common().StaticCallee()
				if callee == nil || !isDBQuery(callee.Name()) {
					continue
				}
				args := features.StringArgs(call)
				if len(args) == 0 {
					continue
				}
				op, table, ok := sqlfold.Recover(args[0])
				out[fn.Name()] = dbSite{method: fn.Name(), op: op, table: table, ok: ok}
			}
		}
	}
	return out
}

func isDBQuery(name string) bool {
	switch name {
	case "ExecContext", "QueryContext", "QueryRowContext":
		return true
	}
	return false
}

func TestFoldReadIsConstantSelect(t *testing.T) {
	sites := foldFixture(t)
	s := sites["GetMessage"]
	if !s.ok || s.op != "SELECT" || s.table != "messages" {
		t.Errorf("GetMessage: want SELECT messages (ok), got %+v", s)
	}
}

func TestFoldRecoversWriteRidingQueryRow(t *testing.T) {
	// INSERT … RETURNING executed via QueryRowContext: the method name says read,
	// only the recovered verb says write. The F-B case.
	s := foldFixture(t)["CreateMessage"]
	if !s.ok || s.op != "INSERT" || s.table != "messages" {
		t.Errorf("CreateMessage: want INSERT messages (ok), got %+v", s)
	}
}

func TestFoldPromotesWriteWithDynamicTable(t *testing.T) {
	// "DELETE FROM " + table: the verb is constant, the table a hole. Write
	// promotion recovers DELETE with an unnamed table.
	s := foldFixture(t)["DeleteByTable"]
	if !s.ok || s.op != "DELETE" || s.table != "" {
		t.Errorf("DeleteByTable: want DELETE with empty table (ok), got %+v", s)
	}
}

func TestFoldPromotesWriteUnderBranchedTail(t *testing.T) {
	// The verb+table fragment is unconditional; the conditional SET-list tail must
	// not block write promotion, and must not be read as part of the prefix.
	s := foldFixture(t)["UpdatePartial"]
	if !s.ok || s.op != "UPDATE" || s.table != "accounts" {
		t.Errorf("UpdatePartial: want UPDATE accounts (ok), got %+v", s)
	}
}

func TestFoldAbstainsWhenVerbIsDynamic(t *testing.T) {
	// The verb itself is a runtime value: nothing is recoverable, so the fold must
	// abstain (fail closed) rather than guess.
	s := foldFixture(t)["ExecOpaque"]
	if s.ok {
		t.Errorf("ExecOpaque: want abstain (ok=false), got %+v", s)
	}
}

// The prime-directive guard: a SELECT-prefixed statement with a dynamic TEXT
// splice must NOT be classified as a read, because the splice could smuggle a
// second, mutating statement. The verb is SELECT but the statement is incomplete,
// so the fold abstains rather than assert a false read (a false non-mutation).
func TestFoldAbstainsOnDynamicTextSpliceInSelect(t *testing.T) {
	s := foldFixture(t)["ReadDynamicFilter"]
	if s.ok {
		t.Errorf("ReadDynamicFilter: a SELECT with a dynamic text splice must abstain, got %+v", s)
	}
}
