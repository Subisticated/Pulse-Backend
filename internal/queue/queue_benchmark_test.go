package queue

import (
	"context"
	"testing"
	"time"

	"pulse-backend/internal/models"
)

// BenchmarkQueueThroughput measures the pure enqueue performance of the buffered channel pipeline
func BenchmarkQueueThroughput(b *testing.B) {
	// Initialize the queue with a nil DB (highly optimized mock path)
	q := New(nil, func(ctx context.Context, event *models.LogEvent) {})
	defer q.Shutdown(1 * time.Second)

	logEvent := models.LogEvent{
		Endpoint:    "/api/v1/checkout",
		Method:      "POST",
		Status:      201,
		Latency:     45,
		Service:     "checkout",
		Environment: "production",
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			// Enqueue telemetry log. In case queue fills, spin briefly to allow background batcher to drain.
			for !q.Enqueue(logEvent) {
				time.Sleep(1 * time.Microsecond)
			}
		}
	})
}
