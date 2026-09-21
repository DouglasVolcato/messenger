package tester

import (
	"context"
	"fmt"
	"log"
	mathrand "math/rand"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type Config struct {
	BaseURL          string
	WebSocketURL     string
	MetricsAddress   string
	Password         string
	UsersStart       int
	UsersMax         int
	UsersStep        int
	RPSStart         int
	RPSMax           int
	RPSStep          int
	WebSocketsStart  int
	WebSocketsMax    int
	WebSocketsStep   int
	PhaseDuration    time.Duration
	MaxConcurrency   int
	Seed             int64
}

type safeRand struct {
	mu sync.Mutex
	r  *mathrand.Rand
}

func (r *safeRand) Intn(max int) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.r.Intn(max)
}

func (r *safeRand) Uint32() uint32 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.r.Uint32()
}

type webSocketClient struct {
	conn *websocket.Conn
	done chan struct{}
	once sync.Once
}

type Runner struct {
	cfg Config

	httpClient *http.Client
	metrics    *Metrics
	random     *safeRand
	semaphore  chan struct{}

	metricsServer *http.Server

	usersMu sync.RWMutex
	users   []generatedUser

	messagesMu   sync.RWMutex
	chatMessages []messageRef

	wsMu       sync.Mutex
	webSockets []*webSocketClient

	companyID string
	chatID    string

	work sync.WaitGroup
}

func New(cfg Config) (*Runner, error) {
	cfg.BaseURL = strings.TrimRight(cfg.BaseURL, "/")
	if cfg.BaseURL == "" {
		return nil, fmt.Errorf("base URL is required")
	}
	if cfg.WebSocketURL == "" {
		return nil, fmt.Errorf("WebSocket URL is required")
	}
	if cfg.UsersStart < 1 {
		cfg.UsersStart = 1
	}
	if cfg.UsersMax < cfg.UsersStart {
		cfg.UsersMax = cfg.UsersStart
	}
	if cfg.UsersStep < 1 {
		cfg.UsersStep = 1
	}
	if cfg.RPSStart < 1 {
		cfg.RPSStart = 1
	}
	if cfg.RPSMax < cfg.RPSStart {
		cfg.RPSMax = cfg.RPSStart
	}
	if cfg.RPSStep < 1 {
		cfg.RPSStep = 1
	}
	if cfg.WebSocketsStart < 0 {
		cfg.WebSocketsStart = 0
	}
	if cfg.WebSocketsMax < cfg.WebSocketsStart {
		cfg.WebSocketsMax = cfg.WebSocketsStart
	}
	if cfg.WebSocketsStep < 1 {
		cfg.WebSocketsStep = 1
	}
	if cfg.PhaseDuration <= 0 {
		cfg.PhaseDuration = 30 * time.Second
	}
	if cfg.MaxConcurrency < 1 {
		cfg.MaxConcurrency = 64
	}

	transport := &http.Transport{
		MaxIdleConns:        cfg.MaxConcurrency * 2,
		MaxIdleConnsPerHost: cfg.MaxConcurrency,
		MaxConnsPerHost:     cfg.MaxConcurrency,
		IdleConnTimeout:     90 * time.Second,
	}

	return &Runner{
		cfg: cfg,
		httpClient: &http.Client{
			Transport: transport,
			Timeout:   15 * time.Second,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		metrics:   NewMetrics(),
		random:    &safeRand{r: mathrand.New(mathrand.NewSource(cfg.Seed))},
		semaphore: make(chan struct{}, cfg.MaxConcurrency),
	}, nil
}

func (r *Runner) Run(ctx context.Context) error {
	r.startMetricsServer(ctx)

	if err := r.ensureUsers(ctx, r.cfg.UsersStart); err != nil {
		return fmt.Errorf("bootstrap users: %w", err)
	}
	if err := r.ensureWebSockets(ctx, min(r.cfg.WebSocketsStart, r.userCount())); err != nil {
		return fmt.Errorf("bootstrap WebSockets: %w", err)
	}

	phases := max(
		steps(r.cfg.UsersStart, r.cfg.UsersMax, r.cfg.UsersStep),
		steps(r.cfg.RPSStart, r.cfg.RPSMax, r.cfg.RPSStep),
		steps(r.cfg.WebSocketsStart, r.cfg.WebSocketsMax, r.cfg.WebSocketsStep),
	) + 1

	for phase := 0; phase < phases; phase++ {
		if ctx.Err() != nil {
			break
		}

		targetUsers := boundedStep(r.cfg.UsersStart, r.cfg.UsersStep, r.cfg.UsersMax, phase)
		targetRPS := boundedStep(r.cfg.RPSStart, r.cfg.RPSStep, r.cfg.RPSMax, phase)
		targetWebSockets := boundedStep(r.cfg.WebSocketsStart, r.cfg.WebSocketsStep, r.cfg.WebSocketsMax, phase)
		if targetWebSockets > targetUsers {
			targetWebSockets = targetUsers
		}

		if err := r.ensureUsers(ctx, targetUsers); err != nil {
			return fmt.Errorf("phase %d users: %w", phase, err)
		}
		if err := r.ensureWebSockets(ctx, targetWebSockets); err != nil {
			return fmt.Errorf("phase %d WebSockets: %w", phase, err)
		}

		r.metrics.SetPhase(phase, targetRPS)
		log.Printf("phase=%d users=%d target_rps=%d websockets=%d duration=%s",
			phase, r.userCount(), targetRPS, r.activeWebSocketCount(), r.cfg.PhaseDuration)

		if err := r.runPhase(ctx, targetRPS, r.cfg.PhaseDuration); err != nil {
			return err
		}
	}

	r.work.Wait()
	log.Printf("load test complete: users=%d websockets=%d", r.userCount(), r.activeWebSocketCount())
	return nil
}

func (r *Runner) Close() {
	r.wsMu.Lock()
	sockets := append([]*webSocketClient(nil), r.webSockets...)
	r.wsMu.Unlock()
	for _, client := range sockets {
		_ = client.conn.Close()
		client.markClosed(r.metrics)
	}
	if r.metricsServer != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = r.metricsServer.Shutdown(ctx)
	}
	if transport, ok := r.httpClient.Transport.(*http.Transport); ok {
		transport.CloseIdleConnections()
	}
}

