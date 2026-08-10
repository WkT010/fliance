package api

import (
	"errors"
	"log/slog"
	"math/big"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/WkT010/nexa-exchange/internal/market"
	"github.com/WkT010/nexa-exchange/internal/matching"
	"github.com/WkT010/nexa-exchange/internal/risk"
	"github.com/gin-gonic/gin"
)

// OrderStore is the persistence interface required by the order handler and
// the WebSocket bridge.
type OrderStore interface {
	Save(o *matching.Order) error
	Get(id string) (*matching.Order, error)
	ListByUser(userID, pair string, status matching.OrderStatus, limit, offset int) ([]*matching.Order, error)
	UpdateOrderStatus(id string, status matching.OrderStatus) error
	SaveTrade(t *matching.Trade) error
	GetTrades(pair string, limit int) ([]*matching.Trade, error)
}

// CandlestickStore loads historical OHLCV candles.
type CandlestickStore interface {
	Candles(pair, interval string, from, to int64, limit int) ([]*matching.Candle, error)
}

// KlineFetcher backfills historical candles from an external market (Binance)
// when the local candle store has no data yet (fresh deployment).
type KlineFetcher interface {
	FetchKlines(pair, interval string, limit int) ([]*matching.Candle, error)
}

// OrderReleaser releases reserved balances for cancelled or filled orders.
type OrderReleaser interface {
	ReleaseOrder(orderID, userID string) error
}

// MarketDataProvider supplies external (reference) market depth and recent
// trades. The order handler uses it to fall back to a real book / trade tape
// when the matching engine's own state is empty, so the trading UI never shows
// blank panels on a fresh deployment with no resting orders or fills.
type MarketDataProvider interface {
	MarketDepth(pair string, limit int) (*market.Depth, error)
	RecentTrades(pair string, limit int) ([]market.RecentTrade, error)
}

type OrderHandler struct {
	engines     map[string]*matching.MatchingEngine
	exchange    *matching.ExchangeEngine
	store       OrderStore
	candleStore CandlestickStore
	klines      KlineFetcher
	wallet      WalletService
	releaser    OrderReleaser
	risk        *risk.Engine
	prices      MarketDataProvider
}

// NewOrderHandlerWithExchange constructs an order handler backed by the
// exchange facade.
func NewOrderHandlerWithExchange(ex *matching.ExchangeEngine, store OrderStore, riskEng *risk.Engine) *OrderHandler {
	return &OrderHandler{
		exchange: ex,
		store:    store,
		engines:  make(map[string]*matching.MatchingEngine),
		risk:     riskEng,
	}
}

func (h *OrderHandler) SetWallet(ws WalletService, releaser OrderReleaser) {
	h.wallet = ws
	h.releaser = releaser
}

func (h *OrderHandler) SetCandleStore(cs CandlestickStore) {
	h.candleStore = cs
}

// SetPriceProvider wires an external market-data provider used as a fallback
// when the matching engine's own order book is empty.
func (h *OrderHandler) SetPriceProvider(p MarketDataProvider) {
	h.prices = p
}

// SetKlineFetcher wires the external (Binance) kline source used to backfill
// candles when the local store is empty.
func (h *OrderHandler) SetKlineFetcher(k KlineFetcher) {
	h.klines = k
}

func (h *OrderHandler) getEngine(pair string) *matching.MatchingEngine {
	if h.exchange != nil {
		return h.exchange.Get(pair)
	}
	return h.engines[pair]
}

