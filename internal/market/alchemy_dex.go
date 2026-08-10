package market

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"math/big"
	"strings"
)

const (
	UniV3QuoterETH      = "0xb27308f9F90D607463bb33eA1BeBb41C27CE5AB6"
	UniV3QuoterPolygon  = "0xb27308f9F90D607463bb33eA1BeBb41C27CE5AB6"
	UniV3QuoterArbitrum = "0xb27308f9F90D607463bb33eA1BeBb41C27CE5AB6"
	UniV3QuoterOptimism = "0xb27308f9F90D607463bb33eA1BeBb41C27CE5AB6"
)

var tokenAddresses = map[string]string{
	"ETH":  "0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2",
	"USDT": "0xdAC17F958D2ee523a2206206994597C13D831ec7",
	"USDC": "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48",
	"BTC":  "0x2260FAC5E5542a773Aa44fBCfeDf7C193bc2C599",
	"LINK": "0x514910771AF9Ca656af840dff83E8264EcF986CA",
	"UNI":  "0x1f9840a85d5aF5bf1D1762F925BDADdC4201F984",
	"AAVE": "0x7Fc66500c84A76Ad7e9c93437bFc5Ac33E2DDaE9",
	"CRV":  "0xD533a949740bb3306d119CC777fa900bA034cd52",
}

type DEXPriceProvider struct{ chain *AlchemyMultiChain }

func NewDEXPriceProvider(chain *AlchemyMultiChain) *DEXPriceProvider {
	return &DEXPriceProvider{chain: chain}
}

func (d *DEXPriceProvider) QuoteTokenInETH(symbol string) (*big.Float, error) {
	addr, ok := tokenAddresses[symbol]
	if !ok {
		return nil, fmt.Errorf("unknown: %s", symbol)
	}
	data := "f7729d0a" + // quoteExactInputSingle
		"0000000000000000000000000000000000000000000000000000000000000020" +
		padAddress(tokenAddresses["ETH"]) +
		padAddress(addr) +
		"000000000000000000000000000000000000000000000000000000000000000bb8" +
		"0000000000000000000000000000000000000000000000000de0b6b3a7640000" +
		"0000000000000000000000000000000000000000000000000000000000000000"
	result, err := d.chain.Call("ETH", "eth_call", []interface{}{map[string]interface{}{"to": UniV3QuoterETH, "data": "0x" + data}, "latest"})
	if err != nil {
		return nil, fmt.Errorf("quote: %w", err)
	}
	var h string
	json.Unmarshal(result, &h)
	n := new(big.Int)
	n.SetString(strings.TrimPrefix(h, "0x"), 16)
	p := new(big.Float).Quo(new(big.Float).SetInt(n), new(big.Float).SetFloat64(1e18))
	return p, nil
}

type DEXSwapEvent struct {
	Chain, Pool, TokenIn, TokenOut string
	AmountIn, AmountOut, Price     *big.Float
	BlockNumber                    uint64
}

var uniswapPools = map[string]string{
	"ETH/USDC": "0x88e6A0c2dDD26FEEb64F039a2c41296FcB3f5640",
	"ETH/USDT": "0x11b815efB8f581194ae79006d24E0d814B7697F6",
}

func (d *DEXPriceProvider) SubscribeToSwapLogs(chainSymbol string, cb func(*DEXSwapEvent)) error {
	wsc, err := d.chain.ConnectWS(chainSymbol)
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	for name, addr := range uniswapPools {
		wsc.Subscribe("logs", []interface{}{map[string]interface{}{"address": addr}})
		slog.Info("dex ws subscribed to pool logs", "chain", chainSymbol, "pool", name)
	}
	go func() {
		for msg := range wsc.msgCh {
			var m wsMsg
			json.Unmarshal(msg, &m)
			if m.Method != "eth_subscription" {
				continue
			}
			var p struct {
				Subscription string `json:"subscription"`
				Result       struct {
					Address, BlockNumber, Data, Topic0 string
				} `json:"result"`
			}
			json.Unmarshal(m.Params, &p)
			if p.Result.Topic0 != "0xc42079f94a6350d7e6235f29174924f928cc2ac818eb64fed8004e115fbcca67" {
				continue
			}
			d := strings.TrimPrefix(p.Result.Data, "0x")
			a0 := new(big.Int)
			a1 := new(big.Int)
			if len(d) >= 64 {
				a0.SetString(d[:64], 16)
			}
			if len(d) >= 128 {
				a1.SetString(d[64:128], 16)
			}
			price := new(big.Float).SetInt(a1)
			price.Quo(price, new(big.Float).SetInt(a0))
			var bn uint64
			fmt.Sscanf(strings.TrimPrefix(p.Result.BlockNumber, "0x"), "%x", &bn)
			if cb != nil {
				cb(&DEXSwapEvent{Chain: chainSymbol, Pool: p.Result.Address, Price: price, BlockNumber: bn})
			}
		}
	}()
	return nil
}

func padAddress(addr string) string {
	return "000000000000000000000000" + strings.TrimPrefix(addr, "0x")
}
