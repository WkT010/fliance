package matching

import (
	"math/big"
	"time"

	"github.com/google/uuid"
)

// Side of an order.
type Side int8

const (
	Buy  Side = 1
	Sell Side = -1
)

func (s Side) String() string {
	if s == Buy {
		return "buy"
	}
	return "sell"
}

func (s Side) Opposite() Side {
	if s == Buy {
		return Sell
	}
	return Buy
}

// OrderType enumerates supported order types.
type OrderType int8

const (
	Limit             OrderType = 0
	Market            OrderType = 1
	StopLoss          OrderType = 2
	StopLimit         OrderType = 3
	Iceberg           OrderType = 4
	FillOrKill        OrderType = 5
	ImmediateOrCancel OrderType = 6
	PostOnly          OrderType = 7
)

// TimeInForce controls how long an order remains active.
type TimeInForce int8

const (
	GTC TimeInForce = 0
	IOC TimeInForce = 1
	FOK TimeInForce = 2
	GTD TimeInForce = 3
)

// OrderStatus enumerates the lifecycle states of an order.
type OrderStatus int8

const (
	New             OrderStatus = 0
	PartiallyFilled OrderStatus = 1
	Filled          OrderStatus = 2
	Cancelled       OrderStatus = 3
	Rejected        OrderStatus = 4
	Expired         OrderStatus = 5
)

// SelfTradePreventionMode controls how self-trades are resolved.
type SelfTradePreventionMode int8

const (
	STPDisabled    SelfTradePreventionMode = 0 // no STP (legacy behaviour)
	STPCancelTaker SelfTradePreventionMode = 1 // cancel the incoming taker
	STPCancelMaker SelfTradePreventionMode = 2 // cancel the resting maker
	STPCancelBoth  SelfTradePreventionMode = 3 // cancel both legs
)

// DefaultPrecision is the mantissa precision (in bits) used for all monetary
// big.Float values created by the matching package. 128 bits gives ~38 decimal
// digits of precision, which is more than enough for crypto-asset quantities
// and prices while keeping arithmetic deterministic across the engine.
//
// All big.Float accumulators (FilledQty, RemainingQty, trade totals, etc.) are
// initialised at this precision so Add/Sub/Mul/Quo do not silently round to
// float64 precision (53 bits), which caused 0.3 to become 0.299999999999999989.
const DefaultPrecision = 128

// newBigFloat returns a new *big.Float set to 0 with DefaultPrecision.
func newBigFloat() *big.Float {
	f := new(big.Float)
	f.SetPrec(DefaultPrecision)
	return f
}

// newBigFloatCopy returns a new *big.Float with DefaultPrecision and the value
// of x. If x has fewer bits than DefaultPrecision the value is exact; if it has
// more it is rounded (rare in practice). Returns nil if x is nil so callers can
// represent "no value" (e.g. market-order price).
func newBigFloatCopy(x *big.Float) *big.Float {
	if x == nil {
		return nil
	}
	f := newBigFloat()
	f.Set(x)
	return f
}

// Order is the canonical order representation used across the matching engine,
// persistence layer and API.
type Order struct {
	ID            string
	ClientOrderID string
	UserID        string
	Pair          string
	Side          Side
	Type          OrderType
	Price         *big.Float
	StopPrice     *big.Float
	Quantity      *big.Float
	FilledQty     *big.Float
	RemainingQty  *big.Float
	IcebergQty    *big.Float
	VisibleQty    *big.Float
	TimeInForce   TimeInForce
	Status        OrderStatus
	CreatedAt     int64
	UpdatedAt     int64
	// STP is the self-trade-prevention mode applied to this order.
	STP SelfTradePreventionMode
	// PostOnly flag derived from Type==PostOnly for fast checks during matching.
	PostOnly bool
	// bookSeq is the order-book sequence number assigned when the order enters
	// the book (addLocked / snapshot restore). It breaks price ties with strict
	// FIFO (time) priority independent of wall-clock resolution. Unexported so
	// it never leaks into JSON/WAL formats.
	bookSeq uint64
	// done, if non-nil, is closed by the engine after the order has been
	// processed. It provides a happens-before edge so callers can safely read
	// Status/FilledQty once it is closed. Used by SubmitOrderSync.
	done chan struct{}
	// Err carries a processing error (e.g. WAL append failure) back to the
	// submitter. It is written by the engine before done is closed, so it is
	// safe to read once done has fired. nil means no processing error.
	Err error
}

// NewOrder creates a new order with sensible defaults. All monetary big.Float
// fields are initialised at DefaultPrecision so subsequent arithmetic does not
// silently round to float64 precision.
func NewOrder(userID, pair string, side Side, oType OrderType, price, quantity *big.Float) *Order {
	now := time.Now().UnixNano()
	q := newBigFloatCopy(quantity)
	o := &Order{
		ID:           uuid.NewString(),
		UserID:       userID,
		Pair:         pair,
		Side:         side,
		Type:         oType,
		Price:        newBigFloatCopy(price),
		Quantity:     q,
		FilledQty:    newBigFloat(),
		RemainingQty: newBigFloatCopy(quantity),
		TimeInForce:  GTC,
		Status:       New,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if oType == PostOnly {
		o.PostOnly = true
	}
	return o
}

// Trade records a single fill between a taker and a maker.
type Trade struct {
	ID          string
	BuyOrderID  string
	SellOrderID string
	BuyerID     string
	SellerID    string
	Pair        string
	Price       *big.Float
	Quantity    *big.Float
	TakerSide   Side
	Fee         *big.Float
	CreatedAt   int64
}

// OrderBookDepth is a snapshot of the book at a point in time.
type OrderBookDepth struct {
	Pair       string
	Bids, Asks []PriceLevel
	SeqNo      uint64
}

// PriceLevel is an aggregated price level.
type PriceLevel struct {
	Price    *big.Float
	Quantity *big.Float
	Count    int
}

func (s OrderStatus) String() string {
	switch s {
	case New:
		return "new"
	case PartiallyFilled:
		return "partially_filled"
	case Filled:
		return "filled"
	case Cancelled:
		return "cancelled"
	case Rejected:
		return "rejected"
	case Expired:
		return "expired"
	default:
		return "unknown"
	}
}

func (t OrderType) String() string {
	switch t {
	case Limit:
		return "limit"
	case Market:
		return "market"
	case StopLoss:
		return "stop_loss"
	case StopLimit:
		return "stop_limit"
	case Iceberg:
		return "iceberg"
	case FillOrKill:
		return "fok"
	case ImmediateOrCancel:
		return "ioc"
	case PostOnly:
		return "post_only"
	default:
		return "unknown"
	}
}

func (m SelfTradePreventionMode) String() string {
	switch m {
	case STPCancelTaker:
		return "cancel_taker"
	case STPCancelMaker:
		return "cancel_maker"
	case STPCancelBoth:
		return "cancel_both"
	default:
		return "disabled"
	}
}

// timeNowUnixNano is a small indirection so tests can stub the clock if needed.
var timeNowUnixNano = func() int64 { return time.Now().UnixNano() }
