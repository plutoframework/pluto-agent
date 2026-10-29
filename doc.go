// Package agent provides typed, bounded AI-backed application runs.
//
// The host authenticates dependencies before invoking an agent. Tools receive
// those dependencies separately from strictly validated model arguments.
// Permission checks, resource policies, and optional host approval are enforced
// at every dispatch. Provider output and streaming deltas are untrusted until
// final schema and application validation succeed.
//
// Runs are in-memory operations, not durable workflows. Context cancellation
// requires cooperative providers, policies, observers, and handlers. Retrying a
// run can repeat effects; the host owns idempotency and durable audit records.
package agent
