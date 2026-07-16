package openapi

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/jyang234/golang-code-graph/internal/config"
)

const eventBusSpec = `
openapi: 3.0.3
info:
  title: event-bus
  version: "1.0"
paths:
  /v1/publishers/{publisherId}/eventTypes/{eventType}/versions/{version}/events/{eventId}:
    parameters:
      - name: publisherId
        in: path
        required: true
    post:
      operationId: CreateEvent
      summary: publish an event
  /v1/events/{eventId}:
    get:
      operationId: GetEvent
  /v1/events:
    get:
      operationId: ListEvents
    # an operation with no operationId is skipped (oapi-codegen would synthesize a
    # name this labeler does not reverse)
    post: {}
`

func TestParseSpec(t *testing.T) {
	ops, err := ParseSpec([]byte(eventBusSpec))
	if err != nil {
		t.Fatal(err)
	}
	want := []Operation{
		{OperationID: "CreateEvent", Method: "POST", Template: "/v1/publishers/{publisherId}/eventTypes/{eventType}/versions/{version}/events/{eventId}"},
		{OperationID: "GetEvent", Method: "GET", Template: "/v1/events/{eventId}"},
		{OperationID: "ListEvents", Method: "GET", Template: "/v1/events"},
	}
	if !reflect.DeepEqual(ops, want) {
		t.Errorf("ParseSpec =\n  %+v\nwant\n  %+v", ops, want)
	}
}

// ParseSpec output must be a pure function of the bytes: a determinism check that the
// map-ordered walk is fully re-sorted onto intrinsic keys.
func TestParseSpecDeterministic(t *testing.T) {
	first, err := ParseSpec([]byte(eventBusSpec))
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 20; i++ {
		got, err := ParseSpec([]byte(eventBusSpec))
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, first) {
			t.Fatalf("ParseSpec run %d differs: %+v vs %+v", i, got, first)
		}
	}
}

func TestParseSpecRejectsMalformed(t *testing.T) {
	if _, err := ParseSpec([]byte("\tthis: is: not: valid: yaml")); err == nil {
		t.Error("expected a parse error on malformed YAML")
	}
}

