package metrics

import (
	"database/sql"
	"fmt"
	"net/http"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

var latencyBuckets = []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2, 5, 10}

type histogram struct {
	Count   uint64
	Sum     float64
	Buckets []uint64
}

type collector struct {
	mu sync.RWMutex

	requests  map[string]uint64
	durations map[string]*histogram
	inFlight  atomic.Int64
}

var defaultCollector = &collector{
	requests:  make(map[string]uint64),
	durations: make(map[string]*histogram),
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *statusWriter) Write(body []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(body)
}

func Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defaultCollector.inFlight.Add(1)
		defer defaultCollector.inFlight.Add(-1)

		started := time.Now()
		recorder := &statusWriter{ResponseWriter: w}
		next.ServeHTTP(recorder, r)

		status := recorder.status
		if status == 0 {
			status = http.StatusOK
		}
		route := normalizedPattern(r.Pattern)
		defaultCollector.observe(r.Method, route, status, time.Since(started))
	})
}

func Handler(database *sql.DB) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		defaultCollector.write(w, database)
	})
}

func (c *collector) observe(method, route string, status int, elapsed time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	requestKey := method + "|" + route + "|" + strconv.Itoa(status)
	c.requests[requestKey]++

	durationKey := method + "|" + route
	h := c.durations[durationKey]
	if h == nil {
		h = &histogram{Buckets: make([]uint64, len(latencyBuckets))}
		c.durations[durationKey] = h
	}
	seconds := elapsed.Seconds()
	h.Count++
	h.Sum += seconds
	for index, bucket := range latencyBuckets {
		if seconds <= bucket {
			h.Buckets[index]++
		}
	}
}

func (c *collector) write(w http.ResponseWriter, database *sql.DB) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	fmt.Fprintln(w, "# HELP server_http_requests_in_flight Requests currently being handled by this replica.")
	fmt.Fprintln(w, "# TYPE server_http_requests_in_flight gauge")
	fmt.Fprintf(w, "server_http_requests_in_flight %d\n", c.inFlight.Load())

	fmt.Fprintln(w, "# HELP server_http_requests_total HTTP requests handled by route and status.")
	fmt.Fprintln(w, "# TYPE server_http_requests_total counter")
	for _, key := range sortedKeys(c.requests) {
		parts := strings.SplitN(key, "|", 3)
		fmt.Fprintf(w, "server_http_requests_total{method=%q,route=%q,status=%q} %d\n",
			escapeLabel(parts[0]), escapeLabel(parts[1]), escapeLabel(parts[2]), c.requests[key])
	}

	fmt.Fprintln(w, "# HELP server_http_request_duration_seconds HTTP request latency by normalized route.")
	fmt.Fprintln(w, "# TYPE server_http_request_duration_seconds histogram")
	durationKeys := make([]string, 0, len(c.durations))
	for key := range c.durations {
		durationKeys = append(durationKeys, key)
	}
	sort.Strings(durationKeys)
	for _, key := range durationKeys {
		parts := strings.SplitN(key, "|", 2)
		h := c.durations[key]
		for index, bucket := range latencyBuckets {
			fmt.Fprintf(w, "server_http_request_duration_seconds_bucket{method=%q,route=%q,le=%q} %d\n",
				escapeLabel(parts[0]), escapeLabel(parts[1]), strconv.FormatFloat(bucket, 'f', -1, 64), h.Buckets[index])
		}
		fmt.Fprintf(w, "server_http_request_duration_seconds_bucket{method=%q,route=%q,le=\"+Inf\"} %d\n",
			escapeLabel(parts[0]), escapeLabel(parts[1]), h.Count)
		fmt.Fprintf(w, "server_http_request_duration_seconds_sum{method=%q,route=%q} %.9f\n",
			escapeLabel(parts[0]), escapeLabel(parts[1]), h.Sum)
		fmt.Fprintf(w, "server_http_request_duration_seconds_count{method=%q,route=%q} %d\n",
			escapeLabel(parts[0]), escapeLabel(parts[1]), h.Count)
	}

	if database != nil {
		stats := database.Stats()
		fmt.Fprintln(w, "# HELP server_db_connections_open Open database connections.")
		fmt.Fprintln(w, "# TYPE server_db_connections_open gauge")
		fmt.Fprintf(w, "server_db_connections_open %d\n", stats.OpenConnections)
		fmt.Fprintln(w, "# HELP server_db_connections_in_use Database connections currently in use.")
		fmt.Fprintln(w, "# TYPE server_db_connections_in_use gauge")
		fmt.Fprintf(w, "server_db_connections_in_use %d\n", stats.InUse)
		fmt.Fprintln(w, "# HELP server_db_connections_idle Idle database connections.")
		fmt.Fprintln(w, "# TYPE server_db_connections_idle gauge")
		fmt.Fprintf(w, "server_db_connections_idle %d\n", stats.Idle)
		fmt.Fprintln(w, "# HELP server_db_wait_count_total Total waits for a database connection.")
		fmt.Fprintln(w, "# TYPE server_db_wait_count_total counter")
		fmt.Fprintf(w, "server_db_wait_count_total %d\n", stats.WaitCount)
		fmt.Fprintln(w, "# HELP server_db_wait_duration_seconds Total time blocked waiting for a database connection.")
		fmt.Fprintln(w, "# TYPE server_db_wait_duration_seconds counter")
		fmt.Fprintf(w, "server_db_wait_duration_seconds %.9f\n", stats.WaitDuration.Seconds())
	}

	writeRuntime(w, "server")
}

func writeRuntime(w http.ResponseWriter, prefix string) {
	var memory runtime.MemStats
	runtime.ReadMemStats(&memory)
	fmt.Fprintf(w, "%s_go_goroutines %d\n", prefix, runtime.NumGoroutine())
	fmt.Fprintf(w, "%s_go_heap_alloc_bytes %d\n", prefix, memory.HeapAlloc)
	fmt.Fprintf(w, "%s_go_heap_inuse_bytes %d\n", prefix, memory.HeapInuse)
	fmt.Fprintf(w, "%s_go_heap_objects %d\n", prefix, memory.HeapObjects)
	fmt.Fprintf(w, "%s_go_gc_cycles_total %d\n", prefix, memory.NumGC)
}

func normalizedPattern(pattern string) string {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return "unmatched"
	}
	if index := strings.IndexByte(pattern, ' '); index >= 0 {
		pattern = strings.TrimSpace(pattern[index+1:])
	}
	return pattern
}

func sortedKeys(values map[string]uint64) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func escapeLabel(value string) string {
	value = strings.ReplaceAll(value, "\\", "\\\\")
	value = strings.ReplaceAll(value, "\n", "\\n")
	value = strings.ReplaceAll(value, "\"", "\\\"")
	return value
}
