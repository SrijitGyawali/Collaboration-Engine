// Package events wraps the Kafka producer and consumers for the background
// path. Messages are keyed by document ID so one document's events stay in
// order on a single partition.
package events
