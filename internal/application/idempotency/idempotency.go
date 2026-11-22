// Package idempotency holds the stored response of an Idempotency-Key.
package idempotency

import "time"

// TTL is how long a key is remembered (D3): the one age both stores use — a
// row older than TTL is not replayed (the read's age check) and is deleted by
// PurgeExpired (retention).
const TTL = 24 * time.Hour

// Record is the first response stored under a key.
type Record struct {
	RequestHash string
	Status      int
	ContentType string
	Body        []byte
	CreatedAt   time.Time
	// Release asks the store not to remember the answer: the request is rolled
	// back, key row included, so the key is free and the same request runs
	// again. The middleware sets it for every answer that is not a 2xx and
	// whose problem type is not FINAL or OUTCOME (ADR 0009).
	Release bool
}
