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
