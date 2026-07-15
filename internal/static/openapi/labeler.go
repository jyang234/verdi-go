package openapi

import (
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/tools/go/ssa"

	"github.com/jyang234/golang-code-graph/internal/config"
	"github.com/jyang234/golang-code-graph/internal/static/features"
)

// Via tags a boundary edge whose label the OpenAPI-client labeler derived from a
// declared spec (--reclaim-openapi). It is the single source of truth for the
// provenance token, kept distinct from the reclaim/fold vias so a reviewer can tell a
// spec-asserted label from a discovered constant or a reclaimer-recovered one: the
// spec is an AUTHOR DECLARATION, not a fact the analysis proved from code.
const Via = "openapi-client"

// Labeler maps a generated-client call site to its spec-derived boundary label. It is
// built once from the config's classify.openapiClients plus each declared spec file
// (NewLabeler) and then queried per call site (Label / InDeclaredPackage). A nil
// *Labeler answers every query negatively, so callers need not special-case the
// no-clients-configured build.
type Labeler struct {
	// byPkg keys on the generated-client import path (the config `package`). Its value
	// carries that client's peer and the generated-name → label lookup.
	byPkg map[string]*clientTable
}

// clientTable is one declared generated-client package: the peer its calls address,
// and the map from each oapi-codegen generated FUNCTION NAME to the full boundary
// label ("<peer> <METHOD> <template>") it names. An ambiguous name (two operations'
// generated names collide, or a duplicate operationId) is absent from byName — the
// fail-closed choice, so the callee surfaces as an UnresolvedSpecOperation blind spot
// rather than being labeled with a guessed operation.
type clientTable struct {
	peer   string
	byName map[string]string
}

// NewLabeler builds the labeler from the declared clients, reading each spec file
// RELATIVE to dir (the .flowmap.yaml directory). It returns (nil, nil) when no clients
// are declared, so an opt-in build with an empty config is a byte-identical no-op. A
// missing or unparseable spec, a spec that declares no operation, or a package
// declared twice is a HARD ERROR naming the config entry (no partial labeler): the run
// fails before any output, never a silent skip (CLAUDE.md tenet 2, and the FR's
// missing/unparseable acceptance criterion).
func NewLabeler(clients []config.OpenAPIClientHint, dir string) (*Labeler, error) {
	if len(clients) == 0 {
		return nil, nil
	}
	l := &Labeler{byPkg: make(map[string]*clientTable, len(clients))}
	for i, h := range clients {
		if _, dup := l.byPkg[h.Package]; dup {
			return nil, fmt.Errorf("flowmap config: classify.openapiClients[%d]: package %q is declared more than once (ambiguous peer/spec)", i, h.Package)
		}
		specPath := filepath.Join(dir, filepath.FromSlash(h.Spec))
		b, err := os.ReadFile(specPath)
		if err != nil {
			return nil, fmt.Errorf("flowmap config: classify.openapiClients[%d] (%s): reading spec %q: %w", i, h.Package, h.Spec, err)
		}
		ops, err := ParseSpec(b)
		if err != nil {
			return nil, fmt.Errorf("flowmap config: classify.openapiClients[%d] (%s): spec %q: %w", i, h.Package, h.Spec, err)
		}
		if len(ops) == 0 {
			return nil, fmt.Errorf("flowmap config: classify.openapiClients[%d] (%s): spec %q declares no operation with an operationId (nothing to label — wrong file?)", i, h.Package, h.Spec)
		}
		l.byPkg[h.Package] = newClientTable(h.Peer, ops)
	}
	return l, nil
}

// newClientTable builds the generated-name → label lookup for one client. For every
// operation it stamps the six oapi-codegen generated names to the operation's label;
// a name two distinct operations both generate (or a duplicate operationId) is dropped
// and permanently excluded, so an ambiguous callee is never labeled with a guess.
func newClientTable(peer string, ops []Operation) *clientTable {
	byName := make(map[string]string)
	ambiguous := make(map[string]bool)
	for _, op := range ops {
		label := peer + " " + op.Method + " " + op.Template
		for _, name := range generatedNames(op.OperationID) {
			if ambiguous[name] {
				continue
			}
			if prev, ok := byName[name]; ok && prev != label {
				delete(byName, name)
				ambiguous[name] = true
				continue
			}
			byName[name] = label
		}
	}
	return &clientTable{peer: peer, byName: byName}
}

// generatedNames returns the oapi-codegen v2.x generated function names for an
// operationId — the shapes the FR enumerates: the bare client method, its
// WithResponse / WithBody / WithBodyWithResponse variants, and the package-level
// New<Op>Request[WithBody] request builders. Both a method call (<Op>WithResponse on
// *ClientWithResponses) and a package-function call (New<Op>Request) match on name
// alone, since fn.Name() is the bare symbol for either.
func generatedNames(operationID string) []string {
	return []string{
		operationID,
		operationID + "WithResponse",
		operationID + "WithBody",
		operationID + "WithBodyWithResponse",
		"New" + operationID + "Request",
		"New" + operationID + "RequestWithBody",
	}
}

// Label returns the boundary label ("<peer> <METHOD> <template>", no "boundary:"
// prefix — the caller adds it, exactly as httpLabel is consumed) for a call whose
// callee is a generated operation function in a declared client package, and whether
// it matched. It keys on features.EffectivePkgPath (the sound package attribution that
// resolves generic-instance / method-value synthetics) plus the callee's bare name.
// A nil labeler or callee, an out-of-scope package, or an unmatched/ambiguous name all
// return ok=false.
func (l *Labeler) Label(callee *ssa.Function) (string, bool) {
	if l == nil || callee == nil {
		return "", false
	}
	c := l.byPkg[features.EffectivePkgPath(callee)]
	if c == nil {
		return "", false
	}
	label, ok := c.byName[callee.Name()]
	return label, ok
}

// InDeclaredPackage reports whether fn is defined in one of the declared
// generated-client packages. The caller uses it two ways: to skip labeling a call
// whose CALLER is itself inside a client package (the client's own internal plumbing —
// New<Op>Request calling fmt.Sprintf/http.NewRequest — is not the service's outbound
// edge), and to decide whether a non-operation callee in a declared package warrants
// an UnresolvedSpecOperation disclosure.
func (l *Labeler) InDeclaredPackage(fn *ssa.Function) bool {
	if l == nil || fn == nil {
		return false
	}
	_, ok := l.byPkg[features.EffectivePkgPath(fn)]
	return ok
}
