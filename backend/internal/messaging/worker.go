package messaging

import (
	"context"
	"log/slog"
	"time"
)

const (
	workerBatchSize   = 10          // entries claimed per tick
	workerPollInterval = 15 * time.Second // how often to check for new entries
	workerMaxAttempts = 5           // permanent failure after this many tries
)

// Worker polls the notification_outbox table and delivers pending entries.
// It runs until the context is cancelled (typically on server shutdown).
// Start it in a goroutine: go worker.Run(ctx).
type Worker struct {
	outbox  *OutboxRepository
	svc     *NotificationService
	logger  *slog.Logger
}

func NewWorker(outbox *OutboxRepository, svc *NotificationService, logger *slog.Logger) *Worker {
	return &Worker{outbox: outbox, svc: svc, logger: logger}
}

// Run starts the poll loop. Blocks until ctx is cancelled.
func (w *Worker) Run(ctx context.Context) {
	w.logger.Info("notification worker started", "poll_interval", workerPollInterval)

	ticker := time.NewTicker(workerPollInterval)
	defer ticker.Stop()

	// Process immediately on startup so notifications aren't delayed by the first tick.
	w.processBatch(ctx)

	for {
		select {
		case <-ctx.Done():
			w.logger.Info("notification worker stopped")
			return
		case <-ticker.C:
			w.processBatch(ctx)
		}
	}
}

func (w *Worker) processBatch(ctx context.Context) {
	entries, err := w.outbox.ClaimBatch(ctx, workerBatchSize)
	if err != nil {
		w.logger.ErrorContext(ctx, "outbox: claim batch failed", "error", err)
		return
	}

	if len(entries) == 0 {
		return
	}

	w.logger.InfoContext(ctx, "outbox: processing batch", "count", len(entries))

	for _, entry := range entries {
		if err := w.svc.Deliver(ctx, entry); err != nil {
			w.logger.WarnContext(ctx, "outbox: delivery failed",
				"id", entry.ID,
				"event_type", entry.EventType,
				"attempts", entry.Attempts,
				"error", err,
			)
			if markErr := w.outbox.MarkFailed(ctx, entry.ID, err.Error(), workerMaxAttempts); markErr != nil {
				w.logger.ErrorContext(ctx, "outbox: mark failed error", "id", entry.ID, "error", markErr)
			}
			continue
		}

		if markErr := w.outbox.MarkDone(ctx, entry.ID); markErr != nil {
			w.logger.ErrorContext(ctx, "outbox: mark done error", "id", entry.ID, "error", markErr)
		} else {
			w.logger.InfoContext(ctx, "outbox: delivered",
				"id", entry.ID,
				"event_type", entry.EventType,
			)
		}
	}
}
