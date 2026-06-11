// Package bus is the fixture's must-precede subject: a publish that must be
// preceded by an audit write.
package bus

// Publish emits an event.
func Publish(event string) {}
