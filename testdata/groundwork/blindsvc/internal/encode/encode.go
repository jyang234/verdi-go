// Package encode marshals values reflectively. The reflect call is invisible to
// the static call graph, so any function reaching it is a `reflect` blind spot:
// downstream behavior cannot be followed structurally.
package encode

import "reflect"

// Marshal renders v's type name as bytes. The body is irrelevant; the point is
// the reflect.* call, which the call graph cannot see through.
func Marshal(v any) []byte {
	t := reflect.TypeOf(v)
	if t == nil {
		return nil
	}
	return []byte(t.String())
}
