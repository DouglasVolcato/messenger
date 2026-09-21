package metrics

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"runtime"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

var (
	mu sync.RWMutex
	publishedByType = make(map[string]uint64)

	batchesTotal          atomic.Uint64
	processingErrorsTotal atomic.Uint64
	rabbitReconnectsTotal atomic.Uint64
	lastBatchSize         atomic.Int64
	processingCount       atomic.Uint64
	processingNanos       atomic.Uint64
)

func IncPublished(notificationType string) {
	mu.Lock()
	publishedByType[notificationType]++
	mu.Unlock()
}

func IncProcessingError() {
	processingErrorsTotal.Add(1)
}

func IncRabbitReconnect() {
	rabbitReconnectsTotal.Add(1)
}

func ObserveBatch(size int, elapsed time.Duration) {
	batchesTotal.Add(1)
	lastBatchSize.Store(int64(size))
	processingCount.Add(1)
	processingNanos.Add(uint64(elapsed.Nanoseconds()))
}

func Serve(ctx context.Context, address string, database *sql.DB) {
	server := &http.Server{
		Addr:              address,
		Handler:           Handler(database),
		ReadHeaderTimeout: 3 * time.Second,
	}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Printf("publisher metrics server: %v", err)
	}
}

func Handler(database *sql.DB) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")

		mu.RLock()
		types := make([]string, 0, len(publishedByType))
		for notificationType := range publishedByType {
			types = append(types, notificationType)
		}
		sort.Strings(types)
		fmt.Fprintln(w, "# HELP publisher_events_published_total RabbitMQ publisher-confirmed notification events.")
		fmt.Fprintln(w, "# TYPE publisher_events_published_total counter")
		for _, notificationType := range types {
			fmt.Fprintf(w, "publisher_events_published_total{type=%q} %d\n", notificationType, publishedByType[notificationType])
		}
		mu.RUnlock()

		fmt.Fprintln(w, "# HELP publisher_batches_total Outbox batches processed.")
		fmt.Fprintln(w, "# TYPE publisher_batches_total counter")
		fmt.Fprintf(w, "publisher_batches_total %d\n", batchesTotal.Load())

		fmt.Fprintln(w, "# HELP publisher_processing_errors_total Publisher processing errors.")
		fmt.Fprintln(w, "# TYPE publisher_processing_errors_total counter")
		fmt.Fprintf(w, "publisher_processing_errors_total %d\n", processingErrorsTotal.Load())

		fmt.Fprintln(w, "# HELP publisher_rabbitmq_reconnects_total RabbitMQ connection attempts that had to be retried.")
		fmt.Fprintln(w, "# TYPE publisher_rabbitmq_reconnects_total counter")
		fmt.Fprintf(w, "publisher_rabbitmq_reconnects_total %d\n", rabbitReconnectsTotal.Load())

		fmt.Fprintln(w, "# HELP publisher_last_batch_size Number of events in the latest outbox batch.")
		fmt.Fprintln(w, "# TYPE publisher_last_batch_size gauge")
		fmt.Fprintf(w, "publisher_last_batch_size %d\n", lastBatchSize.Load())

		count := processingCount.Load()
		fmt.Fprintln(w, "# HELP publisher_batch_processing_duration_seconds_sum Total time spent processing outbox batches.")
		fmt.Fprintln(w, "# TYPE publisher_batch_processing_duration_seconds_sum counter")
		fmt.Fprintf(w, "publisher_batch_processing_duration_seconds_sum %.9f\n", float64(processingNanos.Load())/float64(time.Second))
		fmt.Fprintln(w, "# HELP publisher_batch_processing_duration_seconds_count Number of measured outbox batches.")
		fmt.Fprintln(w, "# TYPE publisher_batch_processing_duration_seconds_count counter")
		fmt.Fprintf(w, "publisher_batch_processing_duration_seconds_count %d\n", count)

		if database != nil {
			var pending int64
			var oldestSeconds float64
			err := database.QueryRowContext(r.Context(), `
				SELECT COUNT(*),
				       COALESCE(EXTRACT(EPOCH FROM (NOW() - MIN(created_at))), 0)
				FROM user_notifications_outbox
				WHERE status = 'PENDING'`).Scan(&pending, &oldestSeconds)
			if err == nil {
				fmt.Fprintln(w, "# HELP publisher_outbox_pending_events Current pending notification outbox rows.")
				fmt.Fprintln(w, "# TYPE publisher_outbox_pending_events gauge")
				fmt.Fprintf(w, "publisher_outbox_pending_events %d\n", pending)
				fmt.Fprintln(w, "# HELP publisher_outbox_oldest_event_age_seconds Age of the oldest pending notification.")
				fmt.Fprintln(w, "# TYPE publisher_outbox_oldest_event_age_seconds gauge")
				fmt.Fprintf(w, "publisher_outbox_oldest_event_age_seconds %.6f\n", oldestSeconds)
				fmt.Fprintln(w, "publisher_outbox_query_up 1")
			} else {
				fmt.Fprintln(w, "publisher_outbox_query_up 0")
			}

			stats := database.Stats()
			fmt.Fprintf(w, "publisher_db_connections_open %d\n", stats.OpenConnections)
			fmt.Fprintf(w, "publisher_db_connections_in_use %d\n", stats.InUse)
			fmt.Fprintf(w, "publisher_db_connections_idle %d\n", stats.Idle)
			fmt.Fprintf(w, "publisher_db_wait_count_total %d\n", stats.WaitCount)
		}

		var memory runtime.MemStats
		runtime.ReadMemStats(&memory)
		fmt.Fprintf(w, "publisher_go_goroutines %d\n", runtime.NumGoroutine())
		fmt.Fprintf(w, "publisher_go_heap_alloc_bytes %d\n", memory.HeapAlloc)
		fmt.Fprintf(w, "publisher_go_heap_inuse_bytes %d\n", memory.HeapInuse)
		fmt.Fprintf(w, "publisher_go_heap_objects %d\n", memory.HeapObjects)
		fmt.Fprintf(w, "publisher_go_gc_cycles_total %d\n", memory.NumGC)
	})
}
