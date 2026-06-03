// Package bus is the service's event-bus seam. .flowmap.yaml names Publish under
// classify.busPublish so the static pipeline treats a call to it as an outbound
// published event.
package bus

// Publish emits event onto the bus.
func Publish(event string) {}
