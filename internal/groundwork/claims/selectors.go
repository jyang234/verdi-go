package claims

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

// Selectors is one selector family decoded from either a scalar JSON string or
// a JSON string list. It retains that source shape so structural claims can
// reject lists without silently widening their existing scalar semantics.
type Selectors struct {
	values  []string
	wasList bool
}

// UnmarshalJSON accepts exactly one non-empty string or one non-empty list of
// non-empty strings. It assigns the receiver only after the complete value has
// passed validation.
func (s *Selectors) UnmarshalJSON(data []byte) error {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return errors.New("selectors: expected a JSON string or array of strings")
	}

	dec := json.NewDecoder(bytes.NewReader(data))
	var (
		values  []string
		wasList bool
	)
	switch trimmed[0] {
	case '"':
		var value string
		if err := dec.Decode(&value); err != nil {
			return fmt.Errorf("selectors: decode string: %w", err)
		}
		values = []string{value}
	case '[':
		if err := dec.Decode(&values); err != nil {
			return fmt.Errorf("selectors: decode list: %w", err)
		}
		wasList = true
	default:
		return errors.New("selectors: expected a JSON string or array of strings")
	}
	if _, err := dec.Token(); err != io.EOF {
		return errors.New("selectors: trailing data after JSON value")
	}

	if len(values) == 0 {
		return errors.New("selectors: list must not be empty")
	}
	for i, value := range values {
		if strings.TrimSpace(value) != "" {
			continue
		}
		if wasList {
			return fmt.Errorf("selectors: list member %d must not be empty or whitespace", i+1)
		}
		return errors.New("selectors: string must not be empty or whitespace")
	}

	s.values = append([]string(nil), values...)
	s.wasList = wasList
	return nil
}

// Values returns a copy of the decoded selectors in input order.
func (s Selectors) Values() []string {
	return append([]string(nil), s.values...)
}

// Scalar returns the selector only when its original JSON form was scalar.
func (s Selectors) Scalar() (string, bool) {
	if s.wasList || len(s.values) != 1 {
		return "", false
	}
	return s.values[0], true
}

// Present reports whether the selector family contains any decoded value.
func (s Selectors) Present() bool {
	return len(s.values) > 0
}

// WasList reports whether the selector family's original JSON form was a list.
func (s Selectors) WasList() bool {
	return s.wasList
}
