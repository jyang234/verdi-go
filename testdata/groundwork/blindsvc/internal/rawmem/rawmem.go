// Package rawmem imports unsafe, which marks the whole package as a graph blind
// spot: pointer conversions can route data (and intent) around anything the call
// graph can see. The function need not be reachable for the package disclosure to
// fire.
package rawmem

import "unsafe"

// Size returns the in-memory size of v. The unsafe import is the point.
func Size[T any](v T) uintptr {
	return unsafe.Sizeof(v)
}
