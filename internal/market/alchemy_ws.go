package market

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/gorilla/websocket"
	"log/slog"
	"math/big"
	"net/http"
	"sync"
	"time"
)

type ChainConfig struct {
	Name, Symbol, RPCURL, WSURL string
	Enabled                     bool
	Decimals                    int
}

type AlchemyMultiChain struct {
	apiKey string
	Chains map[string]*ChainConfig
	client *http.Client
	conns  map[string]*AlchemyWSConn
	mu     sync.RWMutex
}

func NewAlchemyMultiChain(apiKey string) *AlchemyMultiChain {
	amc := &AlchemyMultiChain{apiKey: apiKey, client: &http.Client{Timeout: 15 * time.Second}, Chains: make(map[string]*ChainConfig), conns: make(map[string]*AlchemyWSConn)}
	// When no API key is provided, fall back to public RPC endpoints so the
	// product can still fetch real on-chain prices/quotes in dev/test.
	ethRPC, ethWS := fmt.Sprintf("https://eth-mainnet.g.alchemy.com/v2/%s", apiKey), fmt.Sprintf("wss://eth-mainnet.g.alchemy.com/v2/%s", apiKey)
	polygonRPC, polygonWS := fmt.Sprintf("https://polygon-mainnet.g.alchemy.com/v2/%s", apiKey), fmt.Sprintf("wss://polygon-mainnet.g.alchemy.com/v2/%s", apiKey)
	arbRPC, arbWS := fmt.Sprintf("https://arb-mainnet.g.alchemy.com/v2/%s", apiKey), fmt.Sprintf("wss://arb-mainnet.g.alchemy.com/v2/%s", apiKey)
	opRPC, opWS := fmt.Sprintf("https://opt-mainnet.g.alchemy.com/v2/%s", apiKey), fmt.Sprintf("wss://opt-mainnet.g.alchemy.com/v2/%s", apiKey)
	if apiKey == "" {
		// Public RPC fallbacks for dev/test. Production should set ALCHEMY_API_KEY.
		ethRPC = "https://ethereum-rpc.publicnode.com"
		ethWS = ""
		polygonRPC = "https://polygon-rpc.com"
		polygonWS = ""
		arbRPC = "https://arb1.arbitrum.io/rpc"
		arbWS = ""
		opRPC = "https://mainnet.optimism.io"
		opWS = ""
	}
	for _, c := range []*ChainConfig{
		{"Ethereum", "ETH", ethRPC, ethWS, true, 18},
		{"Polygon", "POLYGON", polygonRPC, polygonWS, true, 18},
		{"Arbitrum", "ARB", arbRPC, arbWS, true, 18},
		{"Optimism", "OP", opRPC, opWS, true, 18},
	} {
		amc.Chains[c.Symbol] = c
	}
	return amc
}

type AlchemyWSConn struct {
	conn  *websocket.Conn
	url   string
	done  chan struct{}
	subs  map[int]string
	mu    sync.Mutex
	msgCh chan []byte
}

