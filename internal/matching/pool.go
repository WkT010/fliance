package matching

import (
	"math/big"
	"sync"
)

var OrderPool = sync.Pool{New: func() interface{} { return &Order{Price: new(big.Float), Quantity: new(big.Float), FilledQty: new(big.Float), RemainingQty: new(big.Float)} }}
var TradePool = sync.Pool{New: func() interface{} { return &Trade{Price: new(big.Float), Quantity: new(big.Float), Fee: new(big.Float)} }}
var BigFloatPool = sync.Pool{New: func() interface{} { return new(big.Float) }}

func GetOrder() *Order { return OrderPool.Get().(*Order) }
func PutOrder(o *Order) {
	if o == nil { return }
	o.ID, o.UserID, o.Pair = "", "", ""
	o.Price.SetFloat64(0); o.Quantity.SetFloat64(0); o.FilledQty.SetFloat64(0); o.RemainingQty.SetFloat64(0)
	o.Side, o.Type, o.Status, o.TimeInForce = 0, 0, 0, 0
	o.CreatedAt, o.UpdatedAt = 0, 0
	OrderPool.Put(o)
}

func GetTrade() *Trade { return TradePool.Get().(*Trade) }
func PutTrade(t *Trade) {
	if t == nil { return }
	t.ID, t.BuyOrderID, t.SellOrderID, t.Pair = "", "", "", ""
	t.Price.SetFloat64(0); t.Quantity.SetFloat64(0); t.Fee.SetFloat64(0)
	t.TakerSide, t.CreatedAt = 0, 0
	TradePool.Put(t)
}
