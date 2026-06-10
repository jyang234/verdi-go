// Command groundwork is the deterministic verification layer over flowmap's call
// graph. It consumes the graph JSON flowmap emits (never generating it itself —
// the graph must come from trusted CI) and computes architectural fitness,
// blast-radius, pre-merge gates and review artifacts. No AI sits in any verdict;
// every output is a pure function of (graph, policy, delta).
//
// Phase 0 ships the substrate and two introspection surfaces — `reach` (graph
// reachability) and `policy-check` (validate a policy) — that the later
// verdict-bearing surfaces (fitness, verify, review) build on.
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/jyang234/golang-code-graph/internal/groundwork/fitness"
	"github.com/jyang234/golang-code-graph/internal/groundwork/graph"
	"github.com/jyang234/golang-code-graph/internal/groundwork/policy"
)

var version = "dev"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "groundwork:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		usage()
		return nil
	}
	switch args[0] {
	case "version":
		fmt.Println("groundwork", version)
		return nil
	case "reach":
		return cmdReach(args[1:])
	case "fitness":
		return cmdFitness(args[1:])
	case "policy-check":
		return cmdPolicyCheck(args[1:])
	case "help", "-h", "--help":
		usage()
		return nil
	default:
		return fmt.Errorf("unknown subcommand %q (try `groundwork help`)", args[0])
	}
}

func usage() {
	fmt.Print(`groundwork — deterministic verification over flowmap's call graph

usage:
  groundwork reach <graph.json> <fqn>          reachability + entrypoint cover + effects for a function
  groundwork fitness <policy.json> <graph.json> evaluate the policy's invariants (non-zero exit on violation)
  groundwork policy-check <policy.json>        load and validate a policy
  groundwork version

The graph must be produced by trusted CI (flowmap graph <service>); groundwork
only ever reads it.
`)
}

// cmdReach reports the bidirectional reachability of one function: who breaks if
// it changes (callers), what it depends on (callees), which entry points it is
// live behind, the external effects it reaches, and any blind spots on it.
func cmdReach(args []string) error {
	fs := flag.NewFlagSet("reach", flag.ContinueOnError)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 2 {
		return fmt.Errorf("usage: groundwork reach <graph.json> <fqn>")
	}
	g, err := graph.LoadFile(fs.Arg(0))
	if err != nil {
		return err
	}
	ix := graph.NewIndex(g)
	fqn := fs.Arg(1)
	if !ix.Has(fqn) {
		return fmt.Errorf("no function %q in graph (it has %d nodes)", fqn, len(g.Nodes))
	}

	callers := ix.Reaching(fqn)
	callees := ix.Reachable(fqn)
	cover := ix.EntrypointCover(fqn)
	effects := ix.Effects(append([]string{fqn}, callees...)...)

	fmt.Printf("%s\n\n", fqn)
	fmt.Printf("transitive callers (blast radius): %d\n", len(callers))
	for _, c := range callers {
		fmt.Printf("  ← %s\n", c)
	}
	fmt.Printf("transitive callees (dependencies): %d\n", len(callees))
	for _, c := range callees {
		fmt.Printf("  → %s\n", c)
	}
	fmt.Printf("live behind %d entrypoint(s):\n", len(cover))
	for _, e := range cover {
		fmt.Printf("  ⮕ %s\n", e)
	}
	fmt.Printf("reachable external effects: %d\n", len(effects))
	for _, e := range effects {
		marker := ""
		if e.IsDynamic() {
			marker = "  ⚠ unresolved (soundness frontier)"
		}
		fmt.Printf("  %s [%s]%s\n", strings.TrimPrefix(e.To, "boundary:"), e.Boundary, marker)
	}
	if bs := ix.BlindSpotsAt(fqn); len(bs) > 0 {
		fmt.Printf("blind spots on this function: %d\n", len(bs))
		for _, b := range bs {
			fmt.Printf("  ⚠ %s — %s\n", b.Kind, b.Detail)
		}
	}
	return nil
}

// cmdFitness evaluates a policy's invariants against a graph. It prints every
// finding — violations (which fail the gate) and cautions (the graph abstaining
// where it cannot prove a negative) — and returns an error so CI exits non-zero
// when any invariant is broken.
func cmdFitness(args []string) error {
	fs := flag.NewFlagSet("fitness", flag.ContinueOnError)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 2 {
		return fmt.Errorf("usage: groundwork fitness <policy.json> <graph.json>")
	}
	p, err := policy.Load(fs.Arg(0))
	if err != nil {
		return err
	}
	g, err := graph.LoadFile(fs.Arg(1))
	if err != nil {
		return err
	}
	res := fitness.Check(p, graph.NewIndex(g))

	violations, cautions := res.Violations(), res.Cautions()
	for _, f := range violations {
		fmt.Printf("⛔ [%s] %s\n", f.Rule, f.Summary)
		if f.From != "" {
			fmt.Printf("     %s\n", edgeLine(f))
		}
	}
	for _, f := range cautions {
		fmt.Printf("⚠️  [%s] %s\n", f.Rule, f.Summary)
		if f.From != "" {
			fmt.Printf("     %s\n", edgeLine(f))
		}
	}
	if !res.OK() {
		return fmt.Errorf("%d invariant violation(s)", len(violations))
	}
	fmt.Printf("fitness OK — %d invariant(s) hold, %d caution(s)\n", ruleCount(p), len(cautions))
	return nil
}

// edgeLine renders a finding's exact edge or symbol.
func edgeLine(f fitness.Finding) string {
	if f.To != "" {
		return f.From + " → " + f.To
	}
	return f.From
}

// ruleCount is a rough tally of configured invariants, for the OK summary.
func ruleCount(p *policy.Policy) int {
	n := len(p.MustNotReach)
	if p.Layering != nil {
		n++
	}
	if p.IOBudget != nil {
		n++
	}
	return n
}

// cmdPolicyCheck loads and validates a policy, printing a one-line-per-rule
// summary. It is the lint surface for the CODEOWNERS-gated policy file.
func cmdPolicyCheck(args []string) error {
	fs := flag.NewFlagSet("policy-check", flag.ContinueOnError)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: groundwork policy-check <policy.json>")
	}
	p, err := policy.Load(fs.Arg(0))
	if err != nil {
		return err
	}
	fmt.Printf("policy for %q (v%d) — valid\n", p.Service, p.Version)
	if len(p.Layers) > 0 {
		fmt.Printf("  layers (top→bottom): %s\n", strings.Join(p.LayerNames(), " → "))
	}
	if p.Layering != nil {
		fmt.Printf("  layering: %d allow-listed exception(s), %d root package(s)\n", len(p.Layering.Allow), len(p.Layering.Roots))
	}
	if n := len(p.MustNotReach); n > 0 {
		fmt.Printf("  must_not_reach: %d rule(s)\n", n)
	}
	if p.IOBudget != nil {
		fmt.Printf("  io_budget: max %d write(s) per route\n", p.IOBudget.MaxWritesPerRoute)
	}
	return nil
}
