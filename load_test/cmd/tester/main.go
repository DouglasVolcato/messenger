package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/douglasvolcato/messenger/load_test/internal/tester"
)

func main() {
	cfg := tester.Config{}
	flag.StringVar(&cfg.BaseURL, "base-url", env("TEST_BASE_URL", "http://load-balancer"), "HTTP base URL routed through Nginx")
	flag.StringVar(&cfg.WebSocketURL, "ws-url", env("TEST_WS_URL", "ws://load-balancer/ws/notifications"), "WebSocket URL")
	flag.StringVar(&cfg.MetricsAddress, "metrics-address", env("TEST_METRICS_ADDRESS", ":9091"), "tester Prometheus listen address")
	flag.StringVar(&cfg.Password, "password", env("TEST_USER_PASSWORD", "LoadTest123!"), "password used by generated accounts")
	flag.IntVar(&cfg.UsersStart, "users-start", envInt("TEST_USERS_START", 10), "users available in the first phase")
	flag.IntVar(&cfg.UsersMax, "users-max", envInt("TEST_USERS_MAX", 100), "maximum generated users")
	flag.IntVar(&cfg.UsersStep, "users-step", envInt("TEST_USERS_STEP", 10), "users added per phase")
	flag.IntVar(&cfg.RPSStart, "rps-start", envInt("TEST_RPS_START", 5), "requests per second in the first phase")
	flag.IntVar(&cfg.RPSMax, "rps-max", envInt("TEST_RPS_MAX", 50), "maximum requests per second")
	flag.IntVar(&cfg.RPSStep, "rps-step", envInt("TEST_RPS_STEP", 5), "requests per second added per phase")
	flag.IntVar(&cfg.WebSocketsStart, "ws-start", envInt("TEST_WS_START", 5), "WebSocket connections in the first phase")
	flag.IntVar(&cfg.WebSocketsMax, "ws-max", envInt("TEST_WS_MAX", 100), "maximum WebSocket connections")
	flag.IntVar(&cfg.WebSocketsStep, "ws-step", envInt("TEST_WS_STEP", 10), "WebSocket connections added per phase")
	flag.DurationVar(&cfg.PhaseDuration, "phase-duration", envDuration("TEST_PHASE_DURATION", 30*time.Second), "duration of each load phase")
	flag.IntVar(&cfg.MaxConcurrency, "max-concurrency", envInt("TEST_MAX_CONCURRENCY", 256), "maximum concurrent HTTP operations")
	flag.Int64Var(&cfg.Seed, "seed", envInt64("TEST_SEED", time.Now().UnixNano()), "pseudo-random seed")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	runner, err := tester.New(cfg)
	if err != nil {
		log.Fatal(err)
	}
	defer runner.Close()

	log.Printf("load tester starting: base=%s ws=%s users=%d..%d rps=%d..%d websockets=%d..%d phase=%s",
		cfg.BaseURL, cfg.WebSocketURL, cfg.UsersStart, cfg.UsersMax, cfg.RPSStart, cfg.RPSMax,
		cfg.WebSocketsStart, cfg.WebSocketsMax, cfg.PhaseDuration)

	if err := runner.Run(ctx); err != nil && ctx.Err() == nil {
		log.Fatal(err)
	}
}

func env(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func envInt(name string, fallback int) int {
	var value int
	if _, err := fmt.Sscanf(os.Getenv(name), "%d", &value); err == nil {
		return value
	}
	return fallback
}

func envInt64(name string, fallback int64) int64 {
	var value int64
	if _, err := fmt.Sscanf(os.Getenv(name), "%d", &value); err == nil {
		return value
	}
	return fallback
}

func envDuration(name string, fallback time.Duration) time.Duration {
	if value := os.Getenv(name); value != "" {
		if parsed, err := time.ParseDuration(value); err == nil {
			return parsed
		}
	}
	return fallback
}
