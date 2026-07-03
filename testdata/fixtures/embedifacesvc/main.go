// Command embedifacesvc is the minimal reproduction of the CHA wrapper-splice
// cycle: a struct that EMBEDS an interface, the way oapi-codegen's generated
// client does (`type ClientWithResponses struct { ClientInterface }`).
//
// The embedding makes go/ssa mint a promotion wrapper (*ClientWithResponses).DoThing
// (Synthetic = "wrapper for func (ClientInterface).DoThing(...)", Pkg == nil) whose
// body invokes the embedded interface method. Under --algo cha that invoke resolves
// to EVERY implementer of ClientInterface — including *ClientWithResponses itself,
// whose DoThing method IS that same promotion wrapper. The call graph therefore
// carries a legitimate (*ClientWithResponses).DoThing → (*ClientWithResponses).DoThing
// self-edge (plus an edge to the value-receiver variant), and graphio's wrapper
// splice must cut the revisit instead of recursing to the depth cap. RTA and VTA
// narrow the invoke to *Client and never see the cycle, which is why only cha
// tripped in the field (flowmap-findings-2026-07-03 Finding 5, event-bus
// clients/admin).
package main

import "context"

type ClientInterface interface {
	DoThing(ctx context.Context) error
}

type Client struct{}

func (c *Client) DoThing(ctx context.Context) error { return nil }

type ClientWithResponses struct {
	ClientInterface
}

func (c *ClientWithResponses) DoThingWithResponse(ctx context.Context) error {
	return c.ClientInterface.DoThing(ctx)
}

func main() {
	c := &ClientWithResponses{ClientInterface: &Client{}}
	if err := c.DoThingWithResponse(context.Background()); err != nil {
		panic(err)
	}
}