// GetCandles with *pair support
func (h *OrderHandler) GetCandles(c *gin.Context) {
	pair := strings.TrimPrefix(c.Param("pair"), "/")
	if h.getEngine(pair) == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported pair"})
		return
	}
	interval := c.DefaultQuery("interval", "1m")
	if matching.IntervalSeconds(interval) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported interval"})
		return
	}
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "500"))
	if limit <= 0 || limit > 1500 {
		limit = 500
	}
	var candles []*matching.Candle
	if h.candleStore != nil {
		// Query the most recent window only: the store orders ASC, so an
		// unbounded query would return the OLDEST `limit` candles and miss
		// the recent ones (and repeatedly trigger backfill).
		intervalNs := matching.IntervalSeconds(interval) * int64(time.Second)
		start := time.Now().UnixNano() - int64(limit)*intervalNs
		stored, err := h.candleStore.Candles(pair, interval, start, 0, limit)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load candles"})
			return
		}
		candles = stored
	} else if engine := h.getEngine(pair); engine != nil && engine.MD != nil {
		candles = engine.MD.Candles(interval, limit, 0, 0)
	}
	// Backfill from Binance when the local store cannot cover the recent
	// window: either it is empty (fresh deployment) or its newest candle
	// ended more than one interval ago (e.g. only stale simulator-recorded
	// candles exist). Fetched klines are persisted (upsert per bucket) so
	// subsequent reads hit the store, then served.
	needBackfill := len(candles) == 0
	if !needBackfill && interval != "1s" {
		last := candles[len(candles)-1]
		// The newest candle must cover the current instant (i.e. the in-progress
		// bucket). A bucket that merely ended less than one interval ago still
		// means the current bucket is missing.
		if last == nil || last.CloseTime < time.Now().UnixNano() {
			needBackfill = true
		}
		// Thin series (fewer bars than the requested window, e.g. only a handful
		// of stale simulator-recorded buckets) are refreshed even when the last
		// bucket looks current: the upsert overwrites those rows with real
		// external data.
		if !needBackfill && len(candles) < limit {
			needBackfill = true
		}
	}
	if needBackfill && h.klines != nil && interval != "1s" {
		fetchLimit := klineBackfillLimit(interval, limit)
		if fetched, err := h.fetchKlinesWithTimeout(pair, interval, fetchLimit); err == nil && len(fetched) > 0 {
			h.persistCandles(fetched)
			candles = fetched
		}
	}
	if candles == nil {
		candles = []*matching.Candle{}
	}
	result := make([]gin.H, len(candles))
	for i, cd := range candles {
		result[i] = candleToJSON(cd)
	}
	c.JSON(http.StatusOK, gin.H{"candles": result, "interval": interval, "pair": pair})
}

// klineFetchTimeout bounds synchronous external backfills so a slow mirror can
// never stall a /klines request for 30s+ (the cold-start 500s).
const klineFetchTimeout = 8 * time.Second

// klineBackfillLimits sets how many historical candles are fetched from the
// external mirror per interval. Long intervals need fewer bars to cover a
// useful span; all values stay within Binance's hard limit of 1000.
var klineBackfillLimits = map[string]int{
	"1m": 500, "5m": 500, "15m": 500, "1h": 500, "4h": 300, "1d": 365,
}

// klineBackfillLimit picks the fetch size for an interval: the per-interval
// default, or the client's requested limit when larger, capped at 1000.
func klineBackfillLimit(interval string, requested int) int {
	l, ok := klineBackfillLimits[interval]
	if !ok {
		l = 500
	}
	if requested > l {
		l = requested
	}
	if l > 1000 {
		l = 1000
	}
	return l
}

// fetchKlinesWithTimeout runs the external fetch in a guard goroutine and
// gives up after klineFetchTimeout, serving whatever local candles exist
// instead of hanging the HTTP request.
func (h *OrderHandler) fetchKlinesWithTimeout(pair, interval string, limit int) ([]*matching.Candle, error) {
	type fetchResult struct {
		candles []*matching.Candle
		err     error
	}
	ch := make(chan fetchResult, 1)
	go func() {
		f, err := h.klines.FetchKlines(pair, interval, limit)
		ch <- fetchResult{f, err}
	}()
	select {
	case r := <-ch:
		return r.candles, r.err
	case <-time.After(klineFetchTimeout):
		slog.Warn("kline backfill timed out", "pair", pair, "interval", interval, "timeout", klineFetchTimeout.String())
		return nil, errors.New("kline backfill timeout")
	}
}

