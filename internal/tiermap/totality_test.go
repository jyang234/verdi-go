package tiermap

import (
	"testing"

	"github.com/jyang234/golang-code-graph/internal/model"
)

// TestClassifyIsTotal pins the classifier's documented invariant — "a fixed
// config plus a given operation always yields exactly one tier" — across the
// WHOLE feature space, not just the per-rule examples. For every combination of
// the enum values (plus the unset "" for each) and both bools, Default().Classify
// must return a tier in [1,4] with a non-empty deciding rule, and be
// deterministic. The catch-all is what guarantees totality; this guard fails if a
// future rule edit ever leaves an operation unclassified or out of range.
func TestClassifyIsTotal(t *testing.T) {
	boundaries := []model.Boundary{"", model.BoundaryInbound, model.BoundaryInternal, model.BoundaryCrossPackage, model.BoundaryOutboundSync, model.BoundaryOutboundAsync}
	effects := []model.Effect{"", model.EffectMutate, model.EffectRead, model.EffectIO, model.EffectTelemetry, model.EffectCompute, model.EffectUnknown}
	origins := []model.Origin{"", model.OriginSamePackage, model.OriginFirstParty, model.OriginThirdParty, model.OriginStdlib, model.OriginUnknown}

	c := Default()
	for _, b := range boundaries {
		for _, e := range effects {
			for _, o := range origins {
				for _, fallible := range []bool{false, true} {
					for _, concurrent := range []bool{false, true} {
						f := model.Features{Boundary: b, Effect: e, Origin: o, Fallible: fallible, Concurrent: concurrent}
						tier, rule := c.Classify(f)
						if tier < 1 || tier > 4 {
							t.Errorf("Classify(%+v) = tier %d, want 1..4", f, tier)
						}
						if rule == "" {
							t.Errorf("Classify(%+v) returned an empty deciding rule (unclassified)", f)
						}
						if t2, r2 := c.Classify(f); t2 != tier || r2 != rule {
							t.Errorf("Classify(%+v) not deterministic: (%d,%q) vs (%d,%q)", f, tier, rule, t2, r2)
						}
					}
				}
			}
		}
	}
}
