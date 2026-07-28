package pnl

import (
	"math/big"
	"strings"
	"sync"
	"time"

	"github.com/WkT010/nexa-exchange/internal/matching"
)

// Position tracks a user's cost basis and realized PnL for one asset.
type Position struct {
	Asset         string
	Qty           *big.Float // positive = long, negative = short
	AvgCost       *big.Float // average entry price in quote asset
	RealizedPnL   *big.Float // cumulative realized profit/loss in quote asset
	TodayRealized *big.Float // realized PnL since UTC midnight
	LastFillTime  int64
}

// Summary is the user-facing PnL snapshot.
type Summary struct {
	UserID           string                `json:"user_id"`
	Date             string                `json:"date"`
	TodayRealized    *big.Float            `json:"today_realized"`
	TotalRealized    *big.Float            `json:"total_realized"`
	Unrealized       *big.Float            `json:"unrealized"`
	Positions        map[string]*Position  `json:"positions"`
	ReferencePrices  map[string]*big.Float `json:"reference_prices"`
}

// Service tracks realized PnL from fill events. It is safe for concurrent use.
type Service struct {
	mu        sync.RWMutex
	positions map[string]*Position // key: userID:asset
	daily     map[string]*big.Float // key: userID:date
}

func NewService() *Service {
	return &Service{
		positions: make(map[string]*Position),
		daily:     make(map[string]*big.Float),
	}
}

// RecordFill updates positions and realized PnL for both counterparties.
func (s *Service) RecordFill(fill *matching.FillNotification) {
	if fill == nil || fill.Price == nil || fill.Quantity == nil {
		return
	}
	parts := strings.Split(fill.Pair, "/")
	if len(parts) != 2 {
		return
	}
	base, quote := parts[0], parts[1]
	now := time.Now().UTC()
	date := now.Format("2006-01-02")

	// Taker side determines who bought the base asset.
	// Side is from taker's perspective: Buy means taker bought base.
	takerSide := fill.Side
	s.updateUser(fill.TakerUserID, base, quote, takerSide, fill.Price, fill.Quantity, date)
	makerSide := matching.Sell
	if takerSide == matching.Sell {
		makerSide = matching.Buy
	}
	s.updateUser(fill.MakerUserID, base, quote, makerSide, fill.Price, fill.Quantity, date)
}

func (s *Service) updateUser(userID, base, quote string, side matching.Side, price, qty *big.Float, date string) {
	if userID == "" {
		return
	}
	// Buying base asset: +base qty, -quote cost.
	// Selling base asset: -base qty, +quote proceeds.
	var qtyDelta *big.Float
	if side == matching.Buy {
		qtyDelta = new(big.Float).Copy(qty)
	} else {
		qtyDelta = new(big.Float).Neg(qty)
	}
	costDelta := new(big.Float).Mul(qtyDelta, price) // signed quote-asset flow

	posKey := userID + ":" + base
	s.mu.Lock()
	defer s.mu.Unlock()

	pos, ok := s.positions[posKey]
	if !ok {
		pos = &Position{
			Asset:         base,
			Qty:           new(big.Float),
			AvgCost:       new(big.Float),
			RealizedPnL:   new(big.Float),
			TodayRealized: new(big.Float),
		}
		s.positions[posKey] = pos
	}

	// Realized PnL only occurs when reducing an existing position.
	// If going from long to longer (or short to shorter), just update average cost.
	oldQty := pos.Qty
	newQty := new(big.Float).Add(oldQty, qtyDelta)

	if (oldQty.Sign() > 0 && qtyDelta.Sign() < 0) || (oldQty.Sign() < 0 && qtyDelta.Sign() > 0) {
		// Closing/reducing portion.
		closedQty := new(big.Float).Abs(qtyDelta)
		if new(big.Float).Abs(oldQty).Cmp(closedQty) < 0 {
			closedQty = new(big.Float).Abs(oldQty)
		}
		realized := new(big.Float).Mul(closedQty, new(big.Float).Sub(price, pos.AvgCost))
		if oldQty.Sign() < 0 {
			realized.Neg(realized) // short: profit when buy price < avg sell price
		}
		if side == matching.Sell {
			realized.Neg(realized) // flip to quote-asset PnL convention
		}
		pos.RealizedPnL.Add(pos.RealizedPnL, realized)
		s.addDaily(userID, date, realized)
	}

	// Update average cost for remaining position.
	oldCost := new(big.Float).Mul(oldQty, pos.AvgCost)
	newCost := new(big.Float).Add(oldCost, costDelta)
	if newQty.Sign() != 0 {
		pos.AvgCost = new(big.Float).Quo(newCost, newQty)
	} else {
		pos.AvgCost = new(big.Float)
	}
	pos.Qty = newQty
	pos.LastFillTime = time.Now().UTC().UnixNano()
}

func (s *Service) addDaily(userID, date string, amount *big.Float) {
	key := userID + ":" + date
	d, ok := s.daily[key]
	if !ok {
		d = new(big.Float)
		s.daily[key] = d
	}
	d.Add(d, amount)
}

// Summary returns the user's PnL snapshot using the provided reference prices.
func (s *Service) Summary(userID string, refs map[string]*big.Float) *Summary {
	now := time.Now().UTC()
	date := now.Format("2006-01-02")
	key := userID + ":" + date

	s.mu.RLock()
	defer s.mu.RUnlock()

	summary := &Summary{
		UserID:          userID,
		Date:            date,
		TodayRealized:   new(big.Float),
		TotalRealized:   new(big.Float),
		Unrealized:      new(big.Float),
		Positions:       make(map[string]*Position),
		ReferencePrices: make(map[string]*big.Float),
	}
	if d, ok := s.daily[key]; ok {
		summary.TodayRealized.Copy(d)
	}

	prefix := userID + ":"
	for k, pos := range s.positions {
		if !strings.HasPrefix(k, prefix) {
			continue
		}
		asset := pos.Asset
		cp := &Position{
			Asset:         asset,
			Qty:           new(big.Float).Copy(pos.Qty),
			AvgCost:       new(big.Float).Copy(pos.AvgCost),
			RealizedPnL:   new(big.Float).Copy(pos.RealizedPnL),
			TodayRealized: new(big.Float).Copy(summary.TodayRealized),
			LastFillTime:  pos.LastFillTime,
		}
		summary.TotalRealized.Add(summary.TotalRealized, pos.RealizedPnL)
		if ref, ok := refs[asset]; ok && ref != nil {
			summary.ReferencePrices[asset] = new(big.Float).Copy(ref)
			unrealized := new(big.Float).Mul(pos.Qty, new(big.Float).Sub(ref, pos.AvgCost))
			summary.Unrealized.Add(summary.Unrealized, unrealized)
		}
		summary.Positions[asset] = cp
	}
	return summary
}
