// Package prune runs a periodic background sweeper that enforces the
// retention limits configured under config.Limits: a max-row cap on the
// messages and calls tables, and an optional TTL on both. Pruning is
// non-destructive of audit data — callback_logs and tag rows are not
// touched (the latter cascade via FK when a parent row goes away).
//
// The pruner runs on a 60-second tick by default. A first sweep runs
// immediately when Run starts so an over-capacity DB at startup gets
// trimmed without a 60s wait.
package prune

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/notfoundsam/sms-mock-server/app/clock"
	"github.com/notfoundsam/sms-mock-server/app/config"
	"github.com/notfoundsam/sms-mock-server/app/storage"
)

// DefaultInterval is how often the pruner runs in production.
const DefaultInterval = 60 * time.Second

// Pruner enforces config.Limits against a storage.Store.
type Pruner struct {
	store    storage.Store
	logger   *slog.Logger
	limits   config.Limits
	clock    clock.Clock
	interval time.Duration
	done     chan struct{} // closed when Run exits
}

// New returns a Pruner, or nil if every limit is disabled (no caps and
// no TTL). Callers can use that nil result to skip wiring entirely:
// no goroutine starts, no log noise, no DB queries.
func New(store storage.Store, logger *slog.Logger, limits config.Limits, clk clock.Clock) *Pruner {
	if limits.MaxMessages <= 0 && limits.MaxCalls <= 0 && limits.MaxAge <= 0 {
		return nil
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Pruner{
		store:    store,
		logger:   logger,
		limits:   limits,
		clock:    clk,
		interval: DefaultInterval,
		done:     make(chan struct{}),
	}
}

// SetInterval overrides DefaultInterval. Tests use this to drive the loop
// quickly; production callers leave the default.
func (p *Pruner) SetInterval(d time.Duration) {
	if d > 0 {
		p.interval = d
	}
}

// Run blocks until ctx is canceled. Performs an immediate first pass,
// then ticks every interval. Returns the wrapped ctx.Err on shutdown.
//
// Run is goroutine-safe to call once per Pruner. Callers that need to
// join the goroutine on shutdown should pair Run with Wait — see main.go.
func (p *Pruner) Run(ctx context.Context) error {
	defer close(p.done)
	p.PruneOnce(ctx)

	t := time.NewTicker(p.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("pruner stopped: %w", ctx.Err())
		case <-t.C:
			p.PruneOnce(ctx)
		}
	}
}

// Wait blocks until the Run goroutine has exited, or ctx fires. Returns
// nil on clean exit, ctx.Err() on timeout. Safe to call before Run starts
// (returns ctx.Err immediately if Run never runs); intended for the
// shutdown sequence in main.go after rootCancel().
func (p *Pruner) Wait(ctx context.Context) error {
	select {
	case <-p.done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("pruner wait: %w", ctx.Err())
	}
}

// PruneOnce runs cap-enforcement for both tables, then TTL enforcement,
// logging any non-zero deletion counts. Errors are logged at WARN; they
// don't abort the loop because a transient SQLite contention error
// shouldn't take down a long-running sweeper.
func (p *Pruner) PruneOnce(ctx context.Context) {
	if p.limits.MaxMessages > 0 {
		if n, err := p.store.PruneMessagesByCount(ctx, p.limits.MaxMessages); err != nil {
			p.logger.Warn("prune messages by count failed", "error", err)
		} else if n > 0 {
			p.logger.Info("pruned messages by count", "deleted", n, "cap", p.limits.MaxMessages)
		}
	}
	if p.limits.MaxCalls > 0 {
		if n, err := p.store.PruneCallsByCount(ctx, p.limits.MaxCalls); err != nil {
			p.logger.Warn("prune calls by count failed", "error", err)
		} else if n > 0 {
			p.logger.Info("pruned calls by count", "deleted", n, "cap", p.limits.MaxCalls)
		}
	}
	if p.limits.MaxAge > 0 {
		cutoff := p.clock.Now().Add(-p.limits.MaxAge)
		if n, err := p.store.PruneMessagesByAge(ctx, cutoff); err != nil {
			p.logger.Warn("prune messages by age failed", "error", err)
		} else if n > 0 {
			p.logger.Info("pruned messages by age", "deleted", n, "cutoff", cutoff)
		}
		if n, err := p.store.PruneCallsByAge(ctx, cutoff); err != nil {
			p.logger.Warn("prune calls by age failed", "error", err)
		} else if n > 0 {
			p.logger.Info("pruned calls by age", "deleted", n, "cutoff", cutoff)
		}
	}
}
