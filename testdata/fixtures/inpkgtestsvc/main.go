// Command inpkgtestsvc is the fixture for loader.Load's Tests: false tripwire.
//
// It is deliberately minimal and dependency-free. Its go.mod requires nothing,
// and its test file imports only "testing", so `go list -test ./...` resolves
// without a single go.mod update — with or without the repository workspace.
// That matters: the tripwire must fail on ITS OWN assertion (a .test-variant
// package, or one PkgPath loaded twice) when Tests is flipped to true, not on an
// unrelated module-resolution error from a fixture whose test dependencies are
// not declared.
//
// The in-package test file is what arms it. An EXTERNAL (package foo_test) test
// yields only `foo_test [foo.test]` and `foo.test`, whose paths differ from the
// real package; the in-package variant `foo [foo.test]` is the one that reports
// the SAME PkgPath over the SAME files at the SAME byte offsets — the collision
// class the function-local type discriminator cannot decide. See "Physical
// position" in
// docs/superpowers/specs/2026-07-26-local-generic-type-identity-design.md.
package main

import "fmt"

func greet(name string) string { return "hello, " + name }

func main() { fmt.Println(greet("world")) }