type wsMsg struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	ID      *int            `json:"id,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
}

func (amc *AlchemyMultiChain) Call(symbol, method string, params []interface{}) (json.RawMessage, error) {
	amc.mu.RLock()
	c, ok := amc.Chains[symbol]
	amc.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("unknown chain: %s", symbol)
	}
	body, _ := json.Marshal(map[string]interface{}{"jsonrpc": "2.0", "method": method, "params": params, "id": 1})
	resp, err := amc.client.Post(c.RPCURL, "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("rpc call: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("rpc status %d", resp.StatusCode)
	}
	var r struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return nil, fmt.Errorf("decode rpc response: %w", err)
	}
	if r.Error != nil {
		return nil, fmt.Errorf("rpc error %d: %s", r.Error.Code, r.Error.Message)
	}
	return r.Result, nil
}

func (amc *AlchemyMultiChain) ConnectWS(symbol string) (*AlchemyWSConn, error) {
	c, ok := amc.Chains[symbol]
	if !ok {
		return nil, fmt.Errorf("unknown: %s", symbol)
	}
	conn, _, err := websocket.DefaultDialer.Dial(c.WSURL, nil)
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", symbol, err)
	}
	wsc := &AlchemyWSConn{conn: conn, url: c.WSURL, done: make(chan struct{}), subs: make(map[int]string), msgCh: make(chan []byte, 1000)}
	amc.mu.Lock()
	amc.conns[symbol] = wsc
	amc.mu.Unlock()
	slog.Info("alchemy ws connected", "chain", symbol)
	go func() {
		defer conn.Close()
		for {
			_, msg, err := conn.ReadMessage()
			if err != nil {
				select {
				case <-wsc.done:
					return
				default:
					slog.Warn("alchemy ws disconnected", "chain", symbol, "err", err)
					return
				}
			}
			select {
			case wsc.msgCh <- msg:
			default:
			}
		}
	}()
	return wsc, nil
}

func (wsc *AlchemyWSConn) Subscribe(method string, params []interface{}) error {
	id := int(time.Now().UnixNano() & 0x7FFFFFFF)
	d, _ := json.Marshal(map[string]interface{}{"jsonrpc": "2.0", "method": "eth_subscribe", "params": append([]interface{}{method}, params...), "id": id})
	return wsc.conn.WriteMessage(websocket.TextMessage, d)
}

func (wsc *AlchemyWSConn) SubscribeNewHeads() error { return wsc.Subscribe("newHeads", nil) }

func (amc *AlchemyMultiChain) StartWSBlockMonitor() {
	for sym, c := range amc.Chains {
		if !c.Enabled {
			continue
		}
		go func(s string) {
			wsc, err := amc.ConnectWS(s)
			if err != nil {
				slog.Warn("alchemy ws connect failed", "chain", s, "err", err)
				return
			}
			wsc.SubscribeNewHeads()
			slog.Info("subscribed to newHeads (WS)", "chain", s)
			for msg := range wsc.msgCh {
				var m wsMsg
				json.Unmarshal(msg, &m)
				if m.Method == "eth_subscription" {
					var p struct {
						Subscription string
						Result       json.RawMessage
					}
					json.Unmarshal(m.Params, &p)
					var h struct{ Number string }
					json.Unmarshal(p.Result, &h)
					if h.Number != "" {
						var n uint64
						fmt.Sscanf(h.Number, "0x%x", &n)
						slog.Debug("new block observed", "chain", s, "block", n)
					}
				}
			}
		}(sym)
	}
}

type WSPriceFeed struct {
	prices map[string]*Ticker
	mu     sync.RWMutex
}

func NewWSPriceFeed(apiKey string) *WSPriceFeed {
	pf := &WSPriceFeed{prices: make(map[string]*Ticker)}
	req, _ := http.NewRequest("GET",
		fmt.Sprintf("https://api.g.alchemy.com/prices/v1/tokens/by-symbol?symbols=%s",
			"BTC,ETH,SOL,BNB,ADA,DOGE,XRP,UNI,LINK,MATIC,ARB,OP,AAVE,CRV"), nil)
	req.Header.Set("Authorization", "Bearer "+apiKey)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		slog.Warn("ws-prices seed failed", "err", err)
		return pf
	}
	defer resp.Body.Close()
	var r struct {
		Data []struct {
			Symbol string
			Prices []struct{ Value string }
		}
	}
	json.NewDecoder(resp.Body).Decode(&r)
	m := map[string]string{"BTC": "BTC/USDT", "ETH": "ETH/USDT", "SOL": "SOL/USDT", "BNB": "BNB/USDT", "ADA": "ADA/USDT", "DOGE": "DOGE/USDT", "XRP": "XRP/USDT", "UNI": "UNI/USDT", "LINK": "LINK/USDT", "MATIC": "MATIC/USDT", "ARB": "ARB/USDT", "OP": "OP/USDT", "AAVE": "AAVE/USDT", "CRV": "CRV/USDT"}
	for _, item := range r.Data {
		pair, ok := m[item.Symbol]
		if !ok || len(item.Prices) == 0 {
			continue
		}
		v, _ := new(big.Float).SetString(item.Prices[0].Value)
		pf.prices[pair] = &Ticker{Pair: pair, Last: v, Volume24h: new(big.Float), Timestamp: time.Now().UnixMilli()}
	}
	slog.Info("ws-prices seeded (1 HTTP at startup, then WS only)", "pairs", len(pf.prices))
	return pf
}

func (pf *WSPriceFeed) Get(pair string) *Ticker {
	pf.mu.RLock()
	defer pf.mu.RUnlock()
	if pf.prices[pair] == nil {
		return nil
	}
	cp := *pf.prices[pair]
	return &cp
}

func (pf *WSPriceFeed) GetAll() map[string]*Ticker {
	pf.mu.RLock()
	defer pf.mu.RUnlock()
	cp := make(map[string]*Ticker, len(pf.prices))
	for k, v := range pf.prices {
		cp[k] = v
	}
	return cp
}
