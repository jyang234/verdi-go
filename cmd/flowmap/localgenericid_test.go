package main

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/jyang234/golang-code-graph/internal/static/graphio"
)

// The witnesses under testdata/fixtures/localgenericid are the permanent
// regression fixtures for function-local generic type identity. Every one of them
// is a valid Go program that flowmap refused before this design revision, and
// each pins a DIFFERENT mechanism:
//
//	f3a  swapped two-parameter roles over two structurally identical locals
//	f3b  the same swap collapsed into one type argument via a generic container
//	f3c  repeated struct roles (struct{X A; Y B; Z A}) — the global-seen-set case
//	n1   a local declared inside a GENERIC function (nil Parent after instantiation)
//	n1b  the DECLARED RESIDUAL — must keep failing closed
//	n1c  a local whose structure varies with the enclosing type parameter
//	n2   promoted-method wrappers over function-local receivers
//	f26  two packages, equal file basenames, offset-aligned local declarations
//	rec  a recursive function-local named type
//
// They are driven through the BUILT BINARY in separate processes: the residual
// refusal is a panic, which an in-process call cannot observe without unwinding
// the test, and the determinism evidence is only worth anything across processes.

// witnessBinary builds cmd/flowmap once per test binary run and returns its path.
func witnessBinary(t *testing.T) string {
	t.Helper()
	witnessBuildOnce.Do(func() {
		witnessBuildDir, witnessBuildErr = os.MkdirTemp("", "flowmap-witness")
		if witnessBuildErr != nil {
			return
		}
		witnessBinaryPath = filepath.Join(witnessBuildDir, "flowmap")
		_, file, _, _ := runtime.Caller(0)
		cmd := exec.Command("go", "build", "-o", witnessBinaryPath, ".")
		cmd.Dir = filepath.Dir(file)
		if out, err := cmd.CombinedOutput(); err != nil {
			witnessBuildErr = fmt.Errorf("go build ./cmd/flowmap: %v\n%s", err, out)
		}
	})
	if witnessBuildErr != nil {
		t.Fatalf("build flowmap: %v", witnessBuildErr)
	}
	return witnessBinaryPath
}

var (
	witnessBuildOnce  sync.Once
	witnessBuildDir   string
	witnessBinaryPath string
	witnessBuildErr   error
)

func TestMain(m *testing.M) {
	code := m.Run()
	if witnessBuildDir != "" {
		_ = os.RemoveAll(witnessBuildDir)
	}
	os.Exit(code)
}

func witnessDir(t *testing.T, name string) string {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..", "testdata", "fixtures", "localgenericid", name)
}

// graphWitness runs `flowmap graph --algo <algo> <witness>` in a SEPARATE
// PROCESS and returns its stdout, stderr and exit code. GOWORK=off because the
// witnesses are standalone service modules, not workspace members.
func graphWitness(t *testing.T, name, algo string) (stdout, stderr string, exitCode int) {
	t.Helper()
	return graphServiceDir(t, witnessDir(t, name), algo)
}

// graphServiceDir is graphWitness over an arbitrary service directory, so the
// determinism runs can also cover the shipped localtypeargsvc fixture.
func graphServiceDir(t *testing.T, dir, algo string) (stdout, stderr string, exitCode int) {
	t.Helper()
	cmd := exec.Command(witnessBinary(t), "graph", "--algo", algo, dir)
	cmd.Env = append(os.Environ(), "GOWORK=off")
	var out, errOut strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &errOut
	err := cmd.Run()
	if err != nil {
		var exit *exec.ExitError
		if !asExitError(err, &exit) {
			t.Fatalf("run flowmap graph --algo %s %s: %v", algo, dir, err)
		}
		exitCode = exit.ExitCode()
	}
	return out.String(), errOut.String(), exitCode
}

func asExitError(err error, target **exec.ExitError) bool {
	exit, ok := err.(*exec.ExitError)
	if ok {
		*target = exit
	}
	return ok
}

// countFQN returns how many node records the graph carries for fqn. A generic
// instantiated at several function-local types legally produces several records
// for ONE display FQN; that is the whole point of the discriminator.
func countFQN(t *testing.T, stdout, fqn string) int {
	t.Helper()
	var g graphio.Graph
	if err := json.Unmarshal([]byte(stdout), &g); err != nil {
		t.Fatalf("decode graph: %v", err)
	}
	n := 0
	for _, node := range g.Nodes {
		if node.FQN == fqn {
			n++
		}
	}
	return n
}

