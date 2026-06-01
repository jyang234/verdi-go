// Command flowmap is the CLI for the flowmap verification system. Phase 3 adds
// the static subcommands: `boundary` (generate or --check the gated boundary
// contract) and `graph` (print the non-gated call-graph view). `diff` and
// `coverage` arrive in later phases.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/jyang234/golang-code-graph/internal/static/analyze"
	"github.com/jyang234/golang-code-graph/internal/static/boundary"
	"github.com/jyang234/golang-code-graph/internal/static/graphio"
)

// version is overridden at build time via -ldflags "-X main.version=...".
var version = "dev"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "flowmap:", err)
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
		fmt.Println("flowmap", version)
		return nil
	case "boundary":
		return cmdBoundary(args[1:])
	case "graph":
		return cmdGraph(args[1:])
	case "help", "-h", "--help":
		usage()
		return nil
	default:
		return fmt.Errorf("unknown subcommand %q (try `flowmap help`)", args[0])
	}
}

// cmdBoundary generates the gated boundary contract for a service directory. With
// --check it instead verifies the committed contract is current, exiting non-zero
// if it is stale — the currency gate.
func cmdBoundary(args []string) error {
	fs := flag.NewFlagSet("boundary", flag.ContinueOnError)
	check := fs.Bool("check", false, "verify the committed contract is current; non-zero exit if stale")
	if err := fs.Parse(args); err != nil {
		return err
	}
	dir := dirArg(fs)

	c, err := boundary.Generate(dir)
	if err != nil {
		return err
	}
	path := boundary.ContractPath(dir)

	if *check {
		match, err := boundary.Check(dir, c)
		if err != nil {
			return err
		}
		if !match {
			return fmt.Errorf("boundary contract is stale: regenerate with `flowmap boundary %s` and commit %s", dir, path)
		}
		fmt.Println("boundary contract current:", path)
		return nil
	}

	if err := boundary.Write(dir, c); err != nil {
		return err
	}
	fmt.Println("wrote", path)
	return nil
}

// cmdGraph prints the non-gated call-graph view, optionally scoped to one entry
// point with --entry.
func cmdGraph(args []string) error {
	fs := flag.NewFlagSet("graph", flag.ContinueOnError)
	entry := fs.String("entry", "", `scope to the subgraph reachable from this entry point (e.g. "POST /loan-application")`)
	if err := fs.Parse(args); err != nil {
		return err
	}
	dir := dirArg(fs)

	res, err := analyze.Analyze(dir)
	if err != nil {
		return err
	}
	g, err := graphio.Build(res, *entry)
	if err != nil {
		return err
	}
	b, err := g.Marshal()
	if err != nil {
		return err
	}
	_, err = os.Stdout.Write(b)
	return err
}

// dirArg returns the first positional argument, defaulting to the current
// directory.
func dirArg(fs *flag.FlagSet) string {
	if d := fs.Arg(0); d != "" {
		return d
	}
	return "."
}

func usage() {
	fmt.Println(`flowmap — Go microservice boundary & behavior verification

usage: flowmap <command> [flags] [dir]

commands:
  boundary [--check] [dir]   generate the gated boundary contract (--check: verify currency)
  graph [--entry R] [dir]    print the non-gated call-graph view
  version                    print the flowmap version
  help                       show this message

(diff and coverage arrive in later phases)`)
}
