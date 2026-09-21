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
	mu sync.RWMutex
	processedByPriority = make(map[string]uint64)
	failedByPriority    = make(map[string]uint64)

	sessionsResolvedTotal atomic.Uint64
	grpcCallsTotal        atomic.Uint64
	grpcFailuresTotal     atomic.Uint64
	rabbitReconnectsTotal atomic.Uint64
	processingCount       atomic.Uint64
	processingNanos       atomic.Uint64
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

func IncGRPCCall() {
	grpcCallsTotal.Add(1)
}

func IncGRPCFailure() {
	grpcFailuresTotal.Add(1)
}

func IncRabbitReconnect() {
	rabbitReconnectsTotal.Add(1)
}

func ObserveProcessing(elapsed time.Duration) {
	processingCount.Add(1)
	processingNanos.Add(uint64(elapsed.Nanoseconds()))
}

func Serve(ctx context.Context, address string) {
	server := &http.Server{
		Addr:              address,
		Handler:           Handler(),
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
		fmt.Fprintln(w, "# HELP websocket_worker_events_processed_total RabbitMQ events successfully processed by priority.")
		fmt.Fprintln(w, "# TYPE websocket_worker_events_processed_total counter")
		fmt.Fprintln(w, "# HELP websocket_worker_events_failed_total RabbitMQ event processing failures by priority.")
		fmt.Fprintln(w, "# TYPE websocket_worker_events_failed_total counter")
		for _, priority := range keys {
			fmt.Fprintf(w, "websocket_worker_events_processed_total{priority=%q} %d\n", priority, processedByPriority[priority])
			fmt.Fprintf(w, "websocket_worker_events_failed_total{priority=%q} %d\n", priority, failedByPriority[priority])
		}
		mu.RUnlock()

		fmt.Fprintln(w, "# HELP websocket_worker_sessions_resolved_total Live WebSocket sessions resolved from Redis.")
		fmt.Fprintln(w, "# TYPE websocket_worker_sessions_resolved_total counter")
		fmt.Fprintf(w, "websocket_worker_sessions_resolved_total %d\n", sessionsResolvedTotal.Load())

		fmt.Fprintln(w, "# HELP websocket_worker_grpc_calls_total Delivery calls issued to WebSocket replicas.")
		fmt.Fprintln(w, "# TYPE websocket_worker_grpc_calls_total counter")
		fmt.Fprintf(w, "websocket_worker_grpc_calls_total %d\n", grpcCallsTotal.Load())

		fmt.Fprintln(w, "# HELP websocket_worker_grpc_failures_total Failed gRPC delivery calls.")
		fmt.Fprintln(w, "# TYPE websocket_worker_grpc_failures_total counter")
		fmt.Fprintf(w, "websocket_worker_grpc_failures_total %d\n", grpcFailuresTotal.Load())

		fmt.Fprintln(w, "# HELP websocket_worker_rabbitmq_reconnects_total RabbitMQ reconnect attempts.")
		fmt.Fprintln(w, "# TYPE websocket_worker_rabbitmq_reconnects_total counter")
		fmt.Fprintf(w, "websocket_worker_rabbitmq_reconnects_total %d\n", rabbitReconnectsTotal.Load())

		fmt.Fprintln(w, "# HELP websocket_worker_processing_duration_seconds_sum Total event processing time.")
		fmt.Fprintln(w, "# TYPE websocket_worker_processing_duration_seconds_sum counter")
		fmt.Fprintf(w, "websocket_worker_processing_duration_seconds_sum %.9f\n", float64(processingNanos.Load())/float64(time.Second))
		fmt.Fprintln(w, "# HELP websocket_worker_processing_duration_seconds_count Number of measured event processing operations.")
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
