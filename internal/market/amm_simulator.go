package market

import (
	"log/slog"
	"sync"
	"time"
)

// Simulator periodically runs small swaps against every AMM pool so the market
// shows live price movement even when no real trader is active. It is the
// mechanism that makes the exchange's price feed self-contained (no external
// source required): the AMM pools themselves produce the price action.
type Simulator struct {
	feed     *AMMPriceFeed
	interval time.Duration

	mu      sync.Mutex
	running bool
	stop    chan struct{}
}

// NewSimulator constructs a simulator. interval defaults to 3s if <= 0.
func NewSimulator(feed *AMMPriceFeed, interval time.Duration) *Simulator {
	if interval <= 0 {
		interval = 3 * time.Second
	}
	return &Simulator{feed: feed, interval: interval}
}

// Interval returns the configured tick interval.
func (s *Simulator) Interval() time.Duration { return s.interval }

// IsRunning reports whether the simulator goroutine is active.
func (s *Simulator) IsRunning() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running
}

// Start launches the simulator goroutine. It is a no-op if already running.
// The first tick fires immediately so prices are populated at startup, then on
// each interval. Errors from individual pools are logged, not fatal.
func (s *Simulator) Start() {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return
	}
	s.running = true
	s.stop = make(chan struct{})
	s.mu.Unlock()
	go s.loop()
}

// Stop signals the simulator goroutine to exit. It is a no-op if not running.
func (s *Simulator) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.running {
		return
	}
	s.running = false
	close(s.stop)
}

func (s *Simulator) loop() {
	slog.Info("simulator started", "interval", s.interval)
	// Prime the feed immediately so the first /tickers request has data
	// without waiting a full interval.
	s.tick()
	t := time.NewTicker(s.interval)
	defer t.Stop()
	for {
		select {
		case <-s.stop:
			slog.Info("simulator stopped")
			return
		case <-t.C:
			s.tick()
		}
	}
}

// tick runs one simulated swap per known pool. A reload happens first so any
// real-user swap/liquidity change is reflected, then each pool is perturbed.
func (s *Simulator) tick() {
	if err := s.feed.Reload(); err != nil {
		slog.Warn("simulator reload failed", "err", err)
		return
	}
	for _, pair := range s.feed.Pairs() {
		if err := s.feed.ApplySimulatedSwap(pair, 0); err != nil {
			slog.Warn("simulator swap failed", "pair", pair, "err", err)
		}
	}
}
