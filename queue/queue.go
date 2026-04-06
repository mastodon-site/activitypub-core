// Package queue defines job enqueue/dequeue contracts for API and worker processes.
package queue

import (
	"context"
	"encoding/json"
	"time"
)

// Type identifies a job kind (e.g. deliver_activity, noop).
type Type string

const (
	TypeNoop                 Type = "noop"
	TypeDeliverActivity      Type = "deliver_activity"
	TypeProcessInboxActivity Type = "process_inbox_activity"
)

// Job is a unit of asynchronous work.
type Job struct {
	Type           Type
	Payload        json.RawMessage
	IdempotencyKey string
	RunAfter       time.Time
}

// Lease represents a claimed job in progress.
type Lease struct {
	ID       int64
	Type     Type
	Payload  json.RawMessage
	Attempts int // delivery attempts already recorded before this run (SQL queue); 0 if unknown.
}

// SQLDelayedNack is implemented by SQL queue for delivery backoff and permanent failure.
type SQLDelayedNack interface {
	// NackSchedule marks a processing job failed permanently, or returns it to pending with run_after and incremented attempts.
	NackSchedule(ctx context.Context, id int64, permanent bool, runAfter time.Time, lastErr string) error
}

// Backend abstracts Redis, SQL, or other brokers.
type Backend interface {
	Enqueue(ctx context.Context, job Job) error
	// Dequeue blocks until a job is available or ctx is done (implementation may poll).
	Dequeue(ctx context.Context) (*Lease, error)
	Ack(ctx context.Context, id int64) error
	Nack(ctx context.Context, id int64, requeue bool) error
}
