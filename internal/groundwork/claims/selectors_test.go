package claims

import (
	"reflect"
	"testing"
)

func TestSelectorsAcceptedForms(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		wantValues []string
		wantScalar string
		wantList   bool
	}{
		{
			name:       "scalar",
			input:      `"example.com/service.Handler"`,
			wantValues: []string{"example.com/service.Handler"},
			wantScalar: "example.com/service.Handler",
		},
		{
			name:       "one-element list",
			input:      `["example.com/service.Handler"]`,
			wantValues: []string{"example.com/service.Handler"},
			wantList:   true,
		},
		{
			name:       "multiple selectors",
			input:      `["example.com/service.Handler","example.com/service.Worker"]`,
			wantValues: []string{"example.com/service.Handler", "example.com/service.Worker"},
			wantList:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got Selectors
			if err := got.UnmarshalJSON([]byte(tt.input)); err != nil {
				t.Fatalf("UnmarshalJSON(%s): %v", tt.input, err)
			}
			if !got.Present() {
				t.Fatal("Present() = false, want true")
			}
			if got.WasList() != tt.wantList {
				t.Errorf("WasList() = %t, want %t", got.WasList(), tt.wantList)
			}
			if values := got.Values(); !reflect.DeepEqual(values, tt.wantValues) {
				t.Errorf("Values() = %#v, want %#v", values, tt.wantValues)
			}
			scalar, ok := got.Scalar()
			if tt.wantList {
				if ok || scalar != "" {
					t.Errorf("Scalar() = %q, %t, want empty, false for original list", scalar, ok)
				}
			} else if !ok || scalar != tt.wantScalar {
				t.Errorf("Scalar() = %q, %t, want %q, true", scalar, ok, tt.wantScalar)
			}
		})
	}
}

func TestSelectorsValuesReturnsCopy(t *testing.T) {
	var selectors Selectors
	if err := selectors.UnmarshalJSON([]byte(`["example.com/service.Handler","example.com/service.Worker"]`)); err != nil {
		t.Fatal(err)
	}

	values := selectors.Values()
	values[0] = "mutated"

	want := []string{"example.com/service.Handler", "example.com/service.Worker"}
	if got := selectors.Values(); !reflect.DeepEqual(got, want) {
		t.Errorf("Values() after caller mutation = %#v, want %#v", got, want)
	}
}

func TestSelectorsRejectedForms(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{name: "empty input", input: ``},
		{name: "whitespace input", input: `   `},
		{name: "null", input: `null`},
		{name: "empty string", input: `""`},
		{name: "whitespace-only string", input: `"   "`},
		{name: "empty list", input: `[]`},
		{name: "whitespace-only list member", input: `["example.com/service.Handler","  "]`},
		{name: "non-string list member", input: `["example.com/service.Handler",1]`},
		{name: "object", input: `{"selector":"example.com/service.Handler"}`},
		{name: "number", input: `1`},
		{name: "scalar trailing data", input: `"example.com/service.Handler" null`},
		{name: "list trailing data", input: `["example.com/service.Handler"] true`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var first Selectors
			err1 := first.UnmarshalJSON([]byte(tt.input))
			if err1 == nil {
				t.Fatalf("UnmarshalJSON(%s) succeeded, want error", tt.input)
			}
			var second Selectors
			err2 := second.UnmarshalJSON([]byte(tt.input))
			if err2 == nil {
				t.Fatalf("second UnmarshalJSON(%s) succeeded, want error", tt.input)
			}
			if err1.Error() != err2.Error() {
				t.Errorf("non-deterministic errors: first %q, second %q", err1, err2)
			}
		})
	}
}
