package callgraph

// Regression for the P1 node-sort panic on duplicate synthetic bound-method
// wrappers. go/ssa's createBound/createThunk (x/tools go/ssa) mint a FRESH
// $bound/$thunk wrapper at EVERY method-value / method-expression occurrence and
// never cache them, so K uses of one method M yield K distinct *ssa.Function that
// are byte-identical: same wrapped Object, same Synthetic kind, same RelString,
// same pos (obj.Pos()). They share BOTH the display FQN and the
// InstanceDiscriminator (both empty for a non-generic wrapper), which is exactly
// the tie finalize() cannot order — and pos is identical too, so no positional
// tie-break could separate them. The interchangeable wrappers must be MERGED to one
// node, not fail the deterministic-order guard.

import (
	"fmt"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"strings"
	"testing"

	xcg "golang.org/x/tools/go/callgraph"
	"golang.org/x/tools/go/callgraph/rta"
	"golang.org/x/tools/go/ssa"
	"golang.org/x/tools/go/ssa/ssautil"
)

// dupBoundSrc reproduces the event-bus helper pattern from the flowmap field
// report: two functions each pass the SAME interface method value (r.Exists) into a
// shared helper, so the SSA program holds two distinct bound-method wrappers whose
// RelString(nil) is identical.
const dupBoundSrc = `package fix

type Reader interface{ Exists(id int) bool }

func requireExists(check func(int) bool, id int) bool { return check(id) }

func requirePublisher(r Reader, id int) bool  { return requireExists(r.Exists, id) }
func requireSubscriber(r Reader, id int) bool { return requireExists(r.Exists, id) }

type impl struct{}

func (impl) Exists(id int) bool { return id > 0 }

func main() {
	var r Reader = impl{}
	requirePublisher(r, 1)
	requireSubscriber(r, 2)
}
`

func buildBoundGraph(t *testing.T, src string) *xcg.Graph {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "fix.go", src, 0)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	pkg := types.NewPackage("example.com/fix", "")
	spkg, _, err := ssautil.BuildPackage(
		&types.Config{Importer: importer.Default()}, fset, pkg, []*ast.File{f},
		ssa.SanityCheckFunctions|ssa.InstantiateGenerics)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	spkg.Prog.Build()
	var roots []*ssa.Function
	if m := spkg.Func("main"); m != nil {
		roots = append(roots, m)
	}
	if in := spkg.Func("init"); in != nil {
		roots = append(roots, in)
	}
	return rta.Analyze(roots, true).CallGraph
}

// dupBoundCount reports how many distinct SSA functions the raw RTA graph holds for
// the bound-wrapper FQN — the precondition the regression depends on. Below 2 the
// test would silently stop exercising the collision.
func dupBoundCount(x *xcg.Graph) (fqn string, n int) {
	const want = "(example.com/fix.Reader).Exists$bound"
	for fn := range x.Nodes {
		if fn != nil && fn.RelString(nil) == want {
			n++
		}
	}
	return want, n
}

// TestDuplicateBoundWrappersDoNotPanic is the reproduction: fromX must NOT panic on
// the duplicate bound wrappers, and must collapse them to a single node.
func TestDuplicateBoundWrappersDoNotPanic(t *testing.T) {
	x := buildBoundGraph(t, dupBoundSrc)
	fqn, dup := dupBoundCount(x)
	if dup < 2 {
		t.Fatalf("precondition not met: raw RTA graph holds %d wrappers for %q, want >= 2 "+
			"(go/ssa may have started caching bound wrappers; the collision no longer reproduces)", dup, fqn)
	}

	g := fromX(x, AlgoRTA, nil) // must not panic

	var merged *Node
	var count int
	for _, n := range g.Nodes {
		if n.FQN == fqn {
			count++
			merged = n
		}
	}
	if count != 1 {
		t.Fatalf("bound wrapper %q rendered as %d nodes, want exactly 1 (interchangeable wrappers must merge)", fqn, count)
	}
	// The merged node must be exactly the proven-uncached receiver-less forwarder class
	// (bound/thunk) — the only class mergeKey may collapse (see mergeKey's soundness
	// argument). A wrapper WITH a receiver would be a cached, non-duplicating kind.
	if merged.Func.Signature.Recv() != nil {
		t.Errorf("merged node %q has a receiver; only receiver-less bound/thunk forwarders may merge", fqn)
	}
	if merged.Func.Synthetic == "" {
		t.Errorf("merged node %q is not synthetic; mergeKey must only collapse synthetic wrappers", fqn)
	}
}

// TestDuplicateBoundWrappersDeterministic pins that the merged graph is
// byte-identical across repeated builds of the same program — the property the
// finalize guard exists to protect, now upheld by merging rather than panicking. It
// serializes NODES *and* EDGES (each node's outgoing caller→callee FQNs with the
// call-site position): the merged wrapper's edges are contributed by every use-site
// copy in map-iteration order, so a build-to-build difference here would catch any
// nondeterminism the merge leaks into the edge set, not just node order.
func TestDuplicateBoundWrappersDeterministic(t *testing.T) {
	serialize := func(g *Graph) string {
		var b strings.Builder
		b.WriteString("# nodes\n")
		for _, n := range g.Nodes {
			b.WriteString(n.FQN)
			b.WriteByte('\n')
		}
		b.WriteString("# edges\n")
		for _, n := range g.Nodes {
			for _, e := range n.Out {
				fmt.Fprintf(&b, "%s -> %s @%d\n", e.Caller.FQN, e.Callee.FQN, sitePos(e.Site))
			}
		}
		return b.String()
	}
	var first string
	for i := 0; i < 8; i++ {
		got := serialize(fromX(buildBoundGraph(t, dupBoundSrc), AlgoRTA, nil))
		if i == 0 {
			first = got
			continue
		}
		if got != first {
			t.Fatalf("graph varied across builds:\n--- first ---\n%s\n--- build %d ---\n%s", first, i, got)
		}
	}
}
