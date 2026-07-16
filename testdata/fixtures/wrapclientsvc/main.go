// Command wrapclientsvc is the fixture for OpenAPI-client WRAPPER DESCENT
// (--reclaim-openapi with classify.openapiClients[i].followWrappers: true): a service
// whose outbound edges to the event-bus peer go through hand-written Registrar wrappers
// over a SEPARATE-MODULE spec-generated client (example.com/wrapclientlib/eventbus). The
// wrappers assemble their routes dynamically and the service never calls the generated
// methods through them, so without descent every call is an UnresolvedSpecOperation; with
// descent the labeler follows each wrapper to the generated operation(s) it reaches.
//
// The single handler exercises every descent outcome (see ensure): a one-operation
// wrapper (named), a two-operation wrapper (ambiguous — disclosed), a zero-operation
// wrapper (disclosed), and a DIRECT generated-operation call (plain via=openapi-client,
// the path descent must leave inert). The two constructor calls are non-operations, each
// a zero-operation disclosure. Two more wrappers exercise the INCOMPLETE-walk guard: one
// whose second operation sits past the depth cap (a deep helper chain) and one that hands
// off to a declared-but-not-widened bodiless package — both stay disclosed and are NEVER
// named, so a truncated walk can never NAME a genuinely-ambiguous wrapper.
package main

import (
	"net/http"

	"example.com/wrapclientlib/eventbus"
)

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /ensure", ensure)
	_ = http.ListenAndServe(":8080", mux)
}

// ensure is the HTTP entry point. It reaches the event-bus peer only through the
// separate-module client — every generated-name shape the labeler must descend to or
// resolve directly is exercised here.
func ensure(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	const server = "http://event-bus"

	// The generated constructor and the wrapper constructor are NOT spec operations:
	// each surfaces as a zero-operation UnresolvedSpecOperation disclosure.
	client := eventbus.NewClientWithResponses(server)
	reg := eventbus.NewRegistrar(client)

	// (a) A wrapper reaching EXACTLY ONE operation through one helper hop: NAMED
	// boundary:event-bus POST /v1/eventTypeTemplates, tagged via=openapi-client-wrapper.
	_ = reg.EnsureTemplate(ctx)

	// (b) A wrapper branching to TWO operations: stays disclosed (ambiguous — the
	// descent names both in sorted order, never guesses between them).
	_ = reg.EnsureParticipant(ctx, "publisher")

	// (c) A wrapper reaching ZERO operations (transport helper only): stays disclosed
	// with the zero-found descent outcome.
	_ = reg.Warm(ctx)

	// (d) A DIRECT generated-operation call: named boundary:event-bus GET
	// /v1/eventTypeTemplates/{templateId}, plain via=openapi-client (descent is gated on
	// a no-name-match, so a direct hit never enters it).
	_, _ = client.GetTemplateWithResponse(ctx, "tmpl-1")

	// (e) A wrapper reaching a second operation only past the descent depth cap: the
	// truncated walk is INCOMPLETE, so it stays disclosed (never named) with the depth-cap
	// outcome — the guard against a truncation NAMING a genuinely-ambiguous wrapper.
	_ = reg.EnsureDeep(ctx)

	// (f) A wrapper handing off to a declared-but-not-widened (bodiless) package: the
	// descent dead-ends at the bodiless hop, an INCOMPLETE walk that stays disclosed with
	// the bodiless-package outcome naming which package needs followWrappers.
	_ = reg.EnsureViaLegacy(ctx)
}
