package worker

import (
	"context"
	"errors"
	"log"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/mastodon-site/activitypub-core/internal/config"
	"github.com/mastodon-site/activitypub-core/queue"
)

var deliveryNackOutcomes = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Namespace: "activitypub_core",
		Subsystem: "worker",
		Name:      "delivery_nack_total",
		Help:      "Delivery job nack outcomes (SQL queue backoff).",
	},
	[]string{"outcome"},
)

// HandleDeliveryJobFailure applies backoff, permanent 4xx handling, or max-attempt dead-letter when the SQL queue supports it.
func HandleDeliveryJobFailure(ctx context.Context, q queue.Backend, lease *queue.Lease, jobErr error, cfg *config.Config) {
	if lease == nil || jobErr == nil {
		return
	}
	lastErr := jobErr.Error()
	sq, ok := q.(queue.SQLDelayedNack)
	if !ok {
		_ = q.Nack(ctx, lease.ID, true)
		return
	}
	max := cfg.DeliveryMaxAttempts
	if max < 1 {
		max = 25
	}
	var httpErr *HTTPDeliveryError
	permanent4xx := errors.As(jobErr, &httpErr) && deliveryPermanent4xx(httpErr.StatusCode)
	if permanent4xx {
		if err := sq.NackSchedule(ctx, lease.ID, true, time.Time{}, lastErr); err != nil {
			log.Printf("delivery nack permanent (4xx): %v", err)
		}
		deliveryNackOutcomes.WithLabelValues("permanent_4xx").Inc()
		return
	}
	if lease.Attempts+1 >= max {
		if err := sq.NackSchedule(ctx, lease.ID, true, time.Time{}, lastErr); err != nil {
			log.Printf("delivery nack permanent (max attempts): %v", err)
		}
		deliveryNackOutcomes.WithLabelValues("permanent_max_attempts").Inc()
		return
	}
	next := nextDeliveryRunAfter(lease.Attempts, cfg)
	if err := sq.NackSchedule(ctx, lease.ID, false, next, lastErr); err != nil {
		log.Printf("delivery nack retry: %v", err)
	}
	deliveryNackOutcomes.WithLabelValues("retry_scheduled").Inc()
}

func deliveryPermanent4xx(code int) bool {
	switch code {
	case 400, 401, 403, 404, 405, 410, 422:
		return true
	default:
		return false
	}
}

func nextDeliveryRunAfter(priorFailures int, cfg *config.Config) time.Time {
	initSec := cfg.DeliveryBackoffInitialSec
	if initSec < 1 {
		initSec = 30
	}
	maxSec := cfg.DeliveryBackoffMaxSec
	if maxSec < initSec {
		maxSec = initSec
	}
	d := time.Duration(initSec) * time.Second
	maxD := time.Duration(maxSec) * time.Second
	exp := priorFailures
	if exp > 20 {
		exp = 20
	}
	for i := 0; i < exp; i++ {
		if d >= maxD {
			d = maxD
			break
		}
		d *= 2
		if d > maxD {
			d = maxD
		}
	}
	return time.Now().UTC().Add(d)
}
