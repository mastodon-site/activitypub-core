package sqlqueue

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mastodon-site/activitypub-core/queue"
)

// SQL is a Postgres-backed queue using SKIP LOCKED.
type SQL struct {
	pool *pgxpool.Pool
}

// New creates a SQL queue backend.
func New(pool *pgxpool.Pool) *SQL {
	return &SQL{pool: pool}
}

// Enqueue inserts a pending job.
func (s *SQL) Enqueue(ctx context.Context, job queue.Job) error {
	runAt := job.RunAfter
	if runAt.IsZero() {
		runAt = time.Now().UTC()
	}
	key := job.IdempotencyKey
	_, err := s.pool.Exec(ctx, `
		INSERT INTO queue_jobs (job_type, payload, idempotency_key, status, run_after)
		VALUES ($1, $2, NULLIF($3, ''), 'pending', $4)
	`, string(job.Type), job.Payload, key, runAt)
	return err
}

// EnqueueTx inserts a pending job inside an existing transaction (pair with inbound activity insert).
func (s *SQL) EnqueueTx(ctx context.Context, tx pgx.Tx, job queue.Job) error {
	runAt := job.RunAfter
	if runAt.IsZero() {
		runAt = time.Now().UTC()
	}
	key := job.IdempotencyKey
	_, err := tx.Exec(ctx, `
		INSERT INTO queue_jobs (job_type, payload, idempotency_key, status, run_after)
		VALUES ($1, $2, NULLIF($3, ''), 'pending', $4)
	`, string(job.Type), job.Payload, key, runAt)
	return err
}

// Dequeue claims the next due job.
func (s *SQL) Dequeue(ctx context.Context) (*queue.Lease, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	var id int64
	var jobType string
	var payload []byte
	err = tx.QueryRow(ctx, `
		SELECT id, job_type, payload
		FROM queue_jobs
		WHERE status = 'pending' AND run_after <= now()
		ORDER BY run_after, id
		FOR UPDATE SKIP LOCKED
		LIMIT 1
	`).Scan(&id, &jobType, &payload)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `UPDATE queue_jobs SET status = 'processing', locked_at = now() WHERE id = $1`, id); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &queue.Lease{ID: id, Type: queue.Type(jobType), Payload: json.RawMessage(payload)}, nil
}

// Ack marks a job completed.
func (s *SQL) Ack(ctx context.Context, id int64) error {
	tag, err := s.pool.Exec(ctx, `UPDATE queue_jobs SET status = 'done', finished_at = now() WHERE id = $1 AND status = 'processing'`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("ack: no row for id %d", id)
	}
	return nil
}

// Nack returns a job to pending or leaves it failed (requeue false).
func (s *SQL) Nack(ctx context.Context, id int64, requeue bool) error {
	if requeue {
		_, err := s.pool.Exec(ctx, `
			UPDATE queue_jobs SET status = 'pending', locked_at = NULL, attempts = attempts + 1
			WHERE id = $1`, id)
		return err
	}
	_, err := s.pool.Exec(ctx, `UPDATE queue_jobs SET status = 'failed', finished_at = now() WHERE id = $1`, id)
	return err
}