// A valid-YAML document with no paths parses without error but yields no operations —
// NewLabeler is what turns that into a misconfiguration error, so ParseSpec stays a
// pure reader.
func TestParseSpecNoPathsIsEmpty(t *testing.T) {
	ops, err := ParseSpec([]byte("openapi: 3.0.0\ninfo:\n  title: x\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(ops) != 0 {
		t.Errorf("want no operations, got %+v", ops)
	}
}

// newClientTable stamps the six oapi-codegen generated names to each operation's
// label. This pins the exact name grammar the FR enumerates.
func TestGeneratedNameLookup(t *testing.T) {
	c := newClientTable("event-bus", []Operation{
		{OperationID: "CreateEvent", Method: "POST", Template: "/v1/events/{id}"},
	})
	const label = "event-bus POST /v1/events/{id}"
	for _, name := range []string{
		"CreateEvent",
		"CreateEventWithResponse",
		"CreateEventWithBody",
		"CreateEventWithBodyWithResponse",
		"NewCreateEventRequest",
		"NewCreateEventRequestWithBody",
	} {
		if got := c.byName[name]; got != label {
			t.Errorf("byName[%q] = %q, want %q", name, got, label)
		}
	}
	// A name that is not a generated shape (a constructor, a helper) is absent, so the
	// callee surfaces as a blind spot rather than a label.
	for _, name := range []string{"NewClient", "NewClientWithResponses", "CreateEventRequest", "GetEvent"} {
		if got, ok := c.byName[name]; ok {
			t.Errorf("byName[%q] = %q, want absent", name, got)
		}
	}
}

// goName mirrors oapi-codegen's ToCamelCase: the labeler must match the PascalCase
// names oapi-codegen actually generates, not the raw operationId. Pin the casings.
func TestGoName(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"CreateEvent", "CreateEvent"},  // already PascalCase — unchanged
		{"createEvent", "CreateEvent"},  // camelCase (the common real case)
		{"create_event", "CreateEvent"}, // snake_case
		{"create-event", "CreateEvent"}, // kebab-case
		{"get.event.status", "GetEventStatus"},
		{"listV2Events", "ListV2Events"}, // digits and existing caps preserved
		{"  spaced name ", "SpacedName"}, // trimmed + space separator
		{"___", ""},                      // all separators → empty (contributes nothing)
		{"", ""},
	} {
		if got := goName(tc.in); got != tc.want {
			t.Errorf("goName(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// A camelCase operationId (oapi-codegen normalizes it to PascalCase for the generated
// names) must still resolve to the generated shapes — the fix for the raw-operationId
// gap that silently missed every non-PascalCase id.
func TestGeneratedNameLookupCamelCase(t *testing.T) {
	c := newClientTable("event-bus", []Operation{
		{OperationID: "getEventStatus", Method: "GET", Template: "/v1/events/{id}/status"},
	})
	const label = "event-bus GET /v1/events/{id}/status"
	for _, name := range []string{
		"GetEventStatus",
		"GetEventStatusWithResponse",
		"GetEventStatusWithBodyWithResponse",
		"NewGetEventStatusRequest",
	} {
		if got := c.byName[name]; got != label {
			t.Errorf("byName[%q] = %q, want %q (camelCase operationId must normalize)", name, got, label)
		}
	}
	// The raw camelCase form must NOT be a key (it is not what oapi-codegen generates).
	if _, ok := c.byName["getEventStatusWithResponse"]; ok {
		t.Error("raw camelCase name must not be a lookup key")
	}
}

// When two operations' generated names collide (here operationId "Foo" generates
// "FooWithResponse", and operationId "FooWithResponse" generates it as the bare form),
// the colliding name is DROPPED — the fail-closed choice, so the callee surfaces as a
// blind spot rather than being labeled with a guessed operation. Names unique to each
// operation still resolve.
func TestAmbiguousGeneratedNameDropped(t *testing.T) {
	c := newClientTable("peer", []Operation{
		{OperationID: "Foo", Method: "GET", Template: "/foo"},
		{OperationID: "FooWithResponse", Method: "POST", Template: "/foo-wr"},
	})
	if got, ok := c.byName["FooWithResponse"]; ok {
		t.Errorf("colliding name FooWithResponse should be dropped, got %q", got)
	}
	if got := c.byName["Foo"]; got != "peer GET /foo" {
		t.Errorf("byName[Foo] = %q, want %q", got, "peer GET /foo")
	}
	if got := c.byName["NewFooRequest"]; got != "peer GET /foo" {
		t.Errorf("byName[NewFooRequest] = %q, want %q", got, "peer GET /foo")
	}
	if got := c.byName["FooWithResponseWithResponse"]; got != "peer POST /foo-wr" {
		t.Errorf("byName[FooWithResponseWithResponse] = %q, want %q", got, "peer POST /foo-wr")
	}
}

// A quoted, space-padded package/peer/spec passes config validation (TrimSpace is
// non-empty), so NewLabeler must trim before keying/matching — else the padded package
// would never equal features.EffectivePkgPath and the client would be a silent no-op.
func TestNewLabelerTrimsFields(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "s.yaml"), []byte(eventBusSpec), 0o644); err != nil {
		t.Fatal(err)
	}
	l, err := NewLabeler([]config.OpenAPIClientHint{{Package: "  ex.com/c  ", Peer: " event-bus ", Spec: " s.yaml "}}, dir)
	if err != nil {
		t.Fatal(err)
	}
	c := l.byPkg["ex.com/c"]
	if c == nil {
		t.Fatalf("byPkg must key on the TRIMMED package; keys=%v", l.byPkg)
	}
	if c.peer != "event-bus" {
		t.Errorf("peer = %q, want trimmed %q", c.peer, "event-bus")
	}
	// The trimmed peer flows into labels (no doubled/edge spaces).
	if got := c.byName["CreateEventWithResponse"]; !strings.HasPrefix(got, "event-bus POST ") {
		t.Errorf("label = %q, want it to start with the trimmed peer", got)
	}
}

// FollowWrappers threads the per-client opt-in from the hint onto the built table and
// reads it back package-scoped: a client that set followWrappers is descendable, one
// that did not (the default) is not. The threaded field is what graphio's descent path
// gates on. The method's package resolution over a real *ssa.Function is exercised in
// graphio (integration); here we pin the threading and the nil no-op paths.
func TestFollowWrappersOptIn(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "s.yaml"), []byte(eventBusSpec), 0o644); err != nil {
		t.Fatal(err)
	}
	l, err := NewLabeler([]config.OpenAPIClientHint{
		{Package: "ex.com/on", Peer: "p", Spec: "s.yaml", FollowWrappers: true},
		{Package: "ex.com/off", Peer: "p", Spec: "s.yaml"}, // FollowWrappers defaults false
	}, dir)
	if err != nil {
		t.Fatal(err)
	}
	if on := l.byPkg["ex.com/on"]; on == nil || !on.followWrappers {
		t.Errorf("followWrappers must thread true for the opted-in client: %+v", on)
	}
	if off := l.byPkg["ex.com/off"]; off == nil || off.followWrappers {
		t.Errorf("followWrappers must default false for a client that did not opt in: %+v", off)
	}
	// A nil labeler and a nil function both answer false (the byte-identical no-op path).
	var nilLab *Labeler
	if nilLab.FollowWrappers(nil) {
		t.Error("nil labeler FollowWrappers must be false")
	}
	if l.FollowWrappers(nil) {
		t.Error("nil function FollowWrappers must be false")
	}
}

