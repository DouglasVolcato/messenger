package metrics

import (
	"context"
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
	mu                  sync.RWMutex
	processedByPriority = make(map[string]uint64)
	failedByPriority    = make(map[string]uint64)

	sessionsResolvedTotal  atomic.Uint64
	deliveryRoutesTotal    atomic.Uint64
	deliveryFailuresTotal  atomic.Uint64
	staleReplicaRoutesTotal atomic.Uint64
	chatMemberCacheHitsTotal atomic.Uint64
	chatMemberCacheMissesTotal atomic.Uint64
	rabbitReconnectsTotal   atomic.Uint64
	processingCount         atomic.Uint64
	processingNanos         atomic.Uint64
	ready                   atomic.Bool
)

func IncProcessed(priority string) {
	mu.Lock()
	processedByPriority[priority]++
	mu.Unlock()
}

func IncFailed(priority string) {
	mu.Lock()
	failedByPriority[priority]++
	mu.Unlock()
}

func AddSessions(count int) {
	if count > 0 {
		sessionsResolvedTotal.Add(uint64(count))
	}
}

func IncDeliveryRoute() {
	deliveryRoutesTotal.Add(1)
}

func IncDeliveryFailure() {
	deliveryFailuresTotal.Add(1)
}

func IncStaleReplicaRoute() {
	staleReplicaRoutesTotal.Add(1)
}

func IncChatMemberCacheHit() {
	chatMemberCacheHitsTotal.Add(1)
}

func IncChatMemberCacheMiss() {
	chatMemberCacheMissesTotal.Add(1)
}

func IncRabbitReconnect() {
	rabbitReconnectsTotal.Add(1)
}

func SetReady(value bool) {
	ready.Store(value)
}

func ObserveProcessing(elapsed time.Duration) {
	processingCount.Add(1)
	processingNanos.Add(uint64(elapsed.Nanoseconds()))
}

func Serve(ctx context.Context, address string) {
	mux := http.NewServeMux()
	mux.Handle("GET /metrics", Handler())
	mux.HandleFunc("GET /livez", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, _ *http.Request) {
		if !ready.Load() {
			http.Error(w, "worker not ready", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})

	server := &http.Server{
		Addr:              address,
		Handler:           mux,
		ReadHeaderTimeout: 3 * time.Second,
	}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Printf("websocket worker metrics server: %v", err)
	}
}

func Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")

		mu.RLock()
		priorities := make(map[string]struct{}, len(processedByPriority)+len(failedByPriority))
		for priority := range processedByPriority {
			priorities[priority] = struct{}{}
		}
		for priority := range failedByPriority {
			priorities[priority] = struct{}{}
		}
		keys := make([]string, 0, len(priorities))
		for priority := range priorities {
			keys = append(keys, priority)
		}
		sort.Strings(keys)
		fmt.Fprintln(w, "# HELP websocket_worker_events_processed_total Notification events successfully routed by priority.")
		fmt.Fprintln(w, "# TYPE websocket_worker_events_processed_total counter")
		fmt.Fprintln(w, "# HELP websocket_worker_events_failed_total Notification routing failures by priority.")
		fmt.Fprintln(w, "# TYPE websocket_worker_events_failed_total counter")
		for _, priority := range keys {
			fmt.Fprintf(w, "websocket_worker_events_processed_total{priority=%q} %d\n", priority, processedByPriority[priority])
			fmt.Fprintf(w, "websocket_worker_events_failed_total{priority=%q} %d\n", priority, failedByPriority[priority])
		}
		mu.RUnlock()

		fmt.Fprintln(w, "# HELP websocket_worker_sessions_resolved_total Active WebSocket sessions resolved from Redis.")
		fmt.Fprintln(w, "# TYPE websocket_worker_sessions_resolved_total counter")
		fmt.Fprintf(w, "websocket_worker_sessions_resolved_total %d\n", sessionsResolvedTotal.Load())

		fmt.Fprintln(w, "# HELP websocket_worker_delivery_routes_total Per-replica RabbitMQ delivery batches published.")
		fmt.Fprintln(w, "# TYPE websocket_worker_delivery_routes_total counter")
		fmt.Fprintf(w, "websocket_worker_delivery_routes_total %d\n", deliveryRoutesTotal.Load())

		fmt.Fprintln(w, "# HELP websocket_worker_delivery_failures_total Failed per-replica RabbitMQ delivery publishes.")
		fmt.Fprintln(w, "# TYPE websocket_worker_delivery_failures_total counter")
		fmt.Fprintf(w, "websocket_worker_delivery_failures_total %d\n", deliveryFailuresTotal.Load())

		fmt.Fprintln(w, "# HELP websocket_worker_stale_replica_routes_total Redis sessions ignored because the target replica registration expired.")
		fmt.Fprintln(w, "# TYPE websocket_worker_stale_replica_routes_total counter")
		fmt.Fprintf(w, "websocket_worker_stale_replica_routes_total %d\n", staleReplicaRoutesTotal.Load())

		fmt.Fprintln(w, "# HELP websocket_worker_chat_member_cache_hits_total Chat membership cache hits.")
		fmt.Fprintln(w, "# TYPE websocket_worker_chat_member_cache_hits_total counter")
		fmt.Fprintf(w, "websocket_worker_chat_member_cache_hits_total %d\n", chatMemberCacheHitsTotal.Load())

		fmt.Fprintln(w, "# HELP websocket_worker_chat_member_cache_misses_total Chat membership cache misses.")
		fmt.Fprintln(w, "# TYPE websocket_worker_chat_member_cache_misses_total counter")
		fmt.Fprintf(w, "websocket_worker_chat_member_cache_misses_total %d\n", chatMemberCacheMissesTotal.Load())

		fmt.Fprintln(w, "# HELP websocket_worker_rabbitmq_reconnects_total RabbitMQ reconnect attempts.")
		fmt.Fprintln(w, "# TYPE websocket_worker_rabbitmq_reconnects_total counter")
		fmt.Fprintf(w, "websocket_worker_rabbitmq_reconnects_total %d\n", rabbitReconnectsTotal.Load())

		fmt.Fprintln(w, "# HELP websocket_worker_processing_duration_seconds_sum Total notification routing time.")
		fmt.Fprintln(w, "# TYPE websocket_worker_processing_duration_seconds_sum counter")
		fmt.Fprintf(w, "websocket_worker_processing_duration_seconds_sum %.9f\n", float64(processingNanos.Load())/float64(time.Second))
		fmt.Fprintln(w, "# HELP websocket_worker_processing_duration_seconds_count Number of measured notification routing operations.")
		fmt.Fprintln(w, "# TYPE websocket_worker_processing_duration_seconds_count counter")
		fmt.Fprintf(w, "websocket_worker_processing_duration_seconds_count %d\n", processingCount.Load())

		var memory runtime.MemStats
		runtime.ReadMemStats(&memory)
		fmt.Fprintf(w, "websocket_worker_go_goroutines %d\n", runtime.NumGoroutine())
		fmt.Fprintf(w, "websocket_worker_go_heap_alloc_bytes %d\n", memory.HeapAlloc)
		fmt.Fprintf(w, "websocket_worker_go_heap_inuse_bytes %d\n", memory.HeapInuse)
		fmt.Fprintf(w, "websocket_worker_go_heap_objects %d\n", memory.HeapObjects)
		fmt.Fprintf(w, "websocket_worker_go_gc_cycles_total %d\n", memory.NumGC)
	})
}