// persistCandles stores fetched candles when the backing store supports it
// (upsert per bucket, so re-fetches refresh stale simulator-era rows). Batch
// upserts are preferred; the remote store otherwise pays one round-trip per
// row, which dominates backfill latency.
func (h *OrderHandler) persistCandles(candles []*matching.Candle) {
	if batcher, ok := h.candleStore.(interface{ SaveCandles([]*matching.Candle) error }); ok {
		_ = batcher.SaveCandles(candles)
		return
	}
	saver, ok := h.candleStore.(interface{ SaveCandle(*matching.Candle) error })
	if !ok {
		return
	}
	for _, cd := range candles {
		_ = saver.SaveCandle(cd)
	}
}

// klineFresh reports whether the local store already covers the current bucket
// for pair/interval with a reasonable number of bars (i.e. no backfill is
// needed). Thin series (e.g. only a handful of stale simulator candles) are
// treated as missing so prewarming repairs them.
func (h *OrderHandler) klineFresh(pair, interval string, want int) bool {
	intervalNs := matching.IntervalSeconds(interval) * int64(time.Second)
	if intervalNs == 0 {
		return false
	}
	start := time.Now().UnixNano() - int64(want)*intervalNs
	candles, err := h.candleStore.Candles(pair, interval, start, 0, want)
	if err != nil || len(candles) == 0 {
		return false
	}
	last := candles[len(candles)-1]
	if last == nil || last.CloseTime < time.Now().UnixNano() {
		return false
	}
	return len(candles) >= want/2
}

// StartKlinePrewarm asynchronously backfills candles for every supported pair
// and chart interval right after boot, so the first /klines request per
// (pair, interval) hits the local store instead of blocking on the external
// mirror (which caused the cold-start hangs/500s). Fetches are spaced out to
// stay well within the mirror's rate limits.
func (h *OrderHandler) StartKlinePrewarm(pairs []string) {
	if h.klines == nil || h.candleStore == nil {
		return
	}
	go func() {
		series, total := 0, 0
		for _, interval := range []string{"1m", "5m", "15m", "1h", "4h", "1d"} {
			limit := klineBackfillLimit(interval, 0)
			for _, pair := range pairs {
				if h.klineFresh(pair, interval, limit) {
					continue
				}
				slog.Info("kline prewarm backfilling", "pair", pair, "interval", interval, "limit", limit)
				fetched, err := h.fetchKlinesWithTimeout(pair, interval, limit)
				if err != nil || len(fetched) == 0 {
					if err != nil {
						slog.Warn("kline prewarm failed", "pair", pair, "interval", interval, "err", err)
					}
					continue
				}
				h.persistCandles(fetched)
				series++
				total += len(fetched)
				// ~4 calls/s max keeps the mirror weight usage trivial.
				time.Sleep(250 * time.Millisecond)
			}
		}
		slog.Info("kline prewarm complete", "series", series, "candles", total)
	}()
}

// GetOrderbook with *pair support
func (h *OrderHandler) GetOrderbook(c *gin.Context) {
	pair := strings.TrimPrefix(c.Param("pair"), "/")
	engine := h.getEngine(pair)
	if engine == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported pair: " + pair})
		return
	}
	ob := engine.OrderBook
	if ob == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "orderbook not available"})
		return
	}
	depth := ob.Depth(100)
	// If the matching engine has no resting orders (fresh deployment, no
	// traders yet), fall back to a real L2 book from Binance so the trading UI
	// shows live market depth instead of a blank panel. The SeqNo stays at the
	// engine's value so clients can still detect real matching-engine updates.
	if len(depth.Bids) == 0 && len(depth.Asks) == 0 && h.prices != nil {
		if md, err := h.prices.MarketDepth(pair, 100); err == nil && md != nil && (len(md.Bids) > 0 || len(md.Asks) > 0) {
			c.JSON(http.StatusOK, externalDepthToJSON(md, depth.SeqNo))
			return
		}
	}
	c.JSON(http.StatusOK, matching.OrderBookDepth{Pair: pair, Bids: depth.Bids, Asks: depth.Asks, SeqNo: depth.SeqNo})
}

