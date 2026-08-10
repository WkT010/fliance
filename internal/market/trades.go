package market

import (
	"encoding/json"
	"fmt"
	"math/big"
)

// RecentTrade is a historical trade sourced from an external market (e.g.
// Binance aggTrades). It mirrors the fields the frontend's Trade type expects
// (pair/price/quantity/time/side) so the recent-trades panel can render
// reference activity when the matching engine has no recorded fills.
type RecentTrade struct {
	Pair     string
	Price    *big.Float
	Quantity *big.Float
	Time     int64 // unix milliseconds
	IsBuyer  bool  // true => taker is buyer (aggressive buy)
}

// FetchRecentTrades pulls recent executed trades from Binance's aggTrades
// endpoint for `pair`. aggTrades aggregates fills at the same price/second, so
// it is lighter and more stable than the raw trades stream. `limit` is clamped
// to [1, 1000] (Binance's max).
func (b *BinancePriceFeed) FetchRecentTrades(pair string, limit int) ([]RecentTrade, error) {
	sym, ok := supportedPairs[pair]
	if !ok {
		return nil, fmt.Errorf("unsupported pair: %s", pair)
	}
	if limit <= 0 || limit > 1000 {
		limit = 50
	}
	resp, err := b.get(fmt.Sprintf("/api/v3/aggTrades?symbol=%s&limit=%d", sym, limit))
	if err != nil {
		return nil, fmt.Errorf("binance trades: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("binance trades: status %d", resp.StatusCode)
	}
	var raw []struct {
		Price        string `json:"p"`
		Qty          string `json:"q"`
		Time         int64  `json:"T"`
		IsBuyerMaker bool   `json:"m"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("binance trades decode: %w", err)
	}
	out := make([]RecentTrade, 0, len(raw))
	for _, r := range raw {
		price, ok1 := new(big.Float).SetString(r.Price)
		qty, ok2 := new(big.Float).SetString(r.Qty)
		if !ok1 || !ok2 {
			continue
		}
		// Binance "isBuyerMaker" == true means the buyer was the passive maker,
		// i.e. the aggressive taker was a seller. Flip to derive the taker side.
		out = append(out, RecentTrade{
			Pair:     pair,
			Price:    price,
			Quantity: qty,
			Time:     r.Time,
			IsBuyer:  !r.IsBuyerMaker,
		})
	}
	return out, nil
}
