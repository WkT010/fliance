package market

import (
	"encoding/json"
	"errors"
	"log/slog"
	"math/big"
	"strings"
	"sync"
	"time"

	"github.com/WkT010/nexa-exchange/internal/matching"
	"github.com/gorilla/websocket"
)

// Binance combined-stream WebSocket client.
//
// api.binance.com is geo-blocked in some regions; the public market-data
// mirror wss://data-stream.binance.vision:443/stream serves the identical
// combined streams without authentication and is the default endpoint
// (configurable via BINANCE_WS_URL).
//
// The client subscribes, for every supported symbol, to:
//   - <sym>@miniTicker      (24h rolling stats, ~1/s per symbol)
//   - <sym>@kline_<iv>      (1m/5m/15m/1h/4h/1d candles)
//   - <sym>@depth10@100ms   (top-10 L2 snapshot every 100ms)
//   - <sym>@aggTrade        (aggregated trade tape)
//
// All events are cached in memory with per-source last-update timestamps so
// consumers can apply their own freshness policy. Reconnection uses
// exponential backoff and resubscribes implicitly (streams are part of the
// URL); connections are proactively rebuilt every 24h.

const (
	binanceWSReconnectMin = 1 * time.Second
	binanceWSReconnectMax = 30 * time.Second
	binanceWSMaxConnAge   = 24 * time.Hour
	binanceWSPingInterval = 20 * time.Second
	binanceWSReadTimeout  = 60 * time.Second
	binanceWSTradeCap     = 200 // trades kept per symbol (newest retained)
)

// binanceWSKlineIntervals are the kline streams subscribed per symbol.
var binanceWSKlineIntervals = []string{"1m", "5m", "15m", "1h", "4h", "1d"}

// wsMiniTicker is the payload of the <sym>@miniTicker stream.
type wsMiniTicker struct {
	Symbol   string `json:"s"`
	Last     string `json:"c"`
	Open     string `json:"o"`
	High     string `json:"h"`
	Low      string `json:"l"`
	BaseVol  string `json:"v"`
	QuoteVol string `json:"q"`
}

// BinanceWSClient consumes Binance combined streams and caches them in memory.
type BinanceWSClient struct {
	url     string
	streams string // precomputed "a/b/c" stream list for the ?streams= param

	pairs map[string]string // "BTC/USDT" -> "BTCUSDT"
	bySym map[string]string // "BTCUSDT" -> "BTC/USDT"

	mu       sync.RWMutex
	mini     map[string]*wsMiniTicker    // pair -> latest mini ticker
	tickerAt map[string]int64            // pair -> unix ms of last mini update
	depth    map[string]*Depth           // pair -> top-10 book snapshot
	depthAt  map[string]int64            // pair -> unix ms of last depth update
	trades   map[string][]RecentTrade    // pair -> tape, newest last
	tradesAt map[string]int64            // pair -> unix ms of last trade
	klines   map[string]*matching.Candle // pair|interval -> current candle
	klinesAt map[string]int64            // pair|interval -> unix ms of last kline update

	// Optional event hooks (called outside the cache lock), used by the
	// gateway to bridge Binance events into the platform's WS hub.
	onDepth func(pair string, d *Depth)
	onTrade func(pair string, t RecentTrade)
	// onKline persists every live kline update into the candle store so the
	// in-progress bucket stays current and /klines never needs a REST backfill
	// for the newest bar.
	onKline func(c *matching.Candle)

	mu2     sync.Mutex
	running bool
	done    chan struct{}
}

// NewBinanceWSClient creates a client for the given combined-stream endpoint
// and trading pairs (e.g. ["BTC/USDT", ...]). Pairs without a Binance symbol
// mapping are silently skipped.
func NewBinanceWSClient(wsURL string, pairs []string) *BinanceWSClient {
	if wsURL == "" {
		wsURL = "wss://data-stream.binance.vision:443/stream"
	}
	c := &BinanceWSClient{
		url:      wsURL,
		pairs:    make(map[string]string),
		bySym:    make(map[string]string),
		mini:     make(map[string]*wsMiniTicker),
		tickerAt: make(map[string]int64),
		depth:    make(map[string]*Depth),
		depthAt:  make(map[string]int64),
		trades:   make(map[string][]RecentTrade),
		tradesAt: make(map[string]int64),
		klines:   make(map[string]*matching.Candle),
		klinesAt: make(map[string]int64),
		done:     make(chan struct{}),
	}
	var streams []string
	for _, p := range pairs {
		sym, ok := supportedPairs[p]
		if !ok {
			continue
		}
		c.pairs[p] = sym
		c.bySym[sym] = p
		lower := strings.ToLower(sym)
		streams = append(streams, lower+"@miniTicker", lower+"@depth10@100ms", lower+"@aggTrade")
		for _, iv := range binanceWSKlineIntervals {
			streams = append(streams, lower+"@kline_"+iv)
		}
	}
	c.streams = strings.Join(streams, "/")
	return c
}

