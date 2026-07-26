package websocket

import "encoding/json"

const (
	ChannelOrderbook   = "orderbook"
	ChannelTrades      = "trades"
	ChannelUserOrders  = "user.orders"
	ChannelUserBalance = "user.balance"
	ChannelUser        = "user"
	ChannelTicker      = "ticker"
)

const (
	MsgSubscribe   = "subscribe"
	MsgUnsubscribe = "unsubscribe"
	MsgSnapshot    = "snapshot"
	MsgUpdate      = "update"
	MsgError       = "error"
	MsgAuth        = "auth"
)

type Message struct {
	Type    string          `json:"type"`
	Channel string          `json:"channel,omitempty"`
	Pair    string          `json:"pair,omitempty"`
	Data    json.RawMessage `json:"data,omitempty"`
}

type Subscription struct {
	Type    string   `json:"type"`
	Channel string   `json:"channel"`
	Pairs   []string `json:"pairs,omitempty"`
}
