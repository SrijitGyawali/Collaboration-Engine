// Package pubsub wraps Redis for the live path: per-document Pub/Sub channels
// that fan committed operations out to every gateway, and presence keys with
// a TTL. Delivery is best effort; clients recover missed operations from the
// operations log by sequence number.
package pubsub