// SetDepthHandler installs a callback invoked on every depth snapshot.
func (c *BinanceWSClient) SetDepthHandler(f func(pair string, d *Depth)) { c.onDepth = f }

// SetTradeHandler installs a callback invoked on every aggregated trade.
func (c *BinanceWSClient) SetTradeHandler(f func(pair string, t RecentTrade)) { c.onTrade = f }

// SetKlineHandler installs a callback invoked on every kline stream update
// (both in-progress ticks and closed bars).
func (c *BinanceWSClient) SetKlineHandler(f func(cd *matching.Candle)) { c.onKline = f }

// Start launches the connection loop. No-op if already running.
func (c *BinanceWSClient) Start() {
	c.mu2.Lock()
	defer c.mu2.Unlock()
	if c.running || c.streams == "" {
		return
	}
	c.running = true
	go c.run()
	slog.Info("binance ws client started", "url", c.url, "streams", strings.Count(c.streams, "/")+1)
}

// Stop shuts the client down and blocks until the loop exits.
func (c *BinanceWSClient) Stop() {
	c.mu2.Lock()
	if !c.running {
		c.mu2.Unlock()
		return
	}
	c.running = false
	close(c.done)
	c.mu2.Unlock()
	slog.Info("binance ws client stopped")
}

func (c *BinanceWSClient) run() {
	backoff := binanceWSReconnectMin
	for {
		select {
		case <-c.done:
			return
		default:
		}
		err := c.connectAndServe()
		select {
		case <-c.done:
			return
		default:
		}
		if err != nil {
			slog.Warn("binance ws disconnected; reconnecting", "err", err, "retry_in", backoff.String())
			select {
			case <-c.done:
				return
			case <-time.After(backoff):
			}
			backoff *= 2
			if backoff > binanceWSReconnectMax {
				backoff = binanceWSReconnectMax
			}
		} else {
			// Healthy session ended (e.g. the 24h rebuild): reconnect fast.
			backoff = binanceWSReconnectMin
		}
	}
}

func (c *BinanceWSClient) connectAndServe() error {
	dialer := websocket.Dialer{HandshakeTimeout: 10 * time.Second}
	conn, _, err := dialer.Dial(c.url+"?streams="+c.streams, nil)
	if err != nil {
		return err
	}
	defer conn.Close()
	conn.SetReadLimit(1 << 20)
	conn.SetReadDeadline(time.Now().Add(binanceWSReadTimeout))
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(binanceWSReadTimeout))
		return nil
	})
	slog.Info("binance ws connected", "url", c.url)

	// Reader goroutine: push messages into a bounded channel so a slow
	// consumer can never block the ping/rebuild select loop.
	msgCh := make(chan []byte, 512)
	errCh := make(chan error, 1)
	go func() {
		for {
			_, data, rerr := conn.ReadMessage()
			if rerr != nil {
				errCh <- rerr
				return
			}
			select {
			case msgCh <- data:
			default:
				// Backpressure: drop rather than block; the next snapshot
				// (depth10@100ms / miniTicker) self-heals within ~100ms.
			}
		}
	}()

	ping := time.NewTicker(binanceWSPingInterval)
	defer ping.Stop()
	rebuild := time.NewTimer(binanceWSMaxConnAge)
	defer rebuild.Stop()

	for {
		select {
		case <-c.done:
			return errors.New("client stopped")
		case <-rebuild.C:
			return errors.New("proactive rebuild: connection age exceeded 24h")
		case err := <-errCh:
			return err
		case <-ping.C:
			if err := conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(5*time.Second)); err != nil {
				return err
			}
		case data := <-msgCh:
			c.handleMessage(data)
		}
	}
}

type wsEnvelope struct {
	Stream string          `json:"stream"`
	Data   json.RawMessage `json:"data"`
}

func (c *BinanceWSClient) handleMessage(data []byte) {
	var env wsEnvelope
	if err := json.Unmarshal(data, &env); err != nil {
		return
	}
	parts := strings.SplitN(env.Stream, "@", 2)
	if len(parts) != 2 {
		return
	}
	// Stream names and symbols in the envelope are lowercased by Binance.
	kind := strings.ToLower(parts[1])
	pair, ok := c.bySym[strings.ToUpper(parts[0])]
	if !ok {
		return
	}
	switch {
	case kind == "miniticker":
		c.handleMiniTicker(pair, env.Data)
	case kind == "aggtrade":
		c.handleAggTrade(pair, env.Data)
	case kind == "kline" || strings.HasPrefix(kind, "kline_"):
		c.handleKline(pair, env.Data)
	case strings.HasPrefix(kind, "depth"):
		c.handleDepth(pair, env.Data)
	}
}