func (r *Runner) startMetricsServer(ctx context.Context) {
	r.metricsServer = &http.Server{
		Addr:              r.cfg.MetricsAddress,
		Handler:           r.metrics.Handler(),
		ReadHeaderTimeout: 3 * time.Second,
	}
	go func() {
		if err := r.metricsServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("tester metrics server: %v", err)
		}
	}()
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = r.metricsServer.Shutdown(shutdownCtx)
	}()
}

func (r *Runner) ensureUsers(ctx context.Context, target int) error {
	for r.userCount() < target {
		user, err := r.registerUser(ctx)
		if err != nil {
			r.metrics.IncError("register")
			return err
		}

		if r.userCount() == 0 {
			r.usersMu.Lock()
			r.users = append(r.users, user)
			r.usersMu.Unlock()

			r.companyID, err = r.createCompany(ctx, user)
			if err != nil {
				return err
			}
			r.chatID, err = r.createChat(ctx, user, r.companyID)
			if err != nil {
				return err
			}
			log.Printf("created load-test company=%s chat=%s", r.companyID, r.chatID)
		} else {
			admin := r.adminUser()
			if err := r.addMember(ctx, admin, r.companyID, user); err != nil {
				return err
			}
			if err := r.subscribe(ctx, user); err != nil {
				return err
			}
			r.usersMu.Lock()
			r.users = append(r.users, user)
			r.usersMu.Unlock()
		}

		r.metrics.SetActiveUsers(r.userCount())
	}
	return nil
}

func (r *Runner) runPhase(ctx context.Context, targetRPS int, duration time.Duration) error {
	if targetRPS < 1 {
		return nil
	}

	interval := time.Second / time.Duration(targetRPS)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	timer := time.NewTimer(duration)
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-timer.C:
			return nil
		case <-ticker.C:
			select {
			case r.semaphore <- struct{}{}:
				r.work.Add(1)
				go func() {
					defer func() {
						<-r.semaphore
						r.work.Done()
					}()
					r.runOperation(ctx)
				}()
			default:
				r.metrics.IncError("concurrency_limit")
			}
		}
	}
}

func (r *Runner) runOperation(ctx context.Context) {
	user, ok := r.randomUser()
	if !ok {
		return
	}

	roll := r.random.Intn(100)
	var err error

	switch {
	case roll < 40:
		err = r.sendChatMessage(ctx, user)
	case roll < 60:
		var recipient generatedUser
		recipient, ok = r.randomOtherUser(user.ID)
		if ok {
			err = r.sendDirectMessage(ctx, user, recipient)
		}
	case roll < 70:
		err = r.login(ctx, user)
	case roll < 78:
		err = r.readChat(ctx, user)
	case roll < 83:
		err = r.readCompanies(ctx, user)
	case roll < 87:
		err = r.readNotifications(ctx, user)
	case roll < 90:
		err = r.markNotificationsRead(ctx, user)
	case roll < 95:
		err = r.reactToMessage(ctx, user)
	case roll < 98:
		err = r.editOwnMessage(ctx, user)
	default:
		err = r.deleteOwnMessage(ctx, user)
	}

	if err != nil && ctx.Err() == nil {
		r.metrics.IncError("operation")
		log.Printf("operation error: %v", err)
	}
}

