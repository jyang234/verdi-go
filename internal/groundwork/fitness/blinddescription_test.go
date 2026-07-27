package fitness

import (
	"testing"

	"github.com/jyang234/golang-code-graph/internal/groundwork/facts"
)

// TestBlindDescriptionConcurrentLocations pins the prose the concurrent blind
// locations render. It lives in its own file, apart from
// TestConcurrentCharacterization, precisely because it names the post-extraction
// facts.BlindLocation constants: a characterization test that pins the behavior
// an extraction must preserve has to COMPILE against the pre-extraction package,
// or it cannot be run in the "before" state it exists to capture.
func TestBlindDescriptionConcurrentLocations(t *testing.T) {
	tests := []struct {
		name    string
		witness *facts.BlindWitness
		want    string
	}{
		{
			name: "dynamic direct boundary",
			witness: &facts.BlindWitness{
				Site: "svc.Owner", Kind: "DynamicEffect",
				Detail:   "boundary:bus PUBLISH <dynamic>",
				Location: facts.BlindAtConcurrentBoundary,
			},
			want: "unresolved concurrent boundary effect boundary:bus PUBLISH <dynamic>",
		},
		{
			name: "concurrent dispatch",
			witness: &facts.BlindWitness{
				Site: "example.com/svc/internal/app.Service.run",
				Kind: "ConcurrentDispatch", Detail: "go f()",
				Location: facts.BlindAtConcurrentDispatch,
			},
			want: "ConcurrentDispatch at app.Service.run",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := blindDescription(tt.witness); got != tt.want {
				t.Fatalf("blindDescription() = %q, want %q", got, tt.want)
			}
		})
	}
}