func (c *BinanceWSClient) handleMiniTicker(pair string, data []byte) {
	var mt wsMiniTicker
	if err := json.Unmarshal(data, &mt); err != nil {
		return
	}
	if _, ok := new(big.Float).SetString(mt.Last); !ok {
		return
	}
	c.mu.Lock()
	c.mini[pair] = &mt
	c.tickerAt[pair] = time.Now().UnixMilli()
	c.mu.Unlock()
}

func (c *BinanceWSClient) handleAggTrade(pair string, data []byte) {
	var raw struct {
		Price        string `json:"p"`
		Qty          string `json:"q"`
		Time         int64  `json:"T"`
		IsBuyerMaker bool   `json:"m"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return
	}
	price, ok1 := new(big.Float).SetString(raw.Price)
	qty, ok2 := new(big.Float).SetString(raw.Qty)
	if !ok1 || !ok2 {
		return
	}
	// isBuyerMaker==true means the aggressive taker was a seller.
	t := RecentTrade{Pair: pair, Price: price, Quantity: qty, Time: raw.Time, IsBuyer: !raw.IsBuyerMaker}
	c.mu.Lock()
	list := append(c.trades[pair], t)
	if len(list) > binanceWSTradeCap {
		list = list[len(list)-binanceWSTradeCap:]
	}
	c.trades[pair] = list
	c.tradesAt[pair] = time.Now().UnixMilli()
	cb := c.onTrade
	c.mu.Unlock()
	if cb != nil {
		cb(pair, t)
	}
}

func (c *BinanceWSClient) handleKline(pair string, data []byte) {
	// The kline payload mixes case-sensitive pairs ("t"/"T", "l"/"L",
	// "v"/"V") that encoding/json cannot disambiguate into distinct struct
	// fields, so the k object is decoded into a raw map and fields are read
	// by exact key.
	var wrapper struct {
		K json.RawMessage `json:"k"`
	}
	if err := json.Unmarshal(data, &wrapper); err != nil {
		return
	}
	var kline map[string]json.RawMessage
	if err := json.Unmarshal(wrapper.K, &kline); err != nil {
		return
	}
	var interval string
	var openMs, closeMs int64
	var o, h, l, cl, v string
	if json.Unmarshal(kline["i"], &interval) != nil ||
		json.Unmarshal(kline["t"], &openMs) != nil || json.Unmarshal(kline["T"], &closeMs) != nil {
		return
	}
	if json.Unmarshal(kline["o"], &o) != nil || json.Unmarshal(kline["h"], &h) != nil ||
		json.Unmarshal(kline["l"], &l) != nil || json.Unmarshal(kline["c"], &cl) != nil ||
		json.Unmarshal(kline["v"], &v) != nil {
		return
	}
	open, ok1 := new(big.Float).SetString(o)
	high, ok2 := new(big.Float).SetString(h)
	low, ok3 := new(big.Float).SetString(l)
	close_, ok4 := new(big.Float).SetString(cl)
	vol, ok5 := new(big.Float).SetString(v)
	if !ok1 || !ok2 || !ok3 || !ok4 || !ok5 {
		return
	}
	key := pair + "|" + interval
	candle := &matching.Candle{
		Pair: pair, Interval: interval,
		Open: open, High: high, Low: low, Close: close_, Volume: vol,
		// Candle timestamps in this codebase are nanoseconds (see
		// CandleService.updateCandle); Binance sends milliseconds.
		Timestamp: openMs * int64(time.Millisecond),
		CloseTime: closeMs * int64(time.Millisecond),
	}
	c.mu.Lock()
	c.klines[key] = candle
	c.klinesAt[key] = time.Now().UnixMilli()
	cb := c.onKline
	c.mu.Unlock()
	if cb != nil {
		cb(candle)
	}
}

func (c *BinanceWSClient) handleDepth(pair string, data []byte) {
	// Partial book depth streams (depth10/depth20) use full field names
	// "bids"/"asks"; diff depth streams use "b"/"a". Accept both.
	var raw struct {
		FullBids [][]string `json:"bids"`
		FullAsks [][]string `json:"asks"`
		Bids     [][]string `json:"b"`
		Asks     [][]string `json:"a"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return
	}
	bids, asks := raw.FullBids, raw.FullAsks
	if len(bids) == 0 {
		bids = raw.Bids
	}
	if len(asks) == 0 {
		asks = raw.Asks
	}
	d := &Depth{Pair: pair, Bids: parseDepthLevels(bids), Asks: parseDepthLevels(asks)}
	c.mu.Lock()
	c.depth[pair] = d
	c.depthAt[pair] = time.Now().UnixMilli()
	cb := c.onDepth
	c.mu.Unlock()
	if cb != nil {
		cb(pair, d)
	}
}

