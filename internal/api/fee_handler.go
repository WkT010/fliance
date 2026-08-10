package api

import (
	"fmt"
	"net/http"
	"sync"

	"github.com/gin-gonic/gin"
)

type FeeTier struct {
	Tier      int     `json:"tier"`
	MinVolume float64 `json:"min_volume"`
	MakerRate float64 `json:"maker_rate"`
	TakerRate float64 `json:"taker_rate"`
}

type FeeSchedule struct {
	Pair         string    `json:"pair"`
	DefaultMaker float64   `json:"default_maker"`
	DefaultTaker float64   `json:"default_taker"`
	Tiers        []FeeTier `json:"tiers,omitempty"`
}

type FeeHandler struct {
	mu       sync.RWMutex
	schedule map[string]*FeeSchedule
}

func NewFeeHandler() *FeeHandler {
	h := &FeeHandler{schedule: make(map[string]*FeeSchedule)}
	h.initDefaults()
	return h
}

func (h *FeeHandler) initDefaults() {
	tiers := []FeeTier{{Tier: 0, MinVolume: 0, MakerRate: 0.001, TakerRate: 0.001}}
	pairs := []string{"BTC/USDT", "ETH/USDT", "SOL/USDT", "BNB/USDT", "ADA/USDT", "DOT/USDT", "LINK/USDT", "AVAX/USDT"}
	for _, p := range pairs {
		h.schedule[p] = &FeeSchedule{Pair: p, DefaultMaker: 0.001, DefaultTaker: 0.001, Tiers: append([]FeeTier{}, tiers...)}
	}
}

func (h *FeeHandler) GetFee(pair string, volume30d float64) (maker, taker float64) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	s, ok := h.schedule[pair]
	if !ok {
		return 0.001, 0.001
	}
	maker, taker = s.DefaultMaker, s.DefaultTaker
	for _, t := range s.Tiers {
		if volume30d >= t.MinVolume && (t.MakerRate < maker || t.TakerRate < taker) {
			maker, taker = t.MakerRate, t.TakerRate
		}
	}
	return
}

func (h *FeeHandler) GetSchedule(c *gin.Context) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	schedules := make([]*FeeSchedule, 0, len(h.schedule))
	for _, s := range h.schedule {
		schedules = append(schedules, s)
	}
	c.JSON(http.StatusOK, gin.H{"schedules": schedules})
}

func (h *FeeHandler) GetPairFee(c *gin.Context) {
	pair := c.Param("pair")
	if pair == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "pair required"})
		return
	}
	h.mu.RLock()
	s, ok := h.schedule[pair]
	h.mu.RUnlock()
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "pair not found"})
		return
	}
	c.JSON(http.StatusOK, s)
}

type updateFeeReq struct {
	Pair         string     `json:"pair" binding:"required"`
	DefaultMaker *float64   `json:"default_maker"`
	DefaultTaker *float64   `json:"default_taker"`
	Tiers        *[]FeeTier `json:"tiers,omitempty"`
}

func (h *FeeHandler) UpdateFee(c *gin.Context) {
	var req updateFeeReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	s, ok := h.schedule[req.Pair]
	if !ok {
		s = &FeeSchedule{Pair: req.Pair}
		h.schedule[req.Pair] = s
	}
	if req.DefaultMaker != nil {
		s.DefaultMaker = *req.DefaultMaker
	}
	if req.DefaultTaker != nil {
		s.DefaultTaker = *req.DefaultTaker
	}
	if req.Tiers != nil {
		s.Tiers = *req.Tiers
	}
	c.JSON(http.StatusOK, gin.H{"message": "fee schedule updated", "pair": req.Pair})
}

func (h *FeeHandler) CalculateFee(c *gin.Context) {
	pair := c.Query("pair")
	amount := c.Query("amount")
	side := c.DefaultQuery("side", "market")
	if pair == "" || amount == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "pair and amount required"})
		return
	}
	var amt float64
	if _, err := fmt.Sscanf(amount, "%f", &amt); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid amount"})
		return
	}
	maker, taker := h.GetFee(pair, 0)
	rate := taker
	if side == "limit" {
		rate = maker
	}
	fee := amt * rate
	c.JSON(http.StatusOK, gin.H{"pair": pair, "amount": amt, "maker": maker, "taker": taker, "applied": rate, "fee": fee})
}
