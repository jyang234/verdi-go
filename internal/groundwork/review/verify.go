package review

import (
	"fmt"

	"github.com/jyang234/golang-code-graph/internal/groundwork/graph"
	"github.com/jyang234/golang-code-graph/internal/groundwork/policy"
)

// Authenticity is the result of verifying an artifact against the source graphs.
type Authenticity struct {
	Status Status
	Detail string
}

// Status is the three-way authenticity outcome.
type Status string

const (
	// Authentic: the artifact's digest is self-consistent AND equals the digest
	// recomputed from the trusted source graphs.
	Authentic Status = "AUTHENTIC"
	// Tampered: a field was edited without recomputing the digest — the body no
	// longer hashes to its own claimed digest.
	Tampered Status = "TAMPERED"
	// Stale: the body is self-consistent but its digest does not match what the
	// real base/branch graphs produce — the artifact describes different code
	// (stale), or was re-signed over a doctored body against the trusted graphs.
	Stale Status = "STALE"
)

// OK reports whether the artifact is authentic.
func (a Authenticity) OK() bool { return a.Status == Authentic }

// VerifyArtifact proves an artifact is what it claims, with two independent
// checks (see docs/groundwork/mr-review-artifacts.md):
//
//  1. Body integrity — the artifact must hash to its own claimed digest. Editing
//     any field (e.g. flipping BLOCK→CLEAR) without recomputing breaks this.
//  2. Code correspondence — the claimed digest must equal the digest recomputed
//     from the real base/branch graphs. This is the load-bearing check, and it is
//     only as trustworthy as the graphs: they MUST be CI-generated from the
//     actual code, never supplied by the agent under review.
//
// A re-signed forgery (body edited AND digest recomputed) passes check 1 but
// fails check 2, because the verifier recomputes from the trusted graphs and gets
// a different digest. The digest itself proves nothing; the recomputation does.
func VerifyArtifact(claimed Artifact, p *policy.Policy, base, branch *graph.Graph) Authenticity {
	if got := digestOf(claimed); got != claimed.Digest {
		return Authenticity{
			Status: Tampered,
			Detail: fmt.Sprintf("artifact body hashes to %s but claims digest %s — a field was edited after signing", short(got), short(claimed.Digest)),
		}
	}
	recomputed := Review(p, base, branch)
	if recomputed.Digest != claimed.Digest {
		return Authenticity{
			Status: Stale,
			Detail: fmt.Sprintf("claimed digest %s but the source graphs produce %s — the artifact describes different code (or was re-signed over a doctored body)", short(claimed.Digest), short(recomputed.Digest)),
		}
	}
	return Authenticity{
		Status: Authentic,
		Detail: fmt.Sprintf("digest %s matches the recomputation from the source graphs", short(claimed.Digest)),
	}
}
