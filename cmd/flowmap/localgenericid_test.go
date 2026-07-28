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
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/jyang234/golang-code-graph/internal/static/analyze"
	"github.com/jyang234/golang-code-graph/internal/static/callgraph"
	"github.com/jyang234/golang-code-graph/internal/static/features"
	"github.com/jyang234/golang-code-graph/internal/static/graphio"
)

// The witnesses under testdata/fixtures/localgenericid are the permanent
// regression fixtures for function-local generic type identity. Every one of them
// is a valid Go program that flowmap refused before this design revision, and
// each pins a DIFFERENT mechanism:
//
//	f3a    swapped two-parameter roles over two structurally identical locals
//	f3b    the same swap collapsed into one type argument via a generic container
//	f3c    repeated struct roles (struct{X A; Y B; Z A}) — the global-seen-set case
//	n1     a local declared inside a GENERIC function (nil Parent after instantiation)
//	n1b    the DECLARED RESIDUAL — must keep failing closed
//	n1c    a local whose structure varies with the enclosing type parameter
//	n1lib  the residual reached with ONE instantiation, under rta and vta
//	n2     promoted-method wrappers over function-local receivers
//	f26    two packages, equal file basenames, offset-aligned local declarations
//	rec    a recursive function-local named type
//
// Three more are the shapes the residual's blast radius reaches OTHER than
// through a call to a generic function, which is the only shape the design's
// earlier shape-list phrasing described:
//
//	n1method        the local becomes a type argument of a generic TYPE; the refused
//	                function is that type's METHOD, (Box[L]).Show[L]
//	n1methodnocall  the same, with the method never called — "built", not "called",
//	                is the threshold
//	n1recv          the local reaches the RECEIVER root of a promotion wrapper, which
//	                carries no type arguments at all
//	n1thunk         the same through a $thunk, whose receiver is its first PARAMETER
//	                and which therefore has no Signature receiver either
//
// One is the FABRICATED-EDGE witness, and it asserts edges rather than nodes:
//
//	thunkmerge      two $thunk forwarders over two same-rendering function-local
//	                types, with DISJOINT out-edges under vta. mergeKey merged them
//	                and emitted the union; the fix keeps them apart.
//
// Three more are the enclosing bodies and root positions a blast radius written
// as "a GENERIC FUNCTION declares L, and L IS one of the callee's type
// arguments" leaves out, each confirmed by execution:
//
//	n1closure       L is declared in a CLOSURE nested in a generic function
//	n1typemethod    L is declared in a METHOD OF A GENERIC TYPE; no generic function
//	                DECLARES it (sink[T] is the callee, not the declaring body)
//	n1nestedroot    L is not a root — the type argument is []L, and the encoding walks
//	                the graph BELOW each root
//
// Five are NEGATIVE witnesses: they hold the residual's shape yet graph
// cleanly, and they exist to bound the disclosed blast radius so it cannot be
// restated too widely (see "The uninstantiated-body sub-case" in the design):
//
//	n1zeroinst      the residual's shape with ZERO instantiations — one sink[L], no pair
//	n1nongencallee  the residual's shape with a NON-GENERIC callee — no sink[L] at all
//	n1typeonly      L IS a type argument of a twice-instantiated generic TYPE, but that
//	                type has no methods, so no ssa.Function is built at L
//	n1fqndiffers    all four of the earlier conjuncts hold, but the enclosing type
//	                parameter also reaches the callee's roots, so the FQNs DIFFER and
//	                panicOnDuplicateSortKey short-circuits before the discriminator
//	n1twodecls      the same with the FQNs EQUAL — two locals that render identically —
//	                separated because they come from two DECLARATIONS
//
// n1onedecl is n1twodecls' minimal pair on the refusing side: the two
// declarations collapsed into one generic instantiated twice.
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

	keyDumpBuildOnce sync.Once
	keyDumpPath      string
	keyDumpBuildErr  error
)