// externalDepthToJSON converts a market.Depth (external reference book) into the
// same JSON shape the matching engine emits via OrderBookDepth.MarshalJSON, so
// the frontend's Orderbook type renders it without any client-side special
// casing. Prices/quantities are stringified to match OrderbookLevel.
func externalDepthToJSON(md *market.Depth, seqNo uint64) gin.H {
	bids := make([]gin.H, 0, len(md.Bids))
	for _, l := range md.Bids {
		bids = append(bids, gin.H{"price": safeFloatStr(l.Price), "quantity": safeFloatStr(l.Quantity), "count": 1})
	}
	asks := make([]gin.H, 0, len(md.Asks))
	for _, l := range md.Asks {
		asks = append(asks, gin.H{"price": safeFloatStr(l.Price), "quantity": safeFloatStr(l.Quantity), "count": 1})
	}
	return gin.H{"pair": md.Pair, "bids": bids, "asks": asks, "seq": seqNo}
}

// GetTrades with *pair support
func (h *OrderHandler) GetTrades(c *gin.Context) {
	pair := strings.TrimPrefix(c.Param("pair"), "/")
	engine := h.getEngine(pair)
	if engine == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported pair"})
		return
	}
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	var trades []*matching.Trade
	if h.store != nil {
		trades, _ = h.store.GetTrades(pair, limit)
	}
	// Fall back to Binance's recent trade tape when the matching engine has no
	// recorded fills, so the recent-trades panel shows live market activity
	// instead of staying blank on a fresh deployment.
	if len(trades) == 0 && h.prices != nil {
		if rt, err := h.prices.RecentTrades(pair, limit); err == nil && len(rt) > 0 {
			out := make([]gin.H, 0, len(rt))
			for _, t := range rt {
				side := "sell"
				if t.IsBuyer {
					side = "buy"
				}
				out = append(out, gin.H{
					"pair":     t.Pair,
					"price":    safeFloatStr(t.Price),
					"quantity": safeFloatStr(t.Quantity),
					"time":     t.Time * int64(1e6),
					"side":     side,
				})
			}
			c.JSON(http.StatusOK, gin.H{"trades": out})
			return
		}
	}
	if trades == nil {
		trades = make([]*matching.Trade, 0)
	}
	out := make([]gin.H, len(trades))
	for i, t := range trades {
		out[i] = tradeToJSON(t)
	}
	c.JSON(http.StatusOK, gin.H{"trades": out})
}

// GetTicker with *pair support
func (h *OrderHandler) GetTicker(c *gin.Context) {
	pair := strings.TrimPrefix(c.Param("pair"), "/")
	engine := h.getEngine(pair)
	if engine == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported pair: " + pair})
		return
	}
	if engine.MD != nil {
		var bid, ask *big.Float
		if b := engine.OrderBook.BestBid(); b != nil {
			bid = b.Price
		}
		if a := engine.OrderBook.BestAsk(); a != nil {
			ask = a.Price
		}
		t := engine.MD.Ticker(bid, ask)
		c.JSON(http.StatusOK, tickerToJSON(t))
		return
	}
	// fallback: top-of-book
	bestBid := engine.OrderBook.BestBid()
	bestAsk := engine.OrderBook.BestAsk()
	if bestBid == nil || bestAsk == nil {
		c.JSON(http.StatusOK, gin.H{"pair": pair})
		return
	}
	c.JSON(http.StatusOK, gin.H{"pair": pair, "bid": safeFloatStr(bestBid.Price), "ask": safeFloatStr(bestAsk.Price)})
}

