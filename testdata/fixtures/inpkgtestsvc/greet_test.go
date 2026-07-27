// This test file is IN-PACKAGE (package main, not main_test) on purpose: it is
// what makes go/packages produce the same-PkgPath test variant under
// Tests: true, which is the whole point of the fixture. Moving it to an external
// test package would silently disarm loader's Tests tripwire. It imports only
// "testing", so loading it needs no go.mod change.
package main

import "testing"

func TestGreet(t *testing.T) {
	if got := greet("world"); got != "hello, world" {
		t.Fatalf("greet() = %q", got)
	}
}
