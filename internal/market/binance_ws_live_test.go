package market

import (
	"os"
	"testing"
	"time"
)

// TestBinanceWSLiveCache connects to the real public mirror and verifies the
// in-memory caches fill. It hits live network endpoints, so it only runs when
// BINANCE_LIVE_TEST=1 is set (CI sandboxes are typically offline).
func TestBinanceWSLiveCache(t *testing.T) {
	if os.Getenv("BINANCE_LIVE_TEST") != "1" {
		t.Skip("set BINANCE_LIVE_TEST=1 to run live-network tests")
	}
	c := NewBinanceWSClient("", []string{"BTC/USDT"})
	c.Start()
	defer c.Stop()

	deadline := time.Now().Add(20 * time.Second)
	var gotTicker, gotDepth, gotTrade, gotKline bool
	for time.Now().Before(deadline) && !(gotTicker && gotDepth && gotTrade && gotKline) {
		time.Sleep(500 * time.Millisecond)
		if !gotTicker {
			if tk, _ := c.Ticker("BTC/USDT"); tk != nil {
				gotTicker = true
				t.Logf("ticker last=%s bid=%s ask=%s", tk.Last, tk.Bid, tk.Ask)
			}
		}
		if !gotDepth {
			if d, at := c.Depth("BTC/USDT"); d != nil && at > 0 {
				gotDepth = true
				t.Logf("depth levels bids=%d asks=%d", len(d.Bids), len(d.Asks))
			}
		}
		if !gotTrade {
			if rt, _ := c.RecentTrades("BTC/USDT", 5); len(rt) > 0 {
				gotTrade = true
				t.Logf("trades=%d first=%s", len(rt), rt[0].Price)
			}
		}
		if !gotKline {
			if k, _ := c.CurrentKline("BTC/USDT", "1m"); k != nil {
				gotKline = true
				t.Logf("kline 1m close=%s ts=%d", k.Close, k.Timestamp)
			}
		}
	}
	if !gotTicker {
		t.Error("ticker cache never filled")
	}
	if !gotDepth {
		t.Error("depth cache never filled")
	}
	if !gotTrade {
		t.Error("trade cache never filled")
	}
	if !gotKline {
		t.Error("kline cache never filled")
	}
}