// PlaceOrder accepts a spot order, validates inputs, pre-reserves collateral
// from the user's wallet, then submits to the matching engine. The full
// end-to-end flow: HTTP -> reserve -> persist -> exchange.SubmitOrder
// (risk + WAL + engine) -> HTTP 200 with the canonical order JSON.
func (h *OrderHandler) PlaceOrder(c *gin.Context) {
	uid, _ := c.Get("user_id")
	userID, _ := uid.(string)
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "user not authenticated"})
		return
	}
	var r placeOrderReq
	if err := c.ShouldBindJSON(&r); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}
	pair := normalizePair(r.Pair)
	engine := h.getEngine(pair)
	if engine == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported pair: " + r.Pair})
		return
	}
	var side matching.Side
	switch strings.ToLower(r.Side) {
	case "buy":
		side = matching.Buy
	case "sell":
		side = matching.Sell
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "side must be buy or sell"})
		return
	}
	var oType matching.OrderType
	switch strings.ToLower(r.Type) {
	case "limit":
		oType = matching.Limit
	case "market":
		oType = matching.Market
	case "ioc":
		oType = matching.ImmediateOrCancel
	case "fok":
		oType = matching.FillOrKill
	case "post_only":
		oType = matching.PostOnly
	case "stop_loss":
		oType = matching.StopLoss
	case "stop_limit":
		oType = matching.StopLimit
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported order type"})
		return
	}
	qty, ok := parseBigFloat(r.Quantity)
	if !ok || qty.Sign() <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "quantity must be positive"})
		return
	}
	needsPrice := oType == matching.Limit || oType == matching.StopLimit || oType == matching.PostOnly || oType == matching.Iceberg
	var price *big.Float
	if needsPrice || r.Price != "" {
		price, ok = parseBigFloat(r.Price)
		if !ok || price.Sign() <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "price must be positive"})
			return
		}
	}
	var stopPx *big.Float
	if r.StopPx != "" {
		stopPx, ok = parseBigFloat(r.StopPx)
		if !ok || stopPx.Sign() <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "stop_price must be positive"})
			return
		}
	}
	var tif matching.TimeInForce
	switch strings.ToLower(r.TIF) {
	case "", "gtc":
		tif = matching.GTC
	case "ioc":
		tif = matching.IOC
	case "fok":
		tif = matching.FOK
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported time_in_force"})
		return
	}

	o := matching.NewOrder(userID, pair, side, oType, price, qty)
	o.StopPrice = stopPx
	o.TimeInForce = tif

	// Reserve collateral from the wallet and register the reservation under
	// the order ID so cancel/fill paths (ReleaseOrder / SettleFill) can
	// unwind exactly what was locked:
	//   - limit buy:  price*qty*(1+takerFee) of quote (worst case)
	//   - sells:      qty of base
	//   - market buy: NOT pre-locked (price unknown); the wallet service
	//     settles it on fill, so no nil-price multiplication happens here.
	// On rejection below we roll the reservation back via ReleaseOrder.
	if h.wallet != nil {
		if err := h.wallet.ReserveOrder(o.ID, userID, pair, int(side), int(oType), price, qty); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "insufficient balance: " + err.Error()})
			return
		}
	}

	if h.store != nil {
		_ = h.store.Save(o)
	}
	if h.exchange != nil {
		if err := h.exchange.SubmitOrder(o); err != nil {
			// Roll back the wallet reservation on rejection (risk check etc.).
			if h.wallet != nil && h.releaser != nil {
				_ = h.releaser.ReleaseOrder(o.ID, userID)
			}
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
	} else if !engine.SubmitOrder(o) {
		if h.wallet != nil && h.releaser != nil {
			_ = h.releaser.ReleaseOrder(o.ID, userID)
		}
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "matching engine busy, please retry"})
		return
	}
	c.JSON(http.StatusOK, orderToJSON(o))
}

