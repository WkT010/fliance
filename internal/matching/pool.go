package matching

import (
	"math/big"
	"sync"
)

var OrderPool = sync.Pool{New: func() interface{} { return &Order{Price: newBigFloat(), Quantity: newBigFloat(), FilledQty: newBigFloat(), RemainingQty: newBigFloat()} }}
var TradePool = sync.Pool{New: func() interface{} { return &Trade{Price: newBigFloat(), Quantity: newBigFloat(), Fee: newBigFloat()} }}

func GetOrder() *Order {
	o := OrderPool.Get().(*Order)
	o.Price.SetFloat64(0); o.Quantity.SetFloat64(0); o.FilledQty.SetFloat64(0); o.RemainingQty.SetFloat64(0)
	return o
}

func PutOrder(o *Order) {
	if o == nil { return }
	o.ID, o.UserID, o.Pair = "", "", ""
	o.Side, o.Type, o.Status, o.TimeInForce = 0, 0, 0, 0
	o.CreatedAt, o.UpdatedAt = 0, 0
	o.done = nil
	OrderPool.Put(o)
}

func GetTrade() *Trade {
	t := TradePool.Get().(*Trade)
	t.Price.SetFloat64(0); t.Quantity.SetFloat64(0); t.Fee.SetFloat64(0)
	return t
}

func PutTrade(t *Trade) {
	if t == nil { return }
	t.ID, t.BuyOrderID, t.SellOrderID, t.Pair = "", "", "", ""
	t.TakerSide, t.CreatedAt = 0, 0
	TradePool.Put(t)
}
