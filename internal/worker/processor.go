package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mastodon-site/activitypub-core/internal/config"
	"github.com/mastodon-site/activitypub-core/internal/fetch"
	"github.com/mastodon-site/activitypub-core/internal/inboxproc"
	"github.com/mastodon-site/activitypub-core/queue"
)

// ProcessLease runs one dequeued job (deliver_activity, process_inbox_activity, noop).
// httpClient is used for process_inbox_activity; when non-nil it is also used for deliver_activity,
// otherwise a fresh 60s client is created for delivery (matching apw behavior).
func ProcessLease(ctx context.Context, cfg *config.Config, pool *pgxpool.Pool, q queue.Backend, lease *queue.Lease, httpClient *http.Client) error {
	if lease == nil {
		return nil
	}
	switch lease.Type {
	case queue.TypeNoop:
		return nil
	case queue.TypeDeliverActivity:
		// Match historical apw behavior: delivery always uses a dedicated 60s client.
		return DeliverActivity(ctx, cfg, pool, fetch.NewHTTPClient(cfg, 60*time.Second), lease.Payload)
	case queue.TypeProcessInboxActivity:
		if pool == nil {
			return fmt.Errorf("process_inbox_activity requires AP_DATABASE_URL (postgres pool)")
		}
		var payload struct {
			ActivityDBID int64 `json:"activityDbId"`
		}
		if err := json.Unmarshal(lease.Payload, &payload); err != nil {
			return fmt.Errorf("process_inbox_activity payload: %w", err)
		}
		if payload.ActivityDBID < 1 {
			return fmt.Errorf("process_inbox_activity: activityDbId required")
		}
		hc := httpClient
		if hc == nil {
			hc = fetch.NewHTTPClient(cfg, 60*time.Second)
		}
		return inboxproc.ProcessInboxActivity(ctx, pool, q, cfg, hc, payload.ActivityDBID, nil)
	default:
		log.Printf("unknown job type %q — acknowledging", lease.Type)
		return nil
	}
}