// TestLocalGenericIdentityWitnessesGraph is the acceptance gate for the witnesses
// that must now succeed.
//
// Exit 0 IS the distinct-discriminator assertion: two distinct *ssa.Function that
// shared a sort key would reach callgraph.finalize's guard and panic. The node
// counts are the second half — they prove neither instance was quietly merged
// away, which would be the one failure mode worse than the panic.
func TestLocalGenericIdentityWitnessesGraph(t *testing.T) {
	tests := []struct {
		name  string
		algos []string
		fqn   string
		want  int
	}{
		{name: "f3a", algos: []string{"rta", "vta", "cha"}, fqn: "example.com/f3a.pair[example.com/f3a.result example.com/f3a.result]", want: 2},
		{name: "f3b", algos: []string{"rta", "vta", "cha"}, fqn: "example.com/f3b.mk[example.com/f3b.result example.com/f3b.result]", want: 2},
		{name: "f3b_container", algos: []string{"rta", "vta", "cha"}, fqn: "example.com/f3b.report[example.com/f3b.box[example.com/f3b.result, example.com/f3b.result]]", want: 2},
		{name: "f3c", algos: []string{"rta", "vta", "cha"}, fqn: "example.com/f3c.mkS[example.com/f3c.result example.com/f3c.result]", want: 2},
		// n1 is deliberately NOT run under cha here: the whole-program scope also
		// analyzes the UNINSTANTIATED body of gen, whose sink[L] comes from the same
		// one declaration with the same structure. That is the declared residual
		// class reached through a different door, and it is pinned as still-refused
		// by TestLocalGenericIdentityResidualStaysRefused.
		{name: "n1", algos: []string{"rta", "vta"}, fqn: "example.com/n1.sink[example.com/n1.L]", want: 2},
		{name: "n1c", algos: []string{"rta", "vta"}, fqn: "example.com/n1c.sink[example.com/n1c.L]", want: 2},
		// Under cha the uninstantiated body contributes a third sink[L], separated
		// from both instantiations because L's structure mentions X.
		{name: "n1c_wholeprogram", algos: []string{"cha"}, fqn: "example.com/n1c.sink[example.com/n1c.L]", want: 3},
		{name: "f26", algos: []string{"rta", "vta", "cha"}, fqn: "example.com/f26/mm.P[example.com/f26/q1.L example.com/f26/q2.L]", want: 2},
		{name: "rec", algos: []string{"rta", "vta", "cha"}, fqn: "example.com/rec.sink[example.com/rec.R]", want: 1},
	}

	for _, test := range tests {
		for _, algo := range test.algos {
			t.Run(test.name+"/"+algo, func(t *testing.T) {
				name, _, _ := strings.Cut(test.name, "_")
				stdout, stderr, code := graphWitness(t, name, algo)
				if code != 0 {
					t.Fatalf("graph --algo %s ./%s exited %d:\n%s", algo, name, code, stderr)
				}
				if got := countFQN(t, stdout, test.fqn); got != test.want {
					t.Fatalf("graph --algo %s ./%s kept %d node records for %q, want %d",
						algo, name, got, test.fqn, test.want)
				}
			})
		}
	}
}

// TestLocalGenericIdentityWitnessN2Graphs covers the promoted-method wrapper
// witness. go/ssa's promotion wrappers are spliced out of the emitted graph, so
// the CLI cannot show them; exit 0 is the assertion that their two receivers were
// told apart (equal keys would panic), and the two promoted methods surviving as
// nodes is the assertion that neither call was dropped. The wrapper keys
// themselves are pinned in features'
// TestInstanceDiscriminatorSeparatesPromotedLocalReceiverWrappers, for both the
// value and the pointer receiver form.
func TestLocalGenericIdentityWitnessN2Graphs(t *testing.T) {
	for _, algo := range []string{"rta", "vta", "cha"} {
		t.Run(algo, func(t *testing.T) {
			stdout, stderr, code := graphWitness(t, "n2", algo)
			if code != 0 {
				t.Fatalf("graph --algo %s ./n2 exited %d:\n%s", algo, code, stderr)
			}
			for _, fqn := range []string{
				"(example.com/n2.embA).QueryContext",
				"(example.com/n2.embB).QueryContext",
			} {
				if got := countFQN(t, stdout, fqn); got != 1 {
					t.Errorf("graph --algo %s ./n2 kept %d records for %q, want 1", algo, got, fqn)
				}
			}
		})
	}
}

