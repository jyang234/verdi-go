package fitness

import (
	"testing"

	"github.com/jyang234/golang-code-graph/internal/groundwork/policy"
)

// exempted must bind From/To at identifier boundaries: an exception for the edge
// "app.Get → store.Find" must NOT silently also exempt the unrelated
// "app.GetUserAvatar → store.FindByID", which a bare strings.HasPrefix would,
// suppressing a real layering violation.
func TestExemptedBoundary(t *testing.T) {
	allow := []policy.Exception{{From: "svc/app.Get", To: "svc/store.Find"}}

	if !exempted(allow, "svc/app.Get", "svc/store.Find") {
		t.Error("the exact blessed edge must be exempt")
	}
	if exempted(allow, "svc/app.GetUserAvatar", "svc/store.FindByID") {
		t.Error("an unrelated edge sharing a name prefix must NOT be exempt — a real violation would be suppressed")
	}
}