func parseDepthLevels(levels [][]string) []DepthLevel {
	out := make([]DepthLevel, 0, len(levels))
	for _, lv := range levels {
		if len(lv) < 2 {
			continue
		}
		price, ok1 := new(big.Float).SetString(lv[0])
		qty, ok2 := new(big.Float).SetString(lv[1])
		if !ok1 || !ok2 {
			continue
		}
		out = append(out, DepthLevel{Price: price, Quantity: qty})
	}
	return out
}

// buildTicker merges the mini ticker with the best bid/ask from the cached
// depth snapshot. updatedAt (unix ms) is exposed via Ticker.Timestamp so
// callers can apply a uniform freshness policy.
func (c *BinanceWSClient) buildTicker(pair string) (*Ticker, int64) {
	c.mu.RLock()
	mini := c.mini[pair]
	at := c.tickerAt[pair]
	depth := c.depth[pair]
	c.mu.RUnlock()
	if mini == nil || at == 0 {
		return nil, 0
	}
	last, _ := new(big.Float).SetString(mini.Last)
	open, _ := new(big.Float).SetString(mini.Open)
	high, _ := new(big.Float).SetString(mini.High)
	low, _ := new(big.Float).SetString(mini.Low)
	vol, _ := new(big.Float).SetString(mini.BaseVol)
	qvol, _ := new(big.Float).SetString(mini.QuoteVol)

	bid, ask := new(big.Float).Copy(last), new(big.Float).Copy(last)
	if depth != nil {
		if len(depth.Bids) > 0 && depth.Bids[0].Price != nil {
			bid = new(big.Float).Copy(depth.Bids[0].Price)
		}
		if len(depth.Asks) > 0 && depth.Asks[0].Price != nil {
			ask = new(big.Float).Copy(depth.Asks[0].Price)
		}
	}
	chg := new(big.Float).Sub(last, open)
	pct := new(big.Float)
	if open != nil && open.Sign() > 0 {
		pct.Quo(chg, open)
	}
	return &Ticker{
		Pair: pair, Last: last, Bid: bid, Ask: ask,
		Spread: new(big.Float).Sub(ask, bid),
		Volume24h: vol, QuoteVolume24h: qvol, High24h: high, Low24h: low,
		Open24h: open, Change24h: chg, ChangePct24h: pct,
		Timestamp: at,
	}, at
}

// Ticker returns the cached ticker for pair and its last-update time (unix
// ms). Returns (nil, 0) when no data has been received yet.
func (c *BinanceWSClient) Ticker(pair string) (*Ticker, int64) {
	return c.buildTicker(pair)
}

// AllTickers returns cached tickers for every subscribed pair.
func (c *BinanceWSClient) AllTickers() map[string]*Ticker {
	out := make(map[string]*Ticker, len(c.pairs))
	for pair := range c.pairs {
		if t, _ := c.buildTicker(pair); t != nil {
			out[pair] = t
		}
	}
	return out
}

// Depth returns the cached top-of-book snapshot and its last-update time
// (unix ms).
func (c *BinanceWSClient) Depth(pair string) (*Depth, int64) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	d := c.depth[pair]
	if d == nil {
		return nil, 0
	}
	return d, c.depthAt[pair]
}

// RecentTrades returns up to `limit` cached trades, newest first, and the
// last-update time (unix ms).
func (c *BinanceWSClient) RecentTrades(pair string, limit int) ([]RecentTrade, int64) {
	if limit <= 0 {
		limit = 50
	}
	c.mu.RLock()
	list := c.trades[pair]
	at := c.tradesAt[pair]
	out := make([]RecentTrade, 0, len(list))
	for i := len(list) - 1; i >= 0 && len(out) < limit; i-- {
		out = append(out, list[i])
	}
	c.mu.RUnlock()
	if len(out) == 0 {
		return nil, at
	}
	return out, at
}

// CurrentKline returns the latest cached kline for pair/interval and its
// last-update time (unix ms).
func (c *BinanceWSClient) CurrentKline(pair, interval string) (*matching.Candle, int64) {
	key := pair + "|" + interval
	c.mu.RLock()
	defer c.mu.RUnlock()
	k := c.klines[key]
	if k == nil {
		return nil, 0
	}
	return k, c.klinesAt[key]
}
