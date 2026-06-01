package canonjson

import (
	"bytes"
	"strings"
	"testing"
)

// Go map iteration order is randomized; if Marshal leaked it, this would flake.
func TestMarshalSortsMapKeysDeterministically(t *testing.T) {
	want, err := Marshal(map[string]string{"a": "1", "b": "2", "c": "3", "z": "26"})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 100; i++ {
		got, err := Marshal(map[string]string{"z": "26", "c": "3", "b": "2", "a": "1"})
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("iteration %d not byte-identical:\n want %s\n got  %s", i, want, got)
		}
	}
	// Keys appear in sorted order.
	if ai, zi := strings.Index(string(want), `"a"`), strings.Index(string(want), `"z"`); ai > zi {
		t.Fatalf("keys not sorted: %s", want)
	}
}

func TestMarshalNoHTMLEscaping(t *testing.T) {
	got, err := Marshal(map[string]string{"placeholder": "<uuid> & </id>"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "<uuid> & </id>") {
		t.Fatalf("HTML escaping not disabled: %s", got)
	}
}

func TestMarshalTrailingNewline(t *testing.T) {
	got, err := Marshal(map[string]string{"k": "v"})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasSuffix(got, []byte("\n")) {
		t.Fatalf("missing trailing newline: %q", got)
	}
}

// Struct fields keep declaration order (deterministic), not alphabetical.
func TestMarshalStructFieldOrderStable(t *testing.T) {
	type inner struct {
		Op   string `json:"op"`
		Kind string `json:"kind"`
	}
	a, _ := Marshal(inner{Op: "x", Kind: "y"})
	b, _ := Marshal(inner{Op: "x", Kind: "y"})
	if !bytes.Equal(a, b) {
		t.Fatal("struct marshal not stable")
	}
	if strings.Index(string(a), `"op"`) > strings.Index(string(a), `"kind"`) {
		t.Fatalf("expected declaration order op,kind: %s", a)
	}
}