// keyDumpBinary compiles THIS test package to a standalone binary once per run
// and returns its path. The determinism test execs it 20 times per subject in
// discriminator-dump mode (see keyDumpEnv).
//
// It is compiled with `go test -c`, i.e. WITHOUT whatever flags the outer run
// carries, deliberately: `make verify` runs `go test -race ./...`, and a
// race-instrumented child is several times slower per analysis — 2.01s against
// 0.30s, 6.7x, measured at the 2026-07-27 revision — which put this package
// within sight of the 10-minute per-package timeout on a slower machine. The
// child's job is to re-run the loader -> SSA -> discriminator pipeline in a fresh
// process; the race detector adds nothing to that and only buys a timeout risk.
// os.Executable() would have been shorter and is what this must not use. The
// remaining headroom, and why the run count is not the lever, are recorded on
// TestLocalGenericIdentityDeterministicAcrossProcesses.
func keyDumpBinary(t *testing.T) string {
	t.Helper()
	keyDumpBuildOnce.Do(func() {
		dir, err := os.MkdirTemp("", "flowmap-keydump")
		if err != nil {
			keyDumpBuildErr = err
			return
		}
		keyDumpPath = filepath.Join(dir, "keydump")
		_, file, _, _ := runtime.Caller(0)
		cmd := exec.Command("go", "test", "-c", "-o", keyDumpPath, ".")
		cmd.Dir = filepath.Dir(file)
		if out, err := cmd.CombinedOutput(); err != nil {
			keyDumpBuildErr = fmt.Errorf("go test -c ./cmd/flowmap: %v\n%s", err, out)
		}
	})
	if keyDumpBuildErr != nil {
		t.Fatalf("build discriminator dumper: %v", keyDumpBuildErr)
	}
	return keyDumpPath
}

// keyDumpEnv puts this test binary into discriminator-dump mode instead of
// running tests: it analyzes the named service directory and prints the sorted
// (FQN, discriminator) multiset of the surviving call-graph nodes.
//
// It is a deliberate TEST-ONLY observation seam, and it exists because the
// discriminator is deliberately absent from every artifact (see "Non-goals" in
// the design). That absence is what makes key-byte nondeterminism invisible to
// any stdout comparison, so the assertion that polices it needs a channel of its
// own — and a debug channel wired only into the test binary is the smallest one
// that does not put an internal sort key into production output.
const keyDumpEnv = "FLOWMAP_TEST_DISCRIMINATOR_DUMP_DIR"

func TestMain(m *testing.M) {
	if dir := os.Getenv(keyDumpEnv); dir != "" {
		os.Exit(dumpDiscriminatorKeys(dir))
	}
	code := m.Run()
	if witnessBuildDir != "" {
		_ = os.RemoveAll(witnessBuildDir)
	}
	if keyDumpPath != "" {
		_ = os.RemoveAll(filepath.Dir(keyDumpPath))
	}
	os.Exit(code)
}