// TestLocalGenericIdentityResidualStaysRefused is the fixture that must NEVER go
// green without a ratified change to "Residual undecided classes". If it does,
// two distinct *ssa.Function were collapsed into one node and every absence proof
// downstream now covers a function the analysis never examined — a silent false
// PROVEN, which is strictly worse than the refusal.
func TestLocalGenericIdentityResidualStaysRefused(t *testing.T) {
	tests := []struct {
		name  string
		algos []string
		fqn   string
	}{
		{name: "n1b", algos: []string{"rta", "vta", "cha"}, fqn: "example.com/n1b.sink[example.com/n1b.L]"},
		// The cha sub-case: gen is instantiated ONCE, but the whole-program scope
		// also analyzes its uninstantiated body, so two distinct sink[L] come from
		// one declaration with identical structure. Same undecidable class.
		{name: "n1", algos: []string{"cha"}, fqn: "example.com/n1.sink[example.com/n1.L]"},
	}

	for _, test := range tests {
		for _, algo := range test.algos {
			t.Run(test.name+"/"+algo, func(t *testing.T) {
				_, stderr, code := graphWitness(t, test.name, algo)
				if code == 0 {
					t.Fatalf("graph --algo %s ./%s SUCCEEDED; the residual class must fail closed", algo, test.name)
				}
				for _, want := range []string{
					"callgraph: refusing to order two distinct instances of",
					test.fqn,
					`ONE declaration of the function-local`,
					`type "L" at main.go:`,
					"This is a DISCLOSED LIMIT of the function-local type discriminator, not a crash",
					"WHAT YOU CAN DO — any one of:",
					"move the type declaration out of the generic function",
					"instantiate the enclosing generic function only once",
					"UNKNOWN collision class",
					"local-type-graph/v1",
				} {
					if !strings.Contains(stderr, want) {
						t.Errorf("refusal of ./%s under %s is missing %q:\n%s", test.name, algo, want, stderr)
					}
				}
				if strings.Contains(stderr, "share sort key") {
					t.Errorf("refusal of ./%s under %s fell back to the unknown-class wording:\n%s", test.name, algo, stderr)
				}
			})
		}
	}
}

// TestF26DeclarationOffsetsStayAligned keeps the f26 witness armed. Its whole
// point is that q1/local.go and q2/local.go share a basename AND place their
// `type L` declarations at the same byte offsets, so the two mm.P instances draw
// the same two (basename, offset) pairs in swapped order. Editing either file
// without re-aligning the padding would leave a fixture that still compiles,
// still graphs, and proves nothing — a silently disarmed regression test.
func TestF26DeclarationOffsetsStayAligned(t *testing.T) {
	offsets := func(pkg string) []int {
		path := filepath.Join(witnessDir(t, "f26"), pkg, "local.go")
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		file := fset.File(f.Pos())
		var got []int
		ast.Inspect(f, func(n ast.Node) bool {
			if ts, ok := n.(*ast.TypeSpec); ok && ts.Name.Name == "L" {
				got = append(got, file.Offset(ts.Name.Pos()))
			}
			return true
		})
		return got
	}

	q1, q2 := offsets("q1"), offsets("q2")
	if len(q1) != 2 || len(q2) != 2 {
		t.Fatalf("declaration counts = q1:%d q2:%d, want two `type L` in each", len(q1), len(q2))
	}
	if q1[0] == q1[1] {
		t.Fatalf("q1's two declarations share offset %d; the witness needs two distinct offsets", q1[0])
	}
	if q1[0] != q2[0] || q1[1] != q2[1] {
		t.Fatalf("declaration offsets are no longer aligned: q1=%v q2=%v", q1, q2)
	}
}

// TestLocalGenericIdentityDeterministicAcrossProcesses is the determinism
// evidence that counts. Re-deriving discriminators N times from ONE already-built
// *ssa.Program proves only that a pure function is pure; it cannot detect
// variance in how much node sharing go/types happened to perform, which is what
// the encoding's node ids depend on. These runs rebuild the whole
// loader -> SSA -> discriminator pipeline in a FRESH PROCESS every time.
//
// What must not vary is the panic decision: a program that graphs cleanly on one
// run and refuses on the next would violate determinism outright. Byte-identical
// stdout over 20 processes is the observable form of that.
func TestLocalGenericIdentityDeterministicAcrossProcesses(t *testing.T) {
	// A slice, not a map: nothing in this repository iterates a map where the
	// order is observable, and a subtest order that varies run to run is exactly
	// the habit this codebase does not keep.
	subjects := []struct{ name, dir string }{
		{name: "f3c", dir: witnessDir(t, "f3c")},
		{name: "f26", dir: witnessDir(t, "f26")},
		{name: "n2", dir: witnessDir(t, "n2")},
		{name: "localtypeargsvc", dir: localTypeArgFixtureDir()},
	}
	for _, subject := range subjects {
		witness, dir := subject.name, subject.dir
		t.Run(witness, func(t *testing.T) {
			var want string
			for run := 0; run < 20; run++ {
				stdout, stderr, code := graphServiceDir(t, dir, "vta")
				if code != 0 {
					t.Fatalf("run %d of ./%s exited %d:\n%s", run, witness, code, stderr)
				}
				if run == 0 {
					want = stdout
					continue
				}
				if stdout != want {
					t.Fatalf("run %d of ./%s produced different graph bytes", run, witness)
				}
			}
		})
	}
}
