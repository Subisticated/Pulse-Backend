// Package queue implements a high-throughput async log ingestion pipeline.
//
// Architecture:
//
//	HTTP Handler → Enqueue (non-blocking) → buffered channel
//	                                          ↓
//	                                    Batch Worker
//	                                          ↓
//	                                    InsertMany (50 logs or 500ms timeout)
//	                                          ↓
//	                                    Incident Detector (async per log)
package queue

import (
	"context"
	"time"

	"pulse-backend/internal/models"

	"github.com/rs/zerolog/log"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
)

const (
	// QueueSize is the maximum number of log events that can be buffered
	QueueSize = 8192

	// BatchSize is the number of logs to accumulate before flushing to Mongo
	BatchSize = 50

	// FlushInterval is the maximum time to wait before flushing a partial batch
	FlushInterval = 500 * time.Millisecond
)

// AnalyzerFunc is the signature for the anomaly detector callback
type AnalyzerFunc func(ctx context.Context, event *models.LogEvent)

// LogQueue is a lock-free async batching pipeline for telemetry log ingestion
type LogQueue struct {
	ch       chan models.LogEvent
	done     chan struct{} // closed by Shutdown() to signal worker to stop
	flushed  chan struct{} // closed by worker when fully drained
	db       *mongo.Database
	analyzer AnalyzerFunc
}

// New creates and starts a LogQueue. Call Shutdown() on graceful exit.
func New(db *mongo.Database, analyzer AnalyzerFunc) *LogQueue {
	q := &LogQueue{
		ch:       make(chan models.LogEvent, QueueSize),
		done:     make(chan struct{}),
		flushed:  make(chan struct{}),
		db:       db,
		analyzer: analyzer,
	}
	go q.batchWorker()
	return q
}

// Enqueue adds a log event to the async queue.
// Returns false if the queue is full — caller gets HTTP 503.
func (q *LogQueue) Enqueue(event models.LogEvent) bool {
	if event.ID.IsZero() {
		event.ID = primitive.NewObjectID()
	}
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}
	select {
	case q.ch <- event:
		return true
	default:
		log.Warn().Str("service", event.Service).Msg("⚠️  Log queue full — dropping event")
		return false
	}
}

// Len returns the current number of events waiting in the queue
func (q *LogQueue) Len() int {
	return len(q.ch)
}

// Shutdown signals the batch worker to drain all remaining events and exit.
// Blocks until the worker finishes — safe to call from os.Signal handler.
func (q *LogQueue) Shutdown(timeout time.Duration) {
	log.Info().Int("queue_depth", len(q.ch)).Msg("🔄 Flushing log queue before shutdown...")
	close(q.done) // signal worker to stop accepting new events and drain

	select {
	case <-q.flushed:
		log.Info().Msg("✅ Log queue drained successfully")
	case <-time.After(timeout):
		log.Warn().Int("remaining", len(q.ch)).Msg("⚠️  Queue drain timed out — some logs may be lost")
	}
}

// batchWorker drains the queue, accumulates batches, and flushes via InsertMany
func (q *LogQueue) batchWorker() {
	defer close(q.flushed) // signal Shutdown() when we exit

	batch := make([]interface{}, 0, BatchSize)
	refs := make([]models.LogEvent, 0, BatchSize)
	ticker := time.NewTicker(FlushInterval)
	defer ticker.Stop()

	flush := func() {
		if len(batch) == 0 {
			return
		}

		if q.db != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			col := q.db.Collection("logs")
			_, err := col.InsertMany(ctx, batch)
			if err != nil {
				log.Error().Err(err).Int("batch_size", len(batch)).Msg("❌ Batch insert failed")
			} else {
				log.Debug().Int("batch_size", len(batch)).Msg("💾 Batch flushed to MongoDB")
			}
		} else {
			log.Debug().Int("batch_size", len(batch)).Msg("💾 Batch processed (nil DB)")
		}

		// Fire incident detection per log asynchronously
		if q.analyzer != nil {
			for _, ev := range refs {
				e := ev
				go func() {
					detCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
					defer cancel()
					q.analyzer(detCtx, &e)
				}()
			}
		}

		batch = batch[:0]
		refs = refs[:0]
	}

	for {
		select {
		case event, ok := <-q.ch:
			if !ok {
				// Channel closed externally (shouldn't happen but handle gracefully)
				flush()
				return
			}
			batch = append(batch, event)
			refs = append(refs, event)
			if len(batch) >= BatchSize {
				flush()
				ticker.Reset(FlushInterval)
			}

		case <-ticker.C:
			flush()

		case <-q.done:
			// Drain everything remaining in the channel before exiting
			draining := true
			for draining {
				select {
				case event := <-q.ch:
					batch = append(batch, event)
					refs = append(refs, event)
					if len(batch) >= BatchSize {
						flush()
					}
				default:
					draining = false
				}
			}
			flush() // final flush of partial batch
			return
		}
	}
}
