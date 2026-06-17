package blindspots_test

import (
	"testing"

	"github.com/jyang234/golang-code-graph/internal/impeach"
	"github.com/jyang234/golang-code-graph/internal/static/blindspots"
)

// TestBlindSpotKindParity pins the one-source-of-truth invariant that the seam
// kind named by the impeachment cell (impeach.BlindSpotKindImpeachment, where a
// repair is proposed) and the kind recognized in the static manifest
// (blindspots.ImpeachmentSeam, where it is merged and gated) are the SAME string.
// impeach cannot import blindspots without an import cycle, so the value is spelled
// by hand in both places; if a refactor renames one, this test fails rather than
// letting the proposed repair carry a kind the static side silently drops as
// unrecognized (graphio.mergeDeclaredBlindSpots now rejects unrecognized kinds, so
// the drift would otherwise surface only as a confusing config error).
func TestBlindSpotKindParity(t *testing.T) {
	if impeach.BlindSpotKindImpeachment != string(blindspots.ImpeachmentSeam) {
		t.Fatalf("seam-kind parity broken: impeach.BlindSpotKindImpeachment = %q, blindspots.ImpeachmentSeam = %q",
			impeach.BlindSpotKindImpeachment, blindspots.ImpeachmentSeam)
	}
	// And the kind must be in the recognized set, or the merge would reject a
	// genuinely-ratified seam.
	if !blindspots.Recognized(blindspots.ImpeachmentSeam) {
		t.Fatalf("blindspots.ImpeachmentSeam is not in Kinds(); a ratified seam would be rejected by the merge")
	}
}
