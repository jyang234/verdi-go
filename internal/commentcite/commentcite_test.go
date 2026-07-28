// Package commentcite holds a self-checking guard over one class of comment lie:
// a comment that cites a test by name as the pin for an invariant, where no test
// by that name exists. CLAUDE.md makes comments load-bearing ("the oracle a
// reviewer uses to tell a bug from a feature") and tenet 5 requires that anything
// a machine can verify be verified mechanically rather than by review.
//
// This is the gap `make comment-drift` cannot cover. That nudge flags a MOVED body
// under an unchanged asserting comment; a citation of a test that never existed is
// a NEW comment over an unchanged body, which drift detection cannot see. The two
// are complementary, not redundant.
//
// The package imports nothing from the toolchain so it can read raw sources.
package commentcite

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"testing"
)

// citedPackages are the directories whose comments are held to the citation rule,
// relative to the repo root. The guard is deliberately SCOPED rather than
// repo-wide: it is a merge blocker, and a blocker that starts red on unrelated
// pre-existing prose gets disabled instead of obeyed. Widen it by adding a
// directory here once that directory is clean.
// A repo-wide run at the time of writing reported five further HITS — and only
// TWO are stale citations. Read them before widening, because three are
// limitations of the pattern, not defects, and widening onto them turns CORRECT
// comments red:
//
//   - internal/fqnres and internal/static/schemadrift are genuinely stale: each
//     names a test that was later renamed to a longer name it is now a prefix of.
//   - internal/groundwork/fitness has two, both correct as written. One cites a
//     `go test -run` PREFIX GLOB (a real name plus a trailing star); the other
//     HYPHEN-WRAPS a name across two comment lines. Long names plus dense prose
//     make both idioms live in this repo, including in the packages covered here.
//   - internal/fuzzcov names Go's own Fuzz-target naming CONVENTION, not a
//     function.
//
// Those five are deliberately described rather than quoted, because the pattern
// has NO OPT-OUT and quoting them here would make this very comment fail the
// guard — as it did, once, while being written. That is the limitation in one
// sentence: a citation to a testdata fixture, an upstream repo's test, or a name
// under discussion rather than in use misfires identically, and there is no way
// to say so in a comment. Widening means fixing the two stale names AND teaching
// the pattern those shapes (or adding an opt-out marker) — not merely adding a
// directory below. A merge blocker that starts red on prose that is right is one
// somebody deletes instead of obeying.
var citedPackages = []string{
	"internal/commentcite",
	"internal/static/callgraph",
	"internal/static/features",
	"internal/static/graphio",
	"internal/static/loader",
}

// citation matches an identifier in the shape `go test -run` addresses: a Test,
// Fuzz, or Benchmark function name. The trailing [A-Z] is what keeps the pattern
// from firing on prose ("Tests are", "Benchmarking") and on the many identifiers
// that merely start with those words but are not entry points ("TestMain" is a
// real entry point and IS checked; "Testdata" is not matched).
var citation = regexp.MustCompile(`\b(?:Test|Fuzz|Benchmark)[A-Z]\w*\b`)

// skipDirs are never walked: vendored or generated trees, and testdata, whose
// fixture modules are separate programs whose comments this repo does not own.
var skipDirs = map[string]bool{
	".git": true, "testdata": true, "vendor": true, ".worktrees": true, "node_modules": true,
}

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Join(filepath.Dir(file), "..", "..")
}

// walkGoFiles calls fn for every .go file under root, skipping skipDirs.
func walkGoFiles(t *testing.T, root string, fn func(path string)) {
	t.Helper()
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if skipDirs[d.Name()] {
				return fs.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(path, ".go") {
			fn(path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
}

// declaredEntryPoints returns every Test/Fuzz/Benchmark function declared anywhere
// in the repo, keyed by bare name. It is collected REPO-WIDE even though the
// citation check is scoped, because a comment in one package legitimately cites a
// test in another; scoping the collection would manufacture false violations.
func declaredEntryPoints(t *testing.T, root string) map[string]string {
	t.Helper()
	found := map[string]string{}
	fset := token.NewFileSet()
	walkGoFiles(t, root, func(path string) {
		if !strings.HasSuffix(path, "_test.go") {
			return
		}
		f, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		for _, d := range f.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Recv != nil || fd.Name == nil {
				continue
			}
			if citation.MatchString(fd.Name.Name) {
				found[fd.Name.Name] = path
			}
		}
	})
	return found
}

// TestCitedTestNamesExist is the guard: every Test/Fuzz/Benchmark name a comment in
// citedPackages names must be a function that actually exists.
//
// It bites on the two real defects it was written for, both fixed in the same
// change: localtypegraph.go's receiverType pin named a toolchain test that has
// never existed in this repo, and mermaid_focus_test.go's doc comment still named
// the pre-rename identity of the function directly beneath it. In both the pin was
// real and only the citation was wrong — the failure a reader cannot detect and a
// compiler cannot catch. Neither dead name is spelled out here: this guard holds
// its own package too, so quoting one would make the test permanently red.
func TestCitedTestNamesExist(t *testing.T) {
	root := repoRoot(t)
	declared := declaredEntryPoints(t, root)
	if len(declared) == 0 {
		t.Fatal("collected no Test/Fuzz/Benchmark declarations; the walk is broken, not the repo clean")
	}

	fset := token.NewFileSet()
	var violations []string
	for _, pkg := range citedPackages {
		dir := filepath.Join(root, pkg)
		if _, err := os.Stat(dir); err != nil {
			t.Fatalf("cited package %s is not a directory: %v", pkg, err)
		}
		walkGoFiles(t, dir, func(path string) {
			f, err := parser.ParseFile(fset, path, nil, parser.ParseComments|parser.SkipObjectResolution)
			if err != nil {
				t.Fatalf("parse %s: %v", path, err)
			}
			for _, group := range f.Comments {
				for _, c := range group.List {
					for _, name := range citation.FindAllString(c.Text, -1) {
						if _, ok := declared[name]; ok {
							continue
						}
						rel, _ := filepath.Rel(root, path)
						pos := fset.Position(c.Pos())
						violations = append(violations, fmt.Sprintf(
							"%s:%d: cites %s, which no test declares", rel, pos.Line, name))
					}
				}
			}
		})
	}
	sort.Strings(violations)
	for _, v := range violations {
		t.Errorf("comment cites a nonexistent test: %s", v)
	}
}