func TestNewLabelerNoClientsIsNil(t *testing.T) {
	l, err := NewLabeler(nil, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if l != nil {
		t.Errorf("no clients configured must yield a nil labeler, got %+v", l)
	}
}

func TestNewLabelerErrors(t *testing.T) {
	dir := t.TempDir()
	writeSpec := func(name, body string) string {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return name
	}
	good := writeSpec("good.yaml", eventBusSpec)
	empty := writeSpec("empty.yaml", "openapi: 3.0.0\n")
	bad := writeSpec("bad.yaml", "\tnot: [valid")

	for _, tc := range []struct {
		name    string
		clients []config.OpenAPIClientHint
		wantErr string // substring the error must name
	}{
		{"missing spec file", []config.OpenAPIClientHint{{Package: "ex.com/c", Peer: "p", Spec: "nope.yaml"}}, "ex.com/c"},
		{"unparseable spec", []config.OpenAPIClientHint{{Package: "ex.com/c", Peer: "p", Spec: bad}}, "ex.com/c"},
		{"zero-operation spec", []config.OpenAPIClientHint{{Package: "ex.com/c", Peer: "p", Spec: empty}}, "no operation"},
		{"duplicate package", []config.OpenAPIClientHint{
			{Package: "ex.com/c", Peer: "p", Spec: good},
			{Package: "ex.com/c", Peer: "q", Spec: good},
		}, "more than once"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewLabeler(tc.clients, dir)
			if err == nil {
				t.Fatalf("expected an error naming the config entry")
			}
			if tc.wantErr != "" && !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("error %q does not mention %q", err.Error(), tc.wantErr)
			}
		})
	}

	// The happy path builds a non-nil labeler.
	l, err := NewLabeler([]config.OpenAPIClientHint{{Package: "ex.com/c", Peer: "event-bus", Spec: good}}, dir)
	if err != nil {
		t.Fatal(err)
	}
	if l == nil || l.byPkg["ex.com/c"] == nil {
		t.Fatalf("labeler missing the declared package: %+v", l)
	}
	if got := l.byPkg["ex.com/c"].byName["CreateEventWithResponse"]; got != "event-bus POST /v1/publishers/{publisherId}/eventTypes/{eventType}/versions/{version}/events/{eventId}" {
		t.Errorf("CreateEventWithResponse label = %q", got)
	}
}
