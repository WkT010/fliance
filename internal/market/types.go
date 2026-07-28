package market

import "math/big"

const (
	EventTrade     = "trade"
	EventOrderbook = "orderbook"
	EventTicker    = "ticker"
	EventCandle    = "candle"
)

type MarketEvent struct{ Type, Pair string; Data []byte; Timestamp int64 }
type Ticker struct {
	Pair                                           string
	Last, Bid, Ask, Spread                         *big.Float
	Volume24h, QuoteVolume24h                      *big.Float
	High24h, Low24h, Open24h, Change24h, ChangePct24h *big.Float
	Timestamp                                      int64
}
type Candle struct {
	Pair, Interval string
	Open, High, Low, Close, Volume *big.Float
	Timestamp int64
}
