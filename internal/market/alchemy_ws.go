package market

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"math/big"
	"net/http"
	"sync"
	"time"
	"github.com/gorilla/websocket"
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
	for _, c := range []*ChainConfig{
		{"Ethereum", "ETH", fmt.Sprintf("https://eth-mainnet.g.alchemy.com/v2/%s", apiKey), fmt.Sprintf("wss://eth-mainnet.g.alchemy.com/v2/%s", apiKey), true, 18},
		{"Polygon", "POLYGON", fmt.Sprintf("https://polygon-mainnet.g.alchemy.com/v2/%s", apiKey), fmt.Sprintf("wss://polygon-mainnet.g.alchemy.com/v2/%s", apiKey), true, 18},
		{"Arbitrum", "ARB", fmt.Sprintf("https://arb-mainnet.g.alchemy.com/v2/%s", apiKey), fmt.Sprintf("wss://arb-mainnet.g.alchemy.com/v2/%s", apiKey), true, 18},
		{"Optimism", "OP", fmt.Sprintf("https://opt-mainnet.g.alchemy.com/v2/%s", apiKey), fmt.Sprintf("wss://opt-mainnet.g.alchemy.com/v2/%s", apiKey), true, 18},
	} { amc.Chains[c.Symbol] = c }
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
	if !ok { return nil, fmt.Errorf("unknown: %s", symbol) }
	conn, _, err := websocket.DefaultDialer.Dial(c.WSURL, nil)
	if err != nil { return nil, fmt.Errorf("dial %s: %w", symbol, err) }
	wsc := &AlchemyWSConn{conn: conn, url: c.WSURL, done: make(chan struct{}), subs: make(map[int]string), msgCh: make(chan []byte, 1000)}
	amc.mu.Lock()
	amc.conns[symbol] = wsc
	amc.mu.Unlock()
	log.Printf("[alchemy-ws] %s connected", symbol)
	go func() {
		defer conn.Close()
		for {
			_, msg, err := conn.ReadMessage()
			if err != nil { select { case <-wsc.done: return; default: log.Printf("[alchemy-ws] %s disconnect: %v", symbol, err); return } }
			select { case wsc.msgCh <- msg: default: }
		}
	}()
	return wsc, nil
}

func (wsc *AlchemyWSConn) Subscribe(method string, params []interface{}) error {
	id := int(time.Now().UnixNano() & 0x7FFFFFFF)
	d, _ := json.Marshal(map[string]interface{}{"jsonrpc":"2.0","method":"eth_subscribe","params":append([]interface{}{method},params...),"id":id})
	return wsc.conn.WriteMessage(websocket.TextMessage, d)
}

func (wsc *AlchemyWSConn) SubscribeNewHeads() error { return wsc.Subscribe("newHeads", nil) }

func (amc *AlchemyMultiChain) StartWSBlockMonitor() {
	for sym, c := range amc.Chains {
		if !c.Enabled { continue }
		go func(s string) {
			wsc, err := amc.ConnectWS(s)
			if err != nil { log.Printf("[alchemy] %s ws failed: %v", s, err); return }
			wsc.SubscribeNewHeads()
			log.Printf("[alchemy] %s subscribed to newHeads (WS)", s)
			for msg := range wsc.msgCh {
				var m wsMsg
				json.Unmarshal(msg, &m)
				if m.Method == "eth_subscription" {
					var p struct{ Subscription string; Result json.RawMessage }
					json.Unmarshal(m.Params, &p)
					var h struct{ Number string }
					json.Unmarshal(p.Result, &h)
					if h.Number != "" { var n uint64; fmt.Sscanf(h.Number, "0x%x", &n); log.Printf("[alchemy] %s block %d", s, n) }
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
	if err != nil { log.Printf("[ws-prices] seed failed: %v", err); return pf }
	defer resp.Body.Close()
	var r struct{ Data []struct{ Symbol string; Prices []struct{ Value string } } }
	json.NewDecoder(resp.Body).Decode(&r)
	m := map[string]string{"BTC":"BTC/USDT","ETH":"ETH/USDT","SOL":"SOL/USDT","BNB":"BNB/USDT","ADA":"ADA/USDT","DOGE":"DOGE/USDT","XRP":"XRP/USDT","UNI":"UNI/USDT","LINK":"LINK/USDT","MATIC":"MATIC/USDT","ARB":"ARB/USDT","OP":"OP/USDT","AAVE":"AAVE/USDT","CRV":"CRV/USDT"}
	for _, item := range r.Data {
		pair, ok := m[item.Symbol]
		if !ok || len(item.Prices) == 0 { continue }
		v, _ := new(big.Float).SetString(item.Prices[0].Value)
		pf.prices[pair] = &Ticker{Pair: pair, Last: v, Volume24h: new(big.Float), Timestamp: time.Now().UnixMilli()}
	}
	log.Printf("[ws-prices] seeded %d pairs (1 HTTP at startup, then WS only)", len(pf.prices))
	return pf
}

func (pf *WSPriceFeed) Get(pair string) *Ticker {
	pf.mu.RLock(); defer pf.mu.RUnlock()
	if pf.prices[pair] == nil { return nil }
	cp := *pf.prices[pair]; return &cp
}

func (pf *WSPriceFeed) GetAll() map[string]*Ticker {
	pf.mu.RLock(); defer pf.mu.RUnlock()
	cp := make(map[string]*Ticker, len(pf.prices))
	for k, v := range pf.prices { cp[k] = v }
	return cp
}
