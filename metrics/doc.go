// Package metrics aggregates a runtime's events into bounded measurements.
//
// The runtime deliberately defines events and not a metrics backend, and it
// states the rule that makes the difference between a measurement and an
// outage: never label by a message, a path, an error string or an arbitrary
// annotation value. An unbounded label is an unbounded map, and a metrics
// pipeline that grows one has become the incident.
//
// So the labels here are bounded by construction. An event's kind and status
// are bounded by the runtime -- seventeen kinds, five statuses -- and the
// operation name is bounded by the caller, who declares the names worth
// distinguishing and gets Other for everything else. There is no way to ask
// this package for a label it cannot bound.
//
// What it produces is a snapshot: counts, durations and retry delays, in a
// form an exporter can read without this package knowing which exporter.
// Nothing here talks to Prometheus, OpenTelemetry or statsd, for the reason
// the runtime gives for not talking to them either.
package metrics
