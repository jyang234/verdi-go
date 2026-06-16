package canonjson

import (
	"bytes"
	"encoding/json"
	"testing"
)

// FuzzMarshalDeterministic pins the property the whole gating model rests on:
// canonjson.Marshal is a deterministic function of the VALUE, independent of Go's
// randomized map iteration order, at every nesting depth — and never panics on
// arbitrary marshalable input. It parses fuzz bytes into a generic value (nested
// map[string]any / []any, the shape with randomized key order) and marshals it
// repeatedly, asserting byte-identical output every time.
func FuzzMarshalDeterministic(f *testing.F) {
	for _, s := range []string{
		`{}`, `{"z":1,"a":2,"m":{"y":1,"b":2}}`, `[1,2,{"b":1,"a":2}]`,
		`{"placeholder":"<uuid> & </id>"}`, `null`, `"x"`, `123`, `{"k":[{"d":1,"c":2},{"a":3}]}`,
	} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		var v any
		if err := json.Unmarshal(data, &v); err != nil {
			return // only well-formed JSON values are in scope
		}
		first, err := Marshal(v)
		if err != nil {
			t.Fatalf("Marshal errored on a value parsed from valid JSON: %v", err)
		}
		// Re-marshal many times: Go randomizes map iteration per range, so a
		// dropped key-sort would diverge here.
		for i := 0; i < 20; i++ {
			got, err := Marshal(v)
			if err != nil {
				t.Fatalf("Marshal errored on re-run: %v", err)
			}
			if !bytes.Equal(got, first) {
				t.Fatalf("non-deterministic marshal:\n first: %s\n got:   %s", first, got)
			}
		}
	})
}
