package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/WkT010/nexa-exchange/internal/market"
)

// SetAMMSimulator wires the AMM market simulator so admin endpoints can
// start/stop it and report its running state.
func (h *AdminHandler) SetAMMSimulator(s *market.Simulator) { h.ammSim = s }

// SetAMMFeed wires the AMM price feed so admin endpoints can report per-pair
// prices and trigger a reload after seeding.
func (h *AdminHandler) SetAMMFeed(f *market.AMMPriceFeed) { h.ammFeed = f }

// SetAMMBootstrap registers the pool bootstrap function (create+seed default
// pools, then reload the feed) so the admin /amm/seed endpoint can re-run it
// without the api package depending on cmd/api-gateway's default seed list.
func (h *AdminHandler) SetAMMBootstrap(fn func() error) { h.ammBootstrap = fn }

// SeedAMM re-runs pool bootstrap (create+seed any missing default pools) and
// reloads the AMM feed so prices reflect the new state immediately.
// POST /api/v2/admin/amm/seed
func (h *AdminHandler) SeedAMM(c *gin.Context) {
	if h.ammBootstrap == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "bootstrap not configured"})
		return
	}
	if err := h.ammBootstrap(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	pairs := []string{}
	if h.ammFeed != nil {
		pairs = h.ammFeed.Pairs()
	}
	c.JSON(http.StatusOK, gin.H{"status": "seeded", "pairs": pairs})
}

// StartSimulator starts the AMM market simulator.
// POST /api/v2/admin/amm/simulator/start
func (h *AdminHandler) StartSimulator(c *gin.Context) {
	if h.ammSim == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "simulator not configured"})
		return
	}
	h.ammSim.Start()
	c.JSON(http.StatusOK, h.simulatorStatus())
}

// StopSimulator stops the AMM market simulator.
// POST /api/v2/admin/amm/simulator/stop
func (h *AdminHandler) StopSimulator(c *gin.Context) {
	if h.ammSim == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "simulator not configured"})
		return
	}
	h.ammSim.Stop()
	c.JSON(http.StatusOK, h.simulatorStatus())
}

// SimulatorStatus returns the simulator running state and the current mid
// price for every pool the feed knows about.
// GET /api/v2/admin/amm/simulator
func (h *AdminHandler) SimulatorStatus(c *gin.Context) {
	c.JSON(http.StatusOK, h.simulatorStatus())
}

func (h *AdminHandler) simulatorStatus() gin.H {
	out := gin.H{}
	if h.ammSim != nil {
		out["running"] = h.ammSim.IsRunning()
		out["interval_ms"] = h.ammSim.Interval().Milliseconds()
		out["configured"] = true
	} else {
		out["running"] = false
		out["configured"] = false
	}
	prices := gin.H{}
	if h.ammFeed != nil {
		for _, pair := range h.ammFeed.Pairs() {
			if p := h.ammFeed.Price(pair); p != nil {
				prices[pair] = p.Text('f', 8)
			}
		}
	}
	out["prices"] = prices
	return out
}
