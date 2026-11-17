// Package pubsub carries committed operations from the sync service to every
// gateway, so clients connected to different gateways see each other's edits.
// Presence updates (who is in the document, where their cursor is) travel
// the same way, published by the gateways themselves.
//
// The sync service publishes one Message per stored batch, from the
// document's actor, so a document's messages are published in sequence order.
// Each gateway subscribes to a document while at least one of its clients has
// it open, and forwards each message to those clients.
//
// Two implementations share one interface: Redis Pub/Sub for production, and
// Memory for tests and single-process runs. Delivery is best effort in both:
// a message published while a gateway is not subscribed, or while Redis is
// unreachable, is not delivered later. That is safe because PostgreSQL holds
// every operation, and clients catch up from it by sequence number.
package pubsub