// dumpDiscriminatorKeys runs the real loader -> SSA -> roots -> call-graph
// pipeline over dir and prints one framed line per surviving node, sorted. The
// discriminator is rendered with %q so its NUL frames and any non-printing byte
// survive the comparison intact; sorting makes the output a MULTISET, so it does
// not depend on the node order the graph happens to carry.
func dumpDiscriminatorKeys(dir string) int {
	res, err := analyze.Analyze(dir, callgraph.Options{Algo: callgraph.AlgoVTA})
	if err != nil {
		fmt.Fprintf(os.Stderr, "discriminator dump: %v\n", err)
		return 2
	}
	lines := make([]string, 0, len(res.Graph.Nodes))
	for _, n := range res.Graph.Nodes {
		lines = append(lines, fmt.Sprintf("%q\t%q", n.FQN, features.InstanceDiscriminator(n.Func)))
	}
	sort.Strings(lines)
	fmt.Println(strings.Join(lines, "\n"))
	return 0
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
//
// The n1zeroinst, n1nongencallee, n1typeonly, n1fqndiffers and n1twodecls rows
// read the other way round: they are shapes the design must NOT refuse, and
// their counts are what proves the clean exit came from an absent collision
// rather than an absent subject. n1twodecls is the sharpest of them — want 2 on
// ONE display FQN is the pair a refusal would need, present in the graph and
// told apart by the discriminator instead, which is why "the two share an FQN"
// cannot be the criterion.
//
// The n1method, n1methodnocall, n1recv and n1thunk rows are the rta/vta half of
// subjects cha refuses: exactly one function is built at the local type here, and
// the residual test asserts the second one and the refusal under cha. Together the
// two tests are what makes the blast radius a mechanism rather than a shape
// list — see "The uninstantiated-body sub-case" in the design.
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

		// The two generic-TYPE-method shapes, under the algorithms that hold ONE
		// analyzed instance of gen's body. Each builds exactly one function at L,
		// which is the armed half: under cha the uninstantiated body supplies a
		// second and both are refused (see the residual test).
		{name: "n1method", algos: []string{"rta", "vta"}, fqn: "(example.com/n1method.Box[example.com/n1method.L]).Show[example.com/n1method.L]", want: 1},
		// Show is never called here. It is built regardless, because fmt.Println
		// converts Box[L] to an interface and pulls in its method set — so the
		// mechanism's threshold is "the analyzed set BUILDS it", not "calls it".
		{name: "n1methodnocall", algos: []string{"rta", "vta"}, fqn: "(example.com/n1methodnocall.Box[example.com/n1methodnocall.L]).Show[example.com/n1methodnocall.L]", want: 1},
		// n1recv's refused function is a promotion wrapper, which go/ssa splices out
		// of the emitted graph (as in n2), so exit 0 is the separation assertion and
		// the promoted method surviving is the not-dropped assertion.
		{name: "n1recv", algos: []string{"rta", "vta"}, fqn: "(example.com/n1recv.emb).QueryContext", want: 1},
		// n1thunk is n1recv with the promotion wrapper replaced by a method
		// EXPRESSION, so the refused function is a $thunk — spliced out of the
		// emitted graph like every forwarder, which is why the promoted method is
		// what is counted here. Under cha the uninstantiated body supplies the
		// second thunk and both are refused (see the residual test).
		{name: "n1thunk", algos: []string{"rta", "vta"}, fqn: "(example.com/n1thunk.emb).QueryContext", want: 1},

		// The two NEGATIVE witnesses. Both carry n1b's shape — one generic
		// function, one function-local type whose structure does not mention X —
		// and both graph cleanly under every algorithm. They pin the two
		// preconditions the refusal actually needs, so the disclosed blast radius
		// cannot be widened back into a claim that "any program containing this
		// shape is refused under cha". Each was falsified by execution once.

		// Zero instantiations. Under rta and vta gen is unreachable from main, so
		// nothing at all is analyzed; under cha the uninstantiated body IS analyzed
		// and does build sink[L] — but it is the only one, and one instance is not a
		// collision. want 1 (not 0) under cha is the armed half: a fixture where the
		// door were merely shut would prove nothing about the threshold.
		{name: "n1zeroinst", algos: []string{"rta", "vta"}, fqn: "example.com/n1zeroinst.sink[example.com/n1zeroinst.L]", want: 0},
		{name: "n1zeroinst_wholeprogram", algos: []string{"cha"}, fqn: "example.com/n1zeroinst.sink[example.com/n1zeroinst.L]", want: 1},

		// A non-generic callee, with the generic instantiated once. Passing L to
		// `func sink(v any)` instantiates nothing at L, so there is exactly one sink
		// node and no pair to order — the refusal needs a GENERIC callee.
		{name: "n1nongencallee", algos: []string{"rta", "vta", "cha"}, fqn: "example.com/n1nongencallee.sink", want: 1},
		// The armed half of that witness: under cha both the uninstantiated body and
		// the one instantiation are in the analyzed set, so the door n1 walks through
		// is open here too and only the non-generic callee explains the clean graph.
		{name: "n1nongencallee_uninstbody", algos: []string{"cha"}, fqn: "example.com/n1nongencallee.gen", want: 1},
		{name: "n1nongencallee_instance", algos: []string{"cha"}, fqn: "example.com/n1nongencallee.gen[int]", want: 1},

		// All four of the conjuncts the third formulation listed hold here — gen
		// declares L, L reaches sink's roots, cha builds sink from the
		// uninstantiated body and from each instantiation, and L's structure does
		// not mention X — yet nothing is refused, because `x` carries X's
		// instantiation into sink's roots and the three sink instances therefore
		// carry three DIFFERENT display FQNs. panicOnDuplicateSortKey compares FQNs
		// first and returns before it ever reads the discriminator. Replacing `x`
		// with `0` makes this program n1b's class again and it is refused.
		{name: "n1fqndiffers", algos: []string{"rta", "vta", "cha"}, fqn: "example.com/n1fqndiffers.sink[example.com/n1fqndiffers.L int]", want: 1},
		{name: "n1fqndiffers_second", algos: []string{"rta", "vta", "cha"}, fqn: "example.com/n1fqndiffers.sink[example.com/n1fqndiffers.L string]", want: 1},
		// The armed half under cha: the uninstantiated body's sink[L X] is a third
		// distinct instance, so the pair the refusal would need genuinely exists in
		// the analyzed set and only the FQN keeps it apart.
		{name: "n1fqndiffers_uninstbody", algos: []string{"cha"}, fqn: "example.com/n1fqndiffers.sink[example.com/n1fqndiffers.L X]", want: 1},

		// n1twodecls closes the gap n1fqndiffers leaves: here the two instances DO
		// share a display FQN — mkA's L and mkB's L are distinct types that render
		// identically — and the program still graphs cleanly, because the second
		// root's two locals come from two DECLARATIONS with different sites. want 2
		// is the whole assertion: two node records under one FQN is exactly the pair
		// a refusal needs, told apart by the discriminator instead. So "the two
		// built functions share a display FQN" is necessary and NOT sufficient.
		{name: "n1twodecls", algos: []string{"rta", "vta", "cha"}, fqn: "example.com/n1twodecls.sink[example.com/n1twodecls.L example.com/n1twodecls.L]", want: 2},
		{name: "n1twodecls_gen", algos: []string{"rta", "vta", "cha"}, fqn: "example.com/n1twodecls.gen[example.com/n1twodecls.L]", want: 2},

		// A generic TYPE instantiated at L twice, with NO methods. L is a type
		// argument of something the analyzed set instantiates twice — the loose
		// reading of the mechanism — and nothing is refused, because that
		// instantiation builds no ssa.Function whose roots reach L. Adding one
		// never-called method to Box turns this fixture into n1methodnocall, which
		// cha refuses; the two differ by exactly that line.
		{name: "n1typeonly", algos: []string{"rta", "vta", "cha"}, fqn: "example.com/n1typeonly.gen[int]", want: 1},
		// The armed half: under cha both the uninstantiated body and the one
		// instantiation are analyzed, so Box[L] genuinely exists twice.
		{name: "n1typeonly_uninstbody", algos: []string{"cha"}, fqn: "example.com/n1typeonly.gen", want: 1},
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

// TestLocalGenericIdentityThunkMergeDoesNotFabricateEdges is the witness for the
// silent $thunk merge, and it asserts EDGES rather than node counts because the
// damage was never visible in the node set: both thunks were spliced out of the
// emitted graph either way, and the merged node left exactly the nodes a correct
// run leaves.
//
// The defect: a $thunk's receiver is its first PARAMETER, so before
// features.receiverType existed its discriminator root set was empty, its
// InstanceDiscriminator was "", and callgraph.mergeKey admitted it. one()'s and
// two()'s thunks share a display FQN (a function-local type renders without its
// lexical scope) and a wrapped Object (the interface method emb.QueryContext), so
// they merged — into ONE node whose out-edges are the UNION of theirs.
//
// The direction of the damage is the positive pole, and the assertion is written
// to keep that honest. A union never DELETES a callee, so no absence proof could
// flip; what it added was
//
//	one -> (B).QueryContext
//	two -> (A).QueryContext
//
// under vta, which is the only algorithm here precise enough to know better. rta
// and cha resolve the embedded interface call to every implementer regardless, so
// their six edges are sound over-approximation and are IDENTICAL before and after
// the fix — asserting all three is what separates "the fix removed fabricated
// edges" from "the fix made the graph smaller".
func TestLocalGenericIdentityThunkMergeDoesNotFabricateEdges(t *testing.T) {
	const (
		a   = "(example.com/thunkmerge.A).QueryContext"
		b   = "(example.com/thunkmerge.B).QueryContext"
		one = "example.com/thunkmerge.one"
		two = "example.com/thunkmerge.two"
		mn  = "example.com/thunkmerge.main"
	)
	tests := []struct {
		algo string
		want []string
	}{
		// vta separates the two locals' interface contents, so each caller reaches
		// exactly its own implementer. This is the row the merge falsified.
		{algo: "vta", want: []string{
			mn + " -> " + one,
			mn + " -> " + two,
			one + " -> " + a,
			two + " -> " + b,
		}},
		// rta and cha over-approximate the invoke to every implementer of emb.
		// Both edges per caller are sound here, and unchanged by the fix.
		{algo: "rta", want: []string{
			mn + " -> " + one,
			mn + " -> " + two,
			one + " -> " + a,
			one + " -> " + b,
			two + " -> " + a,
			two + " -> " + b,
		}},
		{algo: "cha", want: []string{
			mn + " -> " + one,
			mn + " -> " + two,
			one + " -> " + a,
			one + " -> " + b,
			two + " -> " + a,
			two + " -> " + b,
		}},
	}
	for _, test := range tests {
		t.Run(test.algo, func(t *testing.T) {
			stdout, stderr, code := graphWitness(t, "thunkmerge", test.algo)
			if code != 0 {
				t.Fatalf("graph --algo %s ./thunkmerge exited %d:\n%s", test.algo, code, stderr)
			}
			got := edgePairs(t, stdout)
			if len(got) != len(test.want) {
				t.Fatalf("graph --algo %s ./thunkmerge emitted %d edges, want %d:\n got %v\nwant %v",
					test.algo, len(got), len(test.want), got, test.want)
			}
			for i := range got {
				if got[i] != test.want[i] {
					t.Fatalf("graph --algo %s ./thunkmerge edge %d = %q, want %q\n got %v\nwant %v",
						test.algo, i, got[i], test.want[i], got, test.want)
				}
			}
		})
	}
}

// edgePairs returns the graph's edges as sorted "from -> to" strings. Sorting
// makes the comparison a multiset over an already-canonical serialization, so the
// assertion is about which edges exist and not about the emitter's ordering, which
// graphio.sortGraph pins separately.
func edgePairs(t *testing.T, stdout string) []string {
	t.Helper()
	var g graphio.Graph
	if err := json.Unmarshal([]byte(stdout), &g); err != nil {
		t.Fatalf("decode graph: %v", err)
	}
	got := make([]string, 0, len(g.Edges))
	for _, e := range g.Edges {
		got = append(got, e.From+" -> "+e.To)
	}
	sort.Strings(got)
	return got
}

// TestLocalGenericIdentityResidualStaysRefused is the fixture that must NEVER go
// green without a ratified change to "Residual undecided classes". If it does,
// two distinct *ssa.Function were collapsed into one node and every absence proof
// downstream now covers a function the analysis never examined — a silent false
// PROVEN, which is strictly worse than the refusal.
//
// It asserts the TEXT, not merely the exit code, in both directions: the refusal
// must carry the disclosed wording, and it must NOT claim an instantiation count.
// n1, n1lib, n1method, n1methodnocall and n1recv all instantiate their generic
// exactly ONCE and are refused anyway, which is what makes a count unassertable.
//
// The subject list is also what pins the blast-radius MECHANISM, one subject per
// clause of "The uninstantiated-body sub-case":
//
//   - the ENCLOSING BODY (conjunct 1). n1b/n1/n1lib: a generic function.
//     n1closure: a closure nested in one, with no type parameters of its own.
//     n1typemethod: a method of a generic TYPE. No generic function DECLARES its
//     local — the program's sink[T] is the callee — so a conjunct 1 written as
//     "a generic function declares L" contains only the first three.
//   - the ROOT POSITION (conjunct 2). n1method/n1methodnocall: a type argument
//     of a generic type's method. n1recv: a promotion wrapper's RECEIVER, with
//     no type arguments anywhere. n1thunk: a $thunk's receiver, which is not
//     Signature.Recv() either — it is the thunk's first PARAMETER, and reading
//     only Signature.Recv() is what made this subject exit 0 while n1recv, the
//     same shape one wrapper kind away, exited 2. n1nestedroot: []L, so L is not
//     a root at all. A conjunct 2 written as "L IS one of the type arguments"
//     contains none of the last three.
//   - CONJUNCT 5, that nothing else reaching those roots separates the pair:
//     n1onedecl is the refusing half of the minimal pair whose clean half is
//     n1twodecls (see TestLocalGenericIdentityWitnessesGraph).
func TestLocalGenericIdentityResidualStaysRefused(t *testing.T) {
	tests := []struct {
		name  string
		algos []string
		fqn   string
		file  string
		// local is the function-local type's name, which the diagnostic quotes.
		local string
	}{
		{name: "n1b", algos: []string{"rta", "vta", "cha"}, fqn: "example.com/n1b.sink[example.com/n1b.L]", file: "main.go", local: "L"},
		// The whole-program sub-case: gen is instantiated ONCE, but cha also
		// analyzes its uninstantiated body, so two distinct sink[L] come from one
		// declaration with identical structure. Same undecidable class.
		{name: "n1", algos: []string{"cha"}, fqn: "example.com/n1.sink[example.com/n1.L]", file: "main.go", local: "L"},
		// The same sub-case under rta and vta: a library unit roots at its exported
		// surface, so an exported generic's uninstantiated body is analyzed
		// alongside its single instantiation. The uninstantiated-body door is NOT
		// cha-specific, which is why the diagnostic may not claim an instantiation
		// count and may not offer "instantiate it only once" as a remedy.
		{name: "n1lib", algos: []string{"rta", "vta", "cha"}, fqn: "example.com/n1lib.sink[example.com/n1lib.L]", file: "lib.go", local: "L"},
		// A METHOD OF A GENERIC TYPE. L is a type argument of Box, not of any
		// generic function, and the refused pair is Box's method.
		{name: "n1method", algos: []string{"cha"}, fqn: "(example.com/n1method.Box[example.com/n1method.L]).Show[example.com/n1method.L]", file: "main.go", local: "L"},
		// The same with the method never called: cha refuses it too, so the
		// mechanism's threshold is what the analyzed set BUILDS, not what it calls.
		{name: "n1methodnocall", algos: []string{"cha"}, fqn: "(*example.com/n1methodnocall.Box[example.com/n1methodnocall.L]).Show", file: "main.go", local: "L"},
		// The RECEIVER door: a promotion wrapper over a function-local receiver
		// declared inside a generic function. It carries no type arguments at all,
		// so only the receiver root reaches the local declaration.
		{name: "n1recv", algos: []string{"cha"}, fqn: "(*example.com/n1recv.result).QueryContext", file: "main.go", local: "result"},
		// The same door reached through a $thunk instead of a promotion wrapper.
		// A thunk has no Signature receiver AND no type arguments — its receiver is
		// its first PARAMETER — so before features.receiverType existed this
		// program was not refused: mergeKey saw an empty discriminator and merged
		// the pair. One program shape, exit 2 through n1recv and exit 0 through
		// n1thunk, is the severity cliff this row closes.
		{name: "n1thunk", algos: []string{"cha"}, fqn: "(example.com/n1thunk.local).QueryContext$thunk", file: "main.go", local: "local"},
		// L declared in a CLOSURE nested in a generic function. The closure has no
		// type parameters of its own; the analyzer builds one copy of it per
		// instantiation of gen, which is all conjunct 1 requires.
		{name: "n1closure", algos: []string{"rta", "vta", "cha"}, fqn: "example.com/n1closure.sink[example.com/n1closure.L]", file: "main.go", local: "L"},
		// L declared in a METHOD OF A GENERIC TYPE. Show declares no type
		// parameters, and no generic function declares L either — the program's
		// sink[T] is the CALLEE that carries L into its roots. So a conjunct 1
		// written as "a generic function declares L" excludes this program and
		// flowmap refuses it under every algorithm.
		{name: "n1typemethod", algos: []string{"rta", "vta", "cha"}, fqn: "example.com/n1typemethod.sink[example.com/n1typemethod.L]", file: "main.go", local: "L"},
		// L is NOT a discriminator root: sink's one type argument is []L. The
		// encoding walks the graph below each root, so a conjunct 2 written as "L
		// IS one of the type arguments, or the receiver" excludes this program too.
		{name: "n1nestedroot", algos: []string{"rta", "vta", "cha"}, fqn: "example.com/n1nestedroot.sink[[]example.com/n1nestedroot.L]", file: "main.go", local: "L"},
		// The refusing half of the n1twodecls minimal pair: the two declarations
		// behind the second root collapsed into ONE generic instantiated twice. The
		// guard names gen[M], the first duplicate in sorted node order.
		{name: "n1onedecl", algos: []string{"rta", "vta", "cha"}, fqn: "example.com/n1onedecl.gen[example.com/n1onedecl.M]", file: "main.go", local: "M"},
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
					// The position is rendered for a human, not as the key's site
					// bytes: "main.go:991" reads as line 991 in a 26-line file.
					`type "` + test.local + `" at ` + test.file + ", byte offset ",
					" (line ",
					"This is a DISCLOSED LIMIT of the function-local type discriminator, not a crash",
					"HOW THE ANALYSIS GOT TWO OF THEM:",
					"the analyzed set holds its UNINSTANTIATED body",
					"WHAT YOU CAN DO — any one of:",
					"move the type declaration out of the generic function",
					"shrink the analyzed set to ONE instance of the enclosing generic body",
					"UNKNOWN collision class",
					"local-type-graph/v1",
				} {
					if !strings.Contains(stderr, want) {
						t.Errorf("refusal of ./%s under %s is missing %q:\n%s", test.name, algo, want, stderr)
					}
				}
				// The retired wording was written for one sub-case and is FALSE for
				// the other: n1 under cha and n1lib under every algorithm instantiate
				// their generic exactly once, so an instantiation count the analyzer
				// never established must not reappear, and neither may a remedy that
				// is already satisfied.
				for _, forbidden := range []string{
					"instantiate the enclosing generic function only once",
					"this program instantiates more",
				} {
					if strings.Contains(stderr, forbidden) {
						t.Errorf("refusal of ./%s under %s claims %q, which is false for a generic instantiated once:\n%s",
							test.name, algo, forbidden, stderr)
					}
				}
				// The retired rendering of the position: `main.go:991` is what a
				// reader parses as line 991, and it is a byte offset. The key still
				// carries exactly those bytes; the prose must not.
				if joined := `type "` + test.local + `" at ` + test.file + ":"; strings.Contains(stderr, joined) {
					t.Errorf("refusal of ./%s under %s renders the site as %q, which reads as a line number:\n%s",
						test.name, algo, joined, stderr)
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
// Each run is checked twice, because stdout alone is blind to the risk it is
// meant to police:
//
//   - the graph bytes must be identical, which is the observable form of "the
//     panic decision did not vary" — a program that graphs cleanly on one run and
//     refuses on the next would violate determinism outright;
//   - the sorted MULTISET of (FQN, discriminator) keys must be identical. The
//     discriminator reaches no artifact by design, so key bytes that vary per run
//     without ever colliding change no stdout at all. That is exactly the shape
//     the disclosed types.Type pointer-identity dependency would take, and only
//     this assertion sees it.
//
// COST, AND WHY THE 20 AND THE SUBJECT LIST ARE NOT THE KNOB TO TURN. This test
// is the most expensive thing in the package: 4 subjects x 20 runs x 2 child
// processes = 160 out-of-process analyses, of which the 80 key dumps are what the
// multiset assertion added. Measured on an Apple-silicon laptop at the 2026-07-27
// revision (go 1.25), so treat these as a dated snapshot, not an invariant:
//
//	go test -race ./cmd/flowmap -count=1   129s   (this test 38s of it)
//	go test -race ./... -count=1           cmd/flowmap 251s, whole suite 4m12s
//	one child analysis                     0.30s; 2.01s if race-instrumented (6.7x)
//
// That risk was real and it CAME DUE. Against go's 600s per-package default, the
// contended 251s left ~2.4x — and the CI runners are inside that margin: the same
// commit ran cmd/flowmap in 571s on one runner and timed out on its twin, one
// passing and one failing verdict for identical bytes. The ceiling is now an
// explicit RACE_TIMEOUT (Makefile, mirrored into gates.yml and pinned by
// ciparity's TestRaceTimeoutParity), not go's default; read the current value
// there rather than trusting a number quoted here.
//
// Cutting this test was and remains the wrong lever, for two reasons.
//
// It would not move the constraint. The 80 key dumps cost ~24s isolated and ~47s
// contended; deleting them outright leaves cmd/flowmap around 204s, with
// internal/static/graphio at 232s and static/rebind at 176s immediately behind.
// Whatever machine times this package out times those out at nearly the same
// slowdown, so the headroom is a property of the suite, not of this seam. The CI
// numbers bear that out and are the reason the fix was a suite-wide ceiling rather
// than surgery here: on the same runners, graphio ran 579s against the old 600s —
// closer to timing out than this package was, and on main, owing nothing to this
// test.
//
// And it would weaken the assertion. The mutation this test exists to catch —
// appending a per-process value to every discriminator suffix, which collides
// with nothing and changes no artifact — was re-run and fails at run 1 on every
// subject, on the key multiset alone with the graph bytes still identical. So yes,
// 2 runs of 1 subject would still catch that one. But the risk being policed is
// that two independent loader -> SSA builds share go/types nodes to a DIFFERENT
// degree, which is INTERMITTENT: 20 draws detect what 2 would not, and each
// subject covers a distinct encoding shape (f3c repeated struct roles, f26
// offset-aligned cross-package locals, n2 promoted-method wrappers,
// localtypeargsvc the shipped fixture). Either cut buys time by lowering
// detection probability.
//
// So if this package starts timing out AGAIN: raise RACE_TIMEOUT further, or make
// the children cheaper (they are already built WITHOUT -race for exactly this
// reason — see keyDumpBinary). Do not lower the 20 and do not shorten the subject
// list. Splitting this file into its own package was prototyped and rejected: it
// relocates the seam away from the binary it builds (`go build -o … .` then
// silently writes nothing for a test-only package, failing later as a
// permission-denied exec) and buys headroom for the third-slowest package while
// the slowest keeps its margin.
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
			var wantGraph, wantKeys string
			for run := 0; run < 20; run++ {
				stdout, stderr, code := graphServiceDir(t, dir, "vta")
				if code != 0 {
					t.Fatalf("run %d of ./%s exited %d:\n%s", run, witness, code, stderr)
				}
				keys := discriminatorKeys(t, dir)
				if run == 0 {
					wantGraph, wantKeys = stdout, keys
					if !strings.Contains(keys, localTypeGraphMarkerText) {
						t.Fatalf("./%s produced no local-type-graph suffix at all; the subject cannot police the encoding:\n%s",
							witness, keys)
					}
					continue
				}
				if stdout != wantGraph {
					t.Fatalf("run %d of ./%s produced different graph bytes", run, witness)
				}
				if keys != wantKeys {
					t.Fatalf("run %d of ./%s produced a different discriminator key multiset:\n--- got ---\n%s\n--- want ---\n%s",
						run, witness, keys, wantKeys)
				}
			}
		})
	}
}

// localTypeGraphMarkerText is features.LocalTypeGraphMarker as it appears in the
// %q-quoted dump. Asserting it is present keeps each determinism subject ARMED:
// a subject whose keys carry no type-graph suffix would compare identical bytes
// forever and prove nothing about the encoding.
const localTypeGraphMarkerText = `\x00local-type-graph/v1\x00`

// discriminatorKeys runs the dump binary over dir and returns the sorted
// (FQN, discriminator) multiset it printed. A separate process is the whole
// point: the risk being policed is variance between two independent
// loader -> SSA builds, which no in-process repetition can reach.
func discriminatorKeys(t *testing.T, dir string) string {
	t.Helper()
	cmd := exec.Command(keyDumpBinary(t))
	// GOWORK=off because the witnesses are standalone service modules, not
	// workspace members — the same environment graphServiceDir uses.
	cmd.Env = append(os.Environ(), "GOWORK=off", keyDumpEnv+"="+dir)
	var out, errOut strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &errOut
	if err := cmd.Run(); err != nil {
		t.Fatalf("discriminator dump of %s: %v\n%s", dir, err, errOut.String())
	}
	return out.String()
}
