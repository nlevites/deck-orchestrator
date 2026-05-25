package outbox

import (
	"context"
	"encoding/json"
	"log/slog"
	"math/rand/v2"
	"time"

	"deck-fleet/backend/internal/executor/chaos"
	"deck-fleet/backend/internal/executor/client"
	"deck-fleet/backend/internal/executor/localstore"
)

// Config holds flusher backoff tuning. Chaos drop-delivery lives on *chaos.State.
type Config struct {
	Initial time.Duration
	Max     time.Duration
}

type Flusher struct {
	store  *localstore.Store
	client *client.Client
	cfg    Config
	chaos  *chaos.State
	logger *slog.Logger
}

type Deps struct {
	Store  *localstore.Store
	Client *client.Client
	Cfg    Config
	Chaos  *chaos.State
	Logger *slog.Logger
}

func New(d Deps) *Flusher {
	if d.Store == nil {
		panic("outbox.New: Store is required")
	}
	if d.Client == nil {
		panic("outbox.New: Client is required")
	}
	if d.Logger == nil {
		panic("outbox.New: Logger is required")
	}
	return &Flusher{
		store:  d.Store,
		client: d.Client,
		cfg:    d.Cfg,
		chaos:  d.Chaos,
		logger: d.Logger,
	}
}

func (f *Flusher) dropDelivery() bool {
	if f.chaos == nil {
		return false
	}
	return f.chaos.DropEvents()
}

func (f *Flusher) Run(ctx context.Context) {
	backoff := f.cfg.Initial
	if backoff <= 0 {
		backoff = time.Second
	}
	for {
		if err := ctx.Err(); err != nil {
			return
		}
		row, ok, err := f.store.NextOutbox(ctx)
		if err != nil {
			f.logger.Error("outbox: next", "err", err)
			f.sleep(ctx, backoff)
			continue
		}
		if !ok {
			backoff = f.cfg.Initial
			f.sleep(ctx, 200*time.Millisecond)
			continue
		}

		if f.dropDelivery() {
			f.logger.Info("outbox: chaos.drop_events on; pretending delivery failed", "seq", row.Seq)
			if bErr := f.store.BumpOutboxRetry(ctx, row.Seq, time.Now().UTC()); bErr != nil {
				f.logger.Error("outbox: bump retry", "seq", row.Seq, "err", bErr)
			}
			f.sleep(ctx, backoff)
			backoff = nextBackoff(backoff, f.cfg.Max)
			continue
		}

		now := time.Now().UTC()
		delivered, err := f.client.PostEvent(ctx, row.AttemptID, string(row.Kind), json.RawMessage(row.Payload), row.OccurredAt)
		if !delivered {
			// 404 UNKNOWN_ATTEMPT stays in the outbox (C3 fix); retry
			// rather than silently deleting the row.
			f.logger.Warn("outbox: deliver failed", "seq", row.Seq, "err", err)
			if bErr := f.store.BumpOutboxRetry(ctx, row.Seq, now); bErr != nil {
				f.logger.Error("outbox: bump retry", "seq", row.Seq, "err", bErr)
			}
			f.sleep(ctx, backoff)
			backoff = nextBackoff(backoff, f.cfg.Max)
			continue
		}
		if dErr := f.store.DeleteOutbox(ctx, row.Seq); dErr != nil {
			f.logger.Error("outbox: delete", "seq", row.Seq, "err", dErr)
		}
		backoff = f.cfg.Initial
	}
}

func (f *Flusher) sleep(ctx context.Context, d time.Duration) {
	if d <= 0 {
		return
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C:
	}
}

func nextBackoff(cur, max time.Duration) time.Duration {
	next := cur * 2
	if next > max {
		next = max
	}
	jitter := time.Duration(rand.Int64N(int64(next / 4)))
	return next - next/8 + jitter
}
