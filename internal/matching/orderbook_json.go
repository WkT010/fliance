package matching

import (
	"encoding/json"
	"math/big"
)

// bigFloatJSONStr renders a *big.Float as a JSON string number, matching the
// shape the frontend expects (OrderbookLevel.price / .quantity are strings).
// Returns "0" for nil so callers don't have to null-check every level.
func bigFloatJSONStr(f *big.Float) string {
	if f == nil {
		return "0"
	}
	return f.String()
}

// MarshalJSON renders a PriceLevel as {"price":"<str>","quantity":"<str>","count":N}.
// The frontend types OrderbookLevel.price and .quantity as strings, but the raw
// *big.Float fields do not implement json.Marshaler and would otherwise marshal
// as {} (all unexported fields) — breaking the order book UI. This also pins
// the field names to lowercase so they match the TypeScript OrderbookLevel type
// exactly (previous behavior emitted capitalized Go field names).
func (p PriceLevel) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Price    string `json:"price"`
		Quantity string `json:"quantity"`
		Count    int    `json:"count"`
	}{
		Price:    bigFloatJSONStr(p.Price),
		Quantity: bigFloatJSONStr(p.Quantity),
		Count:    p.Count,
	})
}

// MarshalJSON renders an OrderBookDepth as {"pair":...,"bids":[...],"asks":[...],"seq":N}.
// Field names are pinned to lowercase to match the frontend's Orderbook type
// (the raw struct emitted capitalized Pair/Bids/Asks/SeqNo, which the
// TypeScript layer read as undefined).
func (d OrderBookDepth) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Pair string       `json:"pair"`
		Bids []PriceLevel `json:"bids"`
		Asks []PriceLevel `json:"asks"`
		Seq  uint64       `json:"seq"`
	}{
		Pair: d.Pair,
		Bids: d.Bids,
		Asks: d.Asks,
		Seq:  d.SeqNo,
	})
}
