// Package openapi is the opt-in labeler that names outbound calls made through
// spec-generated HTTP clients (oapi-codegen and comparable generators). The
// constant-fold HTTP labeler (graphio's httpLabel / boundary's constHTTP) names an
// outbound call only when its (peer, method, route) arguments are compile-time
// constants; a generated client assembles the route dynamically —
//
//	operationPath := fmt.Sprintf("/v1/publishers/%s/eventTypes/%s/...", p0, p1, ...)
//
// so no argument is constant and the call surfaces unnamed (an internal edge into
// the client, or an ExternalBoundaryCall / NonConstantBoundaryArg blind spot). The
// information the fold cannot recover is fully determined by two author-owned
// artifacts: the OpenAPI document (operationId → method + path template) and a
// per-package declaration of which peer the generated client addresses. This package
// reads both and labels the call site whose callee is a generated operation function
// as boundary:<peer> <METHOD> <path-template>, provenance-tagged via=openapi-client.
//
// # Soundness (CLAUDE.md alignment)
//
//   - Author-asserted, provenance-marked. peer and spec are DECLARATIONS (classify.
//     openapiClients), exactly like entrypoints.callbacks/workers; the Via tag keeps a
//     spec-derived label distinguishable from a discovered constant — never laundered.
//   - Monotonic. The labeler only NAMES call edges the graph already resolved (the
//     callee is a real function in a real package); it adds no reachability.
//   - Deterministic. Output is a pure function of (config, spec file bytes): the
//     operation table is sorted on operationId, templates are verbatim, and a
//     generated-name collision is dropped rather than resolved by arrival order.
//   - Fail-closed / disclose-not-omit. A missing or unparseable spec is a hard error
//     (NewLabeler, naming the config entry), never a silent skip; a declared-package
//     callee that resolves to no operation is disclosed by the caller as an
//     UnresolvedSpecOperation blind spot (graphio), never guessed.
//
// # Spec parsing scope
//
// ParseSpec reads operationId → (method, path template) directly from each inline
// path item's HTTP-method operations, templates verbatim with {param} placeholders
// preserved. It deliberately does NOT resolve $ref (no network resolution; an
// operation reached only through a $ref is not entered into the table — fail closed,
// so its callees surface as blind spots rather than a guessed name). An operation
// with no operationId is skipped (oapi-codegen names a generated function from the
// operationId; a synthesized fallback name is not something this labeler reverses).
package openapi

import (
	"fmt"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Operation is one spec operation reduced to what a boundary label needs: the
// operationId (the key oapi-codegen derives every generated function name from), the
// uppercased HTTP method, and the path template verbatim (with {param} placeholders).
type Operation struct {
	OperationID string
	Method      string
	Template    string
}

// specDoc / pathItem / operationDoc model the slice of an OpenAPI document ParseSpec
// reads: paths → path template → per-method operation → operationId. yaml.v3 is lenient
// by default (no KnownFields), so every other key an OpenAPI doc carries — info,
// components, path-level parameters, servers, summaries — is ignored rather than
// erroring, and a document that is valid YAML but carries no `paths` simply yields
// zero operations (the caller treats that as a misconfiguration).
type specDoc struct {
	Paths map[string]pathItem `yaml:"paths"`
}

type pathItem struct {
	Get     *operationDoc `yaml:"get"`
	Put     *operationDoc `yaml:"put"`
	Post    *operationDoc `yaml:"post"`
	Delete  *operationDoc `yaml:"delete"`
	Options *operationDoc `yaml:"options"`
	Head    *operationDoc `yaml:"head"`
	Patch   *operationDoc `yaml:"patch"`
	Trace   *operationDoc `yaml:"trace"`
}

type operationDoc struct {
	OperationID string `yaml:"operationId"`
}

// operations returns the path item's method→operation map keyed by the UPPERCASE HTTP
// method (the form the boundary:<peer> <METHOD> <route> grammar uses, matching the
// existing constant-fold HTTP labeler). A nil entry is a method the path does not
// declare; ParseSpec skips it.
func (p pathItem) operations() map[string]*operationDoc {
	return map[string]*operationDoc{
		"GET":     p.Get,
		"PUT":     p.Put,
		"POST":    p.Post,
		"DELETE":  p.Delete,
		"OPTIONS": p.Options,
		"HEAD":    p.Head,
		"PATCH":   p.Patch,
		"TRACE":   p.Trace,
	}
}

// ParseSpec parses an OpenAPI document and returns its operations (those carrying an
// operationId), sorted on intrinsic keys (operationId, then method, then template) so
// the table is byte-identical across runs regardless of the document's map order. A
// YAML/JSON parse failure is returned as an error (fail closed, not a skip). An empty
// slice means the document declared no operationId-bearing operation — the caller
// decides whether that is an error (NewLabeler treats it as a misconfiguration).
func ParseSpec(b []byte) ([]Operation, error) {
	var doc specDoc
	if err := yaml.Unmarshal(b, &doc); err != nil {
		return nil, fmt.Errorf("not a parseable OpenAPI document: %w", err)
	}
	var ops []Operation
	for template, item := range doc.Paths {
		for method, op := range item.operations() {
			if op == nil || strings.TrimSpace(op.OperationID) == "" {
				continue
			}
			ops = append(ops, Operation{OperationID: op.OperationID, Method: method, Template: template})
		}
	}
	sort.Slice(ops, func(i, j int) bool {
		a, b := ops[i], ops[j]
		if a.OperationID != b.OperationID {
			return a.OperationID < b.OperationID
		}
		if a.Method != b.Method {
			return a.Method < b.Method
		}
		return a.Template < b.Template
	})
	return ops, nil
}