// CancelOrder cancels a single spot order owned by the current user.
func (h *OrderHandler) CancelOrder(c *gin.Context) {
	uid, _ := c.Get("user_id")
	userID, _ := uid.(string)
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "user not authenticated"})
		return
	}
	id := c.Param("id")
	pair := c.Query("pair")
	if pair == "" {
		pair = h.findOrderPair(userID, id)
	}
	if pair == "" {
		c.JSON(http.StatusNotFound, gin.H{"error": "order not found"})
		return
	}
	pair = normalizePair(pair)
	if h.exchange != nil {
		if _, err := h.exchange.CancelOrder(id, userID, pair); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
	} else if engine := h.getEngine(pair); engine != nil {
		if _, err := engine.Cancel(id, userID); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
	} else {
		c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported pair: " + pair})
		return
	}
	if h.releaser != nil {
		_ = h.releaser.ReleaseOrder(id, userID)
	}
	o, _ := h.store.Get(id)
	if o == nil {
		c.JSON(http.StatusOK, gin.H{"status": "cancelled", "id": id})
		return
	}
	c.JSON(http.StatusOK, orderToJSON(o))
}

// CancelAllOrders cancels every open order owned by the current user across
// every supported pair.
func (h *OrderHandler) CancelAllOrders(c *gin.Context) {
	uid, _ := c.Get("user_id")
	userID, _ := uid.(string)
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "user not authenticated"})
		return
	}
	cancelled := 0
	for _, engine := range h.allEngines() {
		orders := engine.OrderBook.GetOrdersByUser(userID)
		for _, o := range orders {
			if _, err := engine.Cancel(o.ID, userID); err == nil {
				cancelled++
				if h.releaser != nil {
					_ = h.releaser.ReleaseOrder(o.ID, userID)
				}
			}
		}
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok", "cancelled": cancelled})
}

// findOrderPair scans every engine's order book for an order owned by userID
// with the given id. Returns the pair name on success, "" if not found.
func (h *OrderHandler) findOrderPair(userID, id string) string {
	for _, engine := range h.allEngines() {
		orders := engine.OrderBook.GetOrdersByUser(userID)
		for _, o := range orders {
			if o.ID == id {
				return o.Pair
			}
		}
	}
	return ""
}

// allEngines returns every registered matching engine, using the exchange
// facade if present, otherwise the local map.
func (h *OrderHandler) allEngines() map[string]*matching.MatchingEngine {
	if h.exchange != nil {
		return h.exchange.Engines()
	}
	return h.engines
}

type placeOrderReq struct {
	Pair     string `json:"pair" binding:"required"`
	Side     string `json:"side" binding:"required"`
	Type     string `json:"type" binding:"required"`
	Price    string `json:"price"`
	StopPx   string `json:"stop_price"`
	Quantity string `json:"quantity" binding:"required"`
	TPPrice  string `json:"tp_price"`
	SLPrice  string `json:"sl_price"`
	TIF      string `json:"time_in_force"`
}

// normalizePair upper-cases the pair and trims whitespace so callers can be
// forgiving with formatting.
func normalizePair(p string) string {
	return strings.ToUpper(strings.TrimSpace(p))
}

// GetOrder returns a single spot order. The order must belong to the
// authenticated user; foreign or unknown ids both yield 404 so order
// existence is never leaked to non-owners.
func (h *OrderHandler) GetOrder(c *gin.Context) {
	uid, _ := c.Get("user_id")
	userID, _ := uid.(string)
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "user not authenticated"})
		return
	}
	id := c.Param("id")
	if h.store == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "order store unavailable"})
		return
	}
	o, err := h.store.Get(id)
	if err != nil || o == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "order not found"})
		return
	}
	if o.UserID != userID {
		// 404 (not 403) to avoid revealing that the order exists.
		c.JSON(http.StatusNotFound, gin.H{"error": "order not found"})
		return
	}
	c.JSON(http.StatusOK, orderToJSON(o))
}

// ListOrders returns the authenticated user's spot orders.
func (h *OrderHandler) ListOrders(c *gin.Context) {
	uid, _ := c.Get("user_id")
	userID, _ := uid.(string)
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "user not authenticated"})
		return
	}
	pair := c.Query("pair")
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "100"))
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))
	if h.store == nil {
		c.JSON(http.StatusOK, gin.H{"orders": []interface{}{}})
		return
	}
	orders, _ := h.store.ListByUser(userID, pair, matching.New, limit, offset)
	if orders == nil {
		orders = []*matching.Order{}
	}
	out := make([]gin.H, len(orders))
	for i, o := range orders {
		out[i] = orderToJSON(o)
	}
	c.JSON(http.StatusOK, gin.H{"orders": out})
}

