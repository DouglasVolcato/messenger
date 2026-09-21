package tester

import (
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

var durationBuckets = []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2, 5, 10}

type histogram struct {
	Count   uint64
	Sum     float64
	Buckets []uint64
}

type Metrics struct {
	mu sync.RWMutex

	requests  map[string]uint64
	durations map[string]*histogram
	errors    map[string]uint64
	messages  map[string]uint64

	activeUsers       int
	activeWebSockets  int
	webSocketsTotal   uint64
	webSocketFailures uint64
	webSocketReceived uint64
	targetRPS         int
	phase             int
}

func NewMetrics() *Metrics {
	return &Metrics{
		requests:  make(map[string]uint64),
		durations: make(map[string]*histogram),
		errors:    make(map[string]uint64),
		messages:  make(map[string]uint64),
	}
}

func (m *Metrics) ObserveHTTP(operation string, status int, elapsed time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()

	statusLabel := strconv.Itoa(status)
	if status == 0 {
		statusLabel = "error"
	}
	key := operation + "|" + statusLabel
	m.requests[key]++

	h := m.durations[operation]
	if h == nil {
		h = &histogram{Buckets: make([]uint64, len(durationBuckets))}
		m.durations[operation] = h
	}
	seconds := elapsed.Seconds()
	h.Count++
	h.Sum += seconds
	for i, bucket := range durationBuckets {
		if seconds <= bucket {
			h.Buckets[i]++
		}
	}
}

func (m *Metrics) IncError(kind string) {
	m.mu.Lock()
	m.errors[kind]++
	m.mu.Unlock()
}

func (m *Metrics) IncGeneratedMessage(kind string) {
	m.mu.Lock()
	m.messages[kind]++
	m.mu.Unlock()
}

func (m *Metrics) SetActiveUsers(value int) {
	m.mu.Lock()
	m.activeUsers = value
	m.mu.Unlock()
}

func (m *Metrics) SetActiveWebSockets(value int) {
	m.mu.Lock()
	m.activeWebSockets = value
	m.mu.Unlock()
}

func (m *Metrics) AdjustActiveWebSockets(delta int) {
	m.mu.Lock()
	m.activeWebSockets += delta
	if m.activeWebSockets < 0 {
		m.activeWebSockets = 0
	}
	m.mu.Unlock()
}

func (m *Metrics) IncWebSocketTotal() {
	m.mu.Lock()
	m.webSocketsTotal++
	m.mu.Unlock()
}

func (m *Metrics) IncWebSocketFailure() {
	m.mu.Lock()
	m.webSocketFailures++
	m.mu.Unlock()
}

func (m *Metrics) IncWebSocketReceived() {
	m.mu.Lock()
	m.webSocketReceived++
	m.mu.Unlock()
}

func (m *Metrics) SetPhase(phase, targetRPS int) {
	m.mu.Lock()
	m.phase = phase
	m.targetRPS = targetRPS
	m.mu.Unlock()
}

