package metrics

import (
	"fmt"
	"net/http"
	"runtime"
	"sync/atomic"
)

var (
	activeConnections            atomic.Int64
	connectionsTotal             atomic.Uint64
	disconnectionsTotal          atomic.Uint64
	authFailuresTotal            atomic.Uint64
	upgradeFailuresTotal         atomic.Uint64
	slowClientsTotal             atomic.Uint64
	notificationAttemptsTotal    atomic.Uint64
	realtimeEventsReceivedTotal  atomic.Uint64
	realtimeEventsDeliveredTotal atomic.Uint64
	realtimeEventsNoLocalTotal   atomic.Uint64
	realtimeEventFailuresTotal   atomic.Uint64
	rabbitMQReconnectsTotal      atomic.Uint64
)

func ConnectionOpened() {
	activeConnections.Add(1)
	connectionsTotal.Add(1)
}

func ConnectionClosed() {
	if activeConnections.Add(-1) < 0 {
		activeConnections.Store(0)
	}
	disconnectionsTotal.Add(1)
}

func IncAuthFailure() {
	authFailuresTotal.Add(1)
}

func IncUpgradeFailure() {
	upgradeFailuresTotal.Add(1)
}

func IncSlowClient() {
	slowClientsTotal.Add(1)
}

func IncNotificationAttempt() {
	notificationAttemptsTotal.Add(1)
}

func IncRealtimeEvent(localDeliveries int) {
	realtimeEventsReceivedTotal.Add(1)
	if localDeliveries > 0 {
		realtimeEventsDeliveredTotal.Add(1)
		return
	}
	realtimeEventsNoLocalTotal.Add(1)
}

func IncRealtimeEventFailure() {
	realtimeEventFailuresTotal.Add(1)
}

func IncRabbitReconnect() {
	rabbitMQReconnectsTotal.Add(1)
}

func Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		fmt.Fprintln(w, "# HELP websocket_connections_active Active upgraded WebSocket connections on this replica.")
		fmt.Fprintln(w, "# TYPE websocket_connections_active gauge")
		fmt.Fprintf(w, "websocket_connections_active %d\n", activeConnections.Load())

		fmt.Fprintln(w, "# HELP websocket_connections_total Successfully opened WebSocket connections.")
		fmt.Fprintln(w, "# TYPE websocket_connections_total counter")
		fmt.Fprintf(w, "websocket_connections_total %d\n", connectionsTotal.Load())

		fmt.Fprintln(w, "# HELP websocket_disconnections_total Closed WebSocket connections.")
		fmt.Fprintln(w, "# TYPE websocket_disconnections_total counter")
		fmt.Fprintf(w, "websocket_disconnections_total %d\n", disconnectionsTotal.Load())

		fmt.Fprintln(w, "# HELP websocket_auth_failures_total WebSocket upgrade attempts rejected by authentication.")
		fmt.Fprintln(w, "# TYPE websocket_auth_failures_total counter")
		fmt.Fprintf(w, "websocket_auth_failures_total %d\n", authFailuresTotal.Load())

		fmt.Fprintln(w, "# HELP websocket_upgrade_failures_total Failed HTTP to WebSocket upgrades.")
		fmt.Fprintln(w, "# TYPE websocket_upgrade_failures_total counter")
		fmt.Fprintf(w, "websocket_upgrade_failures_total %d\n", upgradeFailuresTotal.Load())

		fmt.Fprintln(w, "# HELP websocket_slow_client_disconnects_total Connections dropped because the send buffer was full.")
		fmt.Fprintln(w, "# TYPE websocket_slow_client_disconnects_total counter")
		fmt.Fprintf(w, "websocket_slow_client_disconnects_total %d\n", slowClientsTotal.Load())

		fmt.Fprintln(w, "# HELP websocket_notification_delivery_attempts_total Notification delivery attempts to live local connections.")
		fmt.Fprintln(w, "# TYPE websocket_notification_delivery_attempts_total counter")
		fmt.Fprintf(w, "websocket_notification_delivery_attempts_total %d\n", notificationAttemptsTotal.Load())

		fmt.Fprintln(w, "# HELP websocket_realtime_events_received_total Fanout events consumed by this WebSocket replica.")
		fmt.Fprintln(w, "# TYPE websocket_realtime_events_received_total counter")
		fmt.Fprintf(w, "websocket_realtime_events_received_total %d\n", realtimeEventsReceivedTotal.Load())

		fmt.Fprintln(w, "# HELP websocket_realtime_events_delivered_total Fanout events that matched at least one local WebSocket connection.")
		fmt.Fprintln(w, "# TYPE websocket_realtime_events_delivered_total counter")
		fmt.Fprintf(w, "websocket_realtime_events_delivered_total %d\n", realtimeEventsDeliveredTotal.Load())

		fmt.Fprintln(w, "# HELP websocket_realtime_events_no_local_connection_total Fanout events ignored because the target user had no connection on this replica.")
		fmt.Fprintln(w, "# TYPE websocket_realtime_events_no_local_connection_total counter")
		fmt.Fprintf(w, "websocket_realtime_events_no_local_connection_total %d\n", realtimeEventsNoLocalTotal.Load())

		fmt.Fprintln(w, "# HELP websocket_realtime_event_failures_total Realtime fanout events that failed local processing.")
		fmt.Fprintln(w, "# TYPE websocket_realtime_event_failures_total counter")
		fmt.Fprintf(w, "websocket_realtime_event_failures_total %d\n", realtimeEventFailuresTotal.Load())

		fmt.Fprintln(w, "# HELP websocket_rabbitmq_reconnects_total RabbitMQ reconnect attempts made by this WebSocket replica.")
		fmt.Fprintln(w, "# TYPE websocket_rabbitmq_reconnects_total counter")
		fmt.Fprintf(w, "websocket_rabbitmq_reconnects_total %d\n", rabbitMQReconnectsTotal.Load())

		var memory runtime.MemStats
		runtime.ReadMemStats(&memory)
		fmt.Fprintf(w, "websocket_go_goroutines %d\n", runtime.NumGoroutine())
		fmt.Fprintf(w, "websocket_go_heap_alloc_bytes %d\n", memory.HeapAlloc)
		fmt.Fprintf(w, "websocket_go_heap_inuse_bytes %d\n", memory.HeapInuse)
		fmt.Fprintf(w, "websocket_go_heap_objects %d\n", memory.HeapObjects)
		fmt.Fprintf(w, "websocket_go_gc_cycles_total %d\n", memory.NumGC)
	})
}