func (r *Runner) ensureWebSockets(ctx context.Context, target int) error {
	r.pruneClosedWebSockets()

	for r.activeWebSocketCount() < target {
		users := r.userSnapshot()
		index := r.activeWebSocketCount()
		if index >= len(users) {
			break
		}
		user := users[index]

		header := http.Header{}
		header.Set("Cookie", "user="+user.Token)
		dialer := *websocket.DefaultDialer
		dialer.HandshakeTimeout = 5 * time.Second

		connection, response, err := dialer.DialContext(ctx, r.cfg.WebSocketURL, header)
		if response != nil && response.Body != nil {
			_ = response.Body.Close()
		}
		if err != nil {
			r.metrics.IncWebSocketFailure()
			r.metrics.IncError("websocket_connect")
			return fmt.Errorf("connect WebSocket for %s: %w", user.Username, err)
		}

		client := &webSocketClient{conn: connection, done: make(chan struct{})}
		r.wsMu.Lock()
		r.webSockets = append(r.webSockets, client)
		r.wsMu.Unlock()
		r.metrics.IncWebSocketTotal()
		r.metrics.AdjustActiveWebSockets(1)

		go r.readWebSocket(client)
	}
	return nil
}

func (r *Runner) readWebSocket(client *webSocketClient) {
	for {
		if _, _, err := client.conn.ReadMessage(); err != nil {
			client.markClosed(r.metrics)
			return
		}
		r.metrics.IncWebSocketReceived()
	}
}

func (c *webSocketClient) markClosed(metrics *Metrics) {
	c.once.Do(func() {
		close(c.done)
		metrics.AdjustActiveWebSockets(-1)
	})
}

func (r *Runner) pruneClosedWebSockets() {
	r.wsMu.Lock()
	defer r.wsMu.Unlock()

	active := r.webSockets[:0]
	for _, client := range r.webSockets {
		select {
		case <-client.done:
			_ = client.conn.Close()
		default:
			active = append(active, client)
		}
	}
	r.webSockets = active
}

func (r *Runner) activeWebSocketCount() int {
	r.wsMu.Lock()
	defer r.wsMu.Unlock()
	count := 0
	for _, client := range r.webSockets {
		select {
		case <-client.done:
		default:
			count++
		}
	}
	return count
}

func (r *Runner) userCount() int {
	r.usersMu.RLock()
	defer r.usersMu.RUnlock()
	return len(r.users)
}

func (r *Runner) userSnapshot() []generatedUser {
	r.usersMu.RLock()
	defer r.usersMu.RUnlock()
	return append([]generatedUser(nil), r.users...)
}

func (r *Runner) adminUser() generatedUser {
	r.usersMu.RLock()
	defer r.usersMu.RUnlock()
	return r.users[0]
}

func (r *Runner) randomUser() (generatedUser, bool) {
	r.usersMu.RLock()
	defer r.usersMu.RUnlock()
	if len(r.users) == 0 {
		return generatedUser{}, false
	}
	return r.users[r.random.Intn(len(r.users))], true
}

func (r *Runner) randomOtherUser(excludeID string) (generatedUser, bool) {
	r.usersMu.RLock()
	defer r.usersMu.RUnlock()
	if len(r.users) < 2 {
		return generatedUser{}, false
	}
	for attempts := 0; attempts < 5; attempts++ {
		candidate := r.users[r.random.Intn(len(r.users))]
		if candidate.ID != excludeID {
			return candidate, true
		}
	}
	for _, candidate := range r.users {
		if candidate.ID != excludeID {
			return candidate, true
		}
	}
	return generatedUser{}, false
}

func steps(start, maximum, step int) int {
	if maximum <= start {
		return 0
	}
	return (maximum - start + step - 1) / step
}

func boundedStep(start, step, maximum, phase int) int {
	value := start + step*phase
	if value > maximum {
		return maximum
	}
	return value
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(values ...int) int {
	result := 0
	for _, value := range values {
		if value > result {
			result = value
		}
	}
	return result
}
