package matching

import (
	"math/big"
	"time"
	"github.com/google/uuid"
)

type Side int8
const (Buy Side = 1; Sell Side = -1)

type OrderType int8
const (
	Limit      OrderType = 0
	Market     OrderType = 1
	StopLoss   OrderType = 2
	StopLimit  OrderType = 3
	Iceberg    OrderType = 4
	FillOrKill OrderType = 5
	ImmediateOrCancel OrderType = 6
)

type TimeInForce int8
const (GTC TimeInForce = 0; IOC TimeInForce = 1; FOK TimeInForce = 2; GTD TimeInForce = 3)

type OrderStatus int8
const (
	New           OrderStatus = 0
	PartiallyFilled OrderStatus = 1
	Filled        OrderStatus = 2
	Cancelled     OrderStatus = 3
	Rejected      OrderStatus = 4
	Expired       OrderStatus = 5
)

type Order struct {
	ID, ClientOrderID, UserID, Pair string
	Side Side; Type OrderType
	Price, StopPrice, Quantity, FilledQty, RemainingQty, IcebergQty, VisibleQty *big.Float
	TimeInForce TimeInForce; Status OrderStatus
	CreatedAt, UpdatedAt int64
}

func NewOrder(userID, pair string, side Side, oType OrderType, price, quantity *big.Float) *Order {
	now := time.Now().UnixNano()
	rem := new(big.Float).Copy(quantity)
	return &Order{
		ID: uuid.New().String(), UserID: userID, Pair: pair,
		Side: side, Type: oType, Price: price, Quantity: quantity,
		FilledQty: big.NewFloat(0), RemainingQty: rem,
		TimeInForce: GTC, Status: New, CreatedAt: now, UpdatedAt: now,
	}
}

type Trade struct {
	ID, BuyOrderID, SellOrderID, BuyerID, SellerID, Pair string
	Price, Quantity, Fee *big.Float
	TakerSide Side
	CreatedAt int64
}

type OrderBookDepth struct {
	Pair  string
	Bids, Asks []PriceLevel
	SeqNo uint64
}

type PriceLevel struct {
	Price, Quantity *big.Float
	Count int
}