func (m *Metrics) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		m.mu.RLock()
		defer m.mu.RUnlock()

		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")

		fmt.Fprintln(w, "# HELP load_tester_active_users Number of generated users currently available to the workload.")
		fmt.Fprintln(w, "# TYPE load_tester_active_users gauge")
		fmt.Fprintf(w, "load_tester_active_users %d\n", m.activeUsers)

		fmt.Fprintln(w, "# HELP load_tester_websocket_connections_active WebSocket connections currently held open by the tester.")
		fmt.Fprintln(w, "# TYPE load_tester_websocket_connections_active gauge")
		fmt.Fprintf(w, "load_tester_websocket_connections_active %d\n", m.activeWebSockets)

		fmt.Fprintln(w, "# HELP load_tester_websocket_connections_total WebSocket connections successfully opened by the tester.")
		fmt.Fprintln(w, "# TYPE load_tester_websocket_connections_total counter")
		fmt.Fprintf(w, "load_tester_websocket_connections_total %d\n", m.webSocketsTotal)

		fmt.Fprintln(w, "# HELP load_tester_websocket_connection_failures_total Failed WebSocket connection attempts.")
		fmt.Fprintln(w, "# TYPE load_tester_websocket_connection_failures_total counter")
		fmt.Fprintf(w, "load_tester_websocket_connection_failures_total %d\n", m.webSocketFailures)

		fmt.Fprintln(w, "# HELP load_tester_websocket_messages_received_total Messages received over tester WebSocket connections.")
		fmt.Fprintln(w, "# TYPE load_tester_websocket_messages_received_total counter")
		fmt.Fprintf(w, "load_tester_websocket_messages_received_total %d\n", m.webSocketReceived)

		fmt.Fprintln(w, "# HELP load_tester_target_rps Configured request rate for the current phase.")
		fmt.Fprintln(w, "# TYPE load_tester_target_rps gauge")
		fmt.Fprintf(w, "load_tester_target_rps %d\n", m.targetRPS)

		fmt.Fprintln(w, "# HELP load_tester_phase Current zero-based load-test phase.")
		fmt.Fprintln(w, "# TYPE load_tester_phase gauge")
		fmt.Fprintf(w, "load_tester_phase %d\n", m.phase)

		fmt.Fprintln(w, "# HELP load_tester_http_requests_total HTTP operations emitted by the load tester.")
		fmt.Fprintln(w, "# TYPE load_tester_http_requests_total counter")
		requestKeys := sortedKeys(m.requests)
		for _, key := range requestKeys {
			parts := strings.SplitN(key, "|", 2)
			fmt.Fprintf(w, "load_tester_http_requests_total{operation=%q,status=%q} %d\n",
				escapeLabel(parts[0]), escapeLabel(parts[1]), m.requests[key])
		}

		fmt.Fprintln(w, "# HELP load_tester_http_request_duration_seconds HTTP operation latency measured by the load tester.")
		fmt.Fprintln(w, "# TYPE load_tester_http_request_duration_seconds histogram")
		durationKeys := make([]string, 0, len(m.durations))
		for key := range m.durations {
			durationKeys = append(durationKeys, key)
		}
		sort.Strings(durationKeys)
		for _, operation := range durationKeys {
			h := m.durations[operation]
			for i, bucket := range durationBuckets {
				fmt.Fprintf(w, "load_tester_http_request_duration_seconds_bucket{operation=%q,le=%q} %d\n",
					escapeLabel(operation), strconv.FormatFloat(bucket, 'f', -1, 64), h.Buckets[i])
			}
			fmt.Fprintf(w, "load_tester_http_request_duration_seconds_bucket{operation=%q,le=\"+Inf\"} %d\n",
				escapeLabel(operation), h.Count)
			fmt.Fprintf(w, "load_tester_http_request_duration_seconds_sum{operation=%q} %.9f\n", escapeLabel(operation), h.Sum)
			fmt.Fprintf(w, "load_tester_http_request_duration_seconds_count{operation=%q} %d\n", escapeLabel(operation), h.Count)
		}

		fmt.Fprintln(w, "# HELP load_tester_generated_messages_total Messages successfully generated by type.")
		fmt.Fprintln(w, "# TYPE load_tester_generated_messages_total counter")
		for _, kind := range sortedKeys(m.messages) {
			fmt.Fprintf(w, "load_tester_generated_messages_total{type=%q} %d\n", escapeLabel(kind), m.messages[kind])
		}

		fmt.Fprintln(w, "# HELP load_tester_errors_total Internal tester errors grouped by stage.")
		fmt.Fprintln(w, "# TYPE load_tester_errors_total counter")
		for _, kind := range sortedKeys(m.errors) {
			fmt.Fprintf(w, "load_tester_errors_total{kind=%q} %d\n", escapeLabel(kind), m.errors[kind])
		}
	})
}

func sortedKeys[V ~uint64](values map[string]V) []string {
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