// ListTickers returns all 24h tickers.
func (h *OrderHandler) ListTickers(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"tickers": []interface{}{}})
}

func candleToJSON(cd *matching.Candle) gin.H {
	return gin.H{
		"pair":       cd.Pair,
		"interval":   cd.Interval,
		"open":       safeFloatStr(cd.Open),
		"high":       safeFloatStr(cd.High),
		"low":        safeFloatStr(cd.Low),
		"close":      safeFloatStr(cd.Close),
		"volume":     safeFloatStr(cd.Volume),
		"timestamp":  cd.Timestamp,
		"close_time": cd.CloseTime,
	}
}

func tradeToJSON(t *matching.Trade) gin.H {
	// `time` and `side` mirror the frontend's Trade type (RecentTrades.tsx reads
	// t.time / t.side). `created_at` and `taker_side` are kept for any
	// programmatic consumers that expect the engine's raw field names. Both
	// `time` and `created_at` are nanoseconds (time.Now().UnixNano()), which is
	// what the frontend's formatTime() expects (it divides by 1e6 to get ms).
	return gin.H{
		"id":            t.ID,
		"buy_order_id":  t.BuyOrderID,
		"sell_order_id": t.SellOrderID,
		"buyer_id":      t.BuyerID,
		"seller_id":     t.SellerID,
		"pair":          t.Pair,
		"price":         safeFloatStr(t.Price),
		"quantity":      safeFloatStr(t.Quantity),
		"side":          t.TakerSide.String(),
		"taker_side":    t.TakerSide.String(),
		"fee":           safeFloatStr(t.Fee),
		"time":          t.CreatedAt,
		"created_at":    t.CreatedAt,
	}
}

func tickerToJSON(t *matching.Ticker) gin.H {
	return gin.H{
		"pair":             t.Pair,
		"last":             safeFloatStr(t.LastPrice),
		"bid":              safeFloatStr(t.Bid),
		"ask":              safeFloatStr(t.Ask),
		"spread":           safeFloatStr(t.Spread),
		"volume_24h":       safeFloatStr(t.Volume24H),
		"quote_volume_24h": safeFloatStr(t.QuoteVolume24H),
		"high_24h":         safeFloatStr(t.High24H),
		"low_24h":          safeFloatStr(t.Low24H),
		"open_24h":         safeFloatStr(t.Open24H),
		"change_24h":       safeFloatStr(t.Change24H),
		"change_pct_24h":   safeFloatStr(t.ChangePct24H),
		"timestamp":        t.Timestamp,
	}
}

func orderToJSON(o *matching.Order) gin.H {
	return gin.H{
		"id":              o.ID,
		"client_order_id": o.ClientOrderID,
		"user_id":         o.UserID,
		"pair":            o.Pair,
		"side":            o.Side.String(),
		"type":            o.Type.String(),
		"price":           safeFloatStr(o.Price),
		"stop_price":      safeFloatStr(o.StopPrice),
		"quantity":        safeFloatStr(o.Quantity),
		"filled_qty":      safeFloatStr(o.FilledQty),
		"remaining_qty":   safeFloatStr(o.RemainingQty),
		"time_in_force":   timeInForceToString(o.TimeInForce),
		"status":          o.Status.String(),
		"created_at":      o.CreatedAt,
		"updated_at":      o.UpdatedAt,
	}
}

func timeInForceToString(tif matching.TimeInForce) string {
	switch tif {
	case matching.GTC:
		return "gtc"
	case matching.IOC:
		return "ioc"
	case matching.FOK:
		return "fok"
	case matching.GTD:
		return "gtd"
	default:
		return "unknown"
	}
}
