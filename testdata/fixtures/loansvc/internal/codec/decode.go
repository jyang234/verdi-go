// Package codec holds a generic JSON decoder. It exists so the fixture exercises
// a generic function: the static pipeline builds SSA with InstantiateGenerics, so
// Decode[T] must be reachable through its concrete instantiation in the handler.
package codec

import (
	"encoding/json"
	"io"
)

// Decode reads a JSON value of type T from r. The handler instantiates it as
// Decode[origination.Application], which is the instantiation the call graph must
// reach.
func Decode[T any](r io.Reader) (T, error) {
	var v T
	err := json.NewDecoder(r).Decode(&v)
	return v, err
}
