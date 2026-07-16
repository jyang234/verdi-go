package openapi

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"

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

// ViaWrapper tags a boundary edge whose spec label was derived by DESCENDING a
// hand-written wrapper in a declared client package (followWrappers) — the service
// calls a wrapper method, the wrapper calls the generated operation, and the descent
// walked from the former to the latter. It is kept DISTINCT from Via so a label
// recovered by wrapper descent is never conflated with a direct-call label: descent is a
// weaker, over-approximated provenance (it followed the call graph, not a direct name
// match), so a reviewer must be able to audit the two channels separately.
const ViaWrapper = "openapi-client-wrapper"

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
	// followWrappers mirrors the hint's opt-in: when set, a call into this package that
	// matches no generated-name shape may be RESOLVED by descending its hand-written
	// wrappers (graphio.descendWrapper) rather than only disclosed. Off by default, so
	// the feature is inert unless the author declared it.
	followWrappers bool
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
		// Trim the declared fields: config.validate rejects an all-whitespace field, but
		// a quoted, space-PADDED value ("pkg ") passes it, and the padded package would
		// then never equal features.EffectivePkgPath (no spaces) — a silent no-op. Match
		// and key on the trimmed values so a padded declaration works rather than fails
		// closed-but-silent (the peer likewise, so a padded peer never doubles a label's
		// spaces; the spec, so a padded path still opens).
		pkg := strings.TrimSpace(h.Package)
		peer := strings.TrimSpace(h.Peer)
		spec := strings.TrimSpace(h.Spec)
		if _, dup := l.byPkg[pkg]; dup {
			return nil, fmt.Errorf("flowmap config: classify.openapiClients[%d]: package %q is declared more than once (ambiguous peer/spec)", i, pkg)
		}
		specPath := filepath.Join(dir, filepath.FromSlash(spec))
		b, err := os.ReadFile(specPath)
		if err != nil {
			return nil, fmt.Errorf("flowmap config: classify.openapiClients[%d] (%s): reading spec %q: %w", i, pkg, spec, err)
		}
		ops, err := ParseSpec(b)
		if err != nil {
			return nil, fmt.Errorf("flowmap config: classify.openapiClients[%d] (%s): spec %q: %w", i, pkg, spec, err)
		}
		if len(ops) == 0 {
			return nil, fmt.Errorf("flowmap config: classify.openapiClients[%d] (%s): spec %q declares no operation with an operationId (nothing to label — wrong file?)", i, pkg, spec)
		}
		ct := newClientTable(peer, ops)
		// Thread the opt-in through: the generated-name lookup is independent of it, so
		// newClientTable's signature (and its unit tests) stay unchanged and the flag is
		// set on the built table. h.FollowWrappers is a bool — never invalid.
		ct.followWrappers = h.FollowWrappers
		l.byPkg[pkg] = ct
	}
	return l, nil
}

// newClientTable builds the generated-name → label lookup for one client. For every
// operation it stamps the six oapi-codegen generated names to the operation's label;
// a name two distinct operations both generate (or a duplicate operationId) is dropped
// and permanently excluded, so an ambiguous callee is never labeled with a guess. An
// operationId that normalizes to an empty Go name (all separators) contributes nothing.
func newClientTable(peer string, ops []Operation) *clientTable {
	byName := make(map[string]string)
	ambiguous := make(map[string]bool)
	for _, op := range ops {
		base := goName(op.OperationID)
		if base == "" {
			continue
		}
		label := peer + " " + op.Method + " " + op.Template
		for _, name := range generatedNames(base) {
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

// goName converts an operationId to the Go identifier oapi-codegen derives its
// generated function names from — its ToCamelCase: capitalize the first letter and the
// letter after each `_ - . ` separator, drop the separators, and preserve existing
// case and digits. So `createEvent`, `create_event`, `create-event`, and `CreateEvent`
// all yield `CreateEvent`, matching the actual generated `CreateEventWithResponse` /
// `NewCreateEventRequest`. Matching the RAW operationId (as before) only worked for
// operationIds already in PascalCase and silently missed every camelCase/snake/kebab id
// (the common case) — a real gap, though fail-closed (a miss surfaces as
// UnresolvedSpecOperation, never a wrong label). An exact byte-for-byte match with a
// specific oapi-codegen version is not required for the same reason: a residual miss is
// disclosed, never mislabeled. An unmapped rune (punctuation, a symbol) is dropped, as
// oapi-codegen's ToCamelCase drops it.
func goName(operationID string) string {
	var b strings.Builder
	capNext := true
	for _, r := range strings.TrimSpace(operationID) {
		switch {
		case unicode.IsUpper(r), unicode.IsDigit(r):
			b.WriteRune(r)
			capNext = false
		case unicode.IsLower(r):
			if capNext {
				b.WriteRune(unicode.ToUpper(r))
			} else {
				b.WriteRune(r)
			}
			capNext = false
		case r == '_' || r == '-' || r == '.' || r == ' ':
			capNext = true
		default:
			capNext = false // a rune ToCamelCase maps to nothing: drop it
		}
	}
	return b.String()
}

// generatedNames returns the oapi-codegen v2.x generated function names for a
// normalized (goName) operation base — the shapes the FR enumerates: the bare client
// method, its WithResponse / WithBody / WithBodyWithResponse variants, and the
// package-level New<Op>Request[WithBody] request builders. Both a method call
// (<Op>WithResponse on *ClientWithResponses) and a package-function call
// (New<Op>Request) match on name alone, since fn.Name() is the bare symbol for either.
func generatedNames(base string) []string {
	return []string{
		base,
		base + "WithResponse",
		base + "WithBody",
		base + "WithBodyWithResponse",
		"New" + base + "Request",
		"New" + base + "RequestWithBody",
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

// FollowWrappers reports whether fn's declared client package opted into wrapper
// descent (classify.openapiClients[i].followWrappers). The caller uses it as the gate
// on the descent path: a callee in a declared package that matches no operation is
// DESCENDED (its wrappers followed to a generated operation) only when this is true,
// and otherwise stays a plain UnresolvedSpecOperation disclosure exactly as before.
// Like Label and InDeclaredPackage it is body-independent — it keys on
// features.EffectivePkgPath(fn) — and a nil labeler, nil fn, or out-of-scope package
// all answer false, so the no-opt-in build is byte-identical.
func (l *Labeler) FollowWrappers(fn *ssa.Function) bool {
	if l == nil || fn == nil {
		return false
	}
	c := l.byPkg[features.EffectivePkgPath(fn)]
	return c != nil && c.followWrappers
}
