package redisqueue

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/mastodon-site/activitypub-core/queue"
)

// Redis implements a simple list-based queue (RPUSH / BRPOP).
// Job IDs are synthetic (timestamp + random) stored in payload for Ack placeholder.
const listKey = "activitypub_core:queue"

// Redis backend (MVP: brpoplpush-style with single list; Ack is no-op best-effort).
type Redis struct {
	client *redis.Client
}

// New creates a Redis queue.
func New(client *redis.Client) *Redis {
	return &Redis{client: client}
}

type redisJob struct {
	ID      int64           `json:"id"`
	Type    queue.Type      `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

var redisID int64 // process-local fallback for lease id

// Enqueue pushes JSON job onto list.
func (r *Redis) Enqueue(ctx context.Context, job queue.Job) error {
	redisID++
	rj := redisJob{ID: redisID, Type: job.Type, Payload: job.Payload}
	b, err := json.Marshal(rj)
	if err != nil {
		return err
	}
	return r.client.RPush(ctx, listKey, b).Err()
}

// Dequeue uses BRPOP with timeout.
func (r *Redis) Dequeue(ctx context.Context) (*queue.Lease, error) {
	res, err := r.client.BRPop(ctx, time.Second, listKey).Result()
	if err == redis.Nil {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if len(res) != 2 {
		return nil, nil
	}
	var rj redisJob
	if err := json.Unmarshal([]byte(res[1]), &rj); err != nil {
		return nil, err
	}
	return &queue.Lease{ID: rj.ID, Type: rj.Type, Payload: rj.Payload}, nil
}

// Ack is a no-op for list MVP (job already removed by BRPOP).
func (r *Redis) Ack(ctx context.Context, id int64) error {
	_ = ctx
	_ = id
	return nil
}

// Nack cannot re-inject without durable store; log pattern: re-enqueue manually if needed.
func (r *Redis) Nack(ctx context.Context, id int64, requeue bool) error {
	if requeue {
		return fmt.Errorf("redisqueue: Nack requeue not supported in MVP (id=%s)", strconv.FormatInt(id, 10))
	}
	return nil
}
