package market

import (
	"encoding/json"
	"fmt"
	"math/big"
)

// DepthLevel is a single aggregated price level sourced from an external market
// (e.g. Binance L2 book). It mirrors matching.PriceLevel but lives in the
// market package so the market-data providers don't need to import the
// matching engine.
type DepthLevel struct {
	Price    *big.Float
	Quantity *big.Float
}

// Depth is an external (reference) order book snapshot.
type Depth struct {
	Pair  string
	Bids  []DepthLevel
	Asks  []DepthLevel
}

// FetchDepth pulls a limited L2 order book snapshot from Binance for the given
// trading pair. Binance returns bids/asks as [price, qty] string pairs; we
// parse them into *big.Float so the caller can serialize them uniformly with
// the matching engine's own levels.
//
// `limit` is clamped to Binance's supported values (5/10/20/50/100/500/1000).
// On any error (network, unknown pair, bad payload) it returns an error so the
// caller can decide whether to fall back to an empty book.
func (b *BinancePriceFeed) FetchDepth(pair string, limit int) (*Depth, error) {
	sym, ok := supportedPairs[pair]
	if !ok {
		return nil, fmt.Errorf("unsupported pair: %s", pair)
	}
	// Binance accepts only specific limit values; snap to the nearest allowed.
	allowed := []int{5, 10, 20, 50, 100, 500, 1000}
	chosen := 100
	for _, a := range allowed {
		if limit <= a {
			chosen = a
			break
		}
	}
	resp, err := b.get(fmt.Sprintf("/api/v3/depth?symbol=%s&limit=%d", sym, chosen))
	if err != nil {
		return nil, fmt.Errorf("binance depth: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("binance depth: status %d", resp.StatusCode)
	}
	var raw struct {
		LastUpdateID int       `json:"lastUpdateId"`
		Bids         [][]string `json:"bids"`
		Asks         [][]string `json:"asks"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("binance depth decode: %w", err)
	}
	parse := func(levels [][]string) []DepthLevel {
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
	return &Depth{
		Pair: pair,
		Bids: parse(raw.Bids),
		Asks: parse(raw.Asks),
	}, nil
}
