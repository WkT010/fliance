package grpc

import (
	"context"
	"crypto/subtle"
	"crypto/tls"
	"fmt"
	"log/slog"
	"math/big"
	"net"
	"os"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/reflection"
	"google.golang.org/grpc/status"

	"github.com/WkT010/nexa-exchange/internal/matching"
	pb "github.com/WkT010/nexa-exchange/proto/exchange/v1"
)

type MatchingServer struct {
	pb.UnimplementedExchangeServiceServer
	engines map[string]*matching.MatchingEngine
}

func NewMatchingServer(engines map[string]*matching.MatchingEngine) *MatchingServer {
	return &MatchingServer{engines: engines}
}

func protoSideToMatching(s pb.Side) matching.Side {
	if s == pb.Side_SIDE_SELL {
		return matching.Sell
	}
	return matching.Buy
}

func matchingSideToProto(s matching.Side) pb.Side {
	if s == matching.Sell {
		return pb.Side_SIDE_SELL
	}
	return pb.Side_SIDE_BUY
}

func protoTypeToMatching(t pb.OrderType) matching.OrderType {
	switch t {
	case pb.OrderType_ORDER_TYPE_MARKET:
		return matching.Market
	case pb.OrderType_ORDER_TYPE_STOP_LOSS:
		return matching.StopLoss
	case pb.OrderType_ORDER_TYPE_STOP_LIMIT:
		return matching.StopLimit
	case pb.OrderType_ORDER_TYPE_ICEBERG:
		return matching.Iceberg
	case pb.OrderType_ORDER_TYPE_FOK:
		return matching.FillOrKill
	case pb.OrderType_ORDER_TYPE_IOC:
		return matching.ImmediateOrCancel
	case pb.OrderType_ORDER_TYPE_POST_ONLY:
		return matching.PostOnly
	default:
		return matching.Limit
	}
}

func matchingTypeToProto(t matching.OrderType) pb.OrderType {
	switch t {
	case matching.Market:
		return pb.OrderType_ORDER_TYPE_MARKET
	case matching.StopLoss:
		return pb.OrderType_ORDER_TYPE_STOP_LOSS
	case matching.StopLimit:
		return pb.OrderType_ORDER_TYPE_STOP_LIMIT
	case matching.Iceberg:
		return pb.OrderType_ORDER_TYPE_ICEBERG
	case matching.FillOrKill:
		return pb.OrderType_ORDER_TYPE_FOK
	case matching.ImmediateOrCancel:
		return pb.OrderType_ORDER_TYPE_IOC
	case matching.PostOnly:
		return pb.OrderType_ORDER_TYPE_POST_ONLY
	default:
		return pb.OrderType_ORDER_TYPE_LIMIT
	}
}

func matchingStatusToProto(s matching.OrderStatus) pb.OrderStatus {
	switch s {
	case matching.New:
		return pb.OrderStatus_ORDER_STATUS_NEW
	case matching.PartiallyFilled:
		return pb.OrderStatus_ORDER_STATUS_PARTIALLY_FILLED
	case matching.Filled:
		return pb.OrderStatus_ORDER_STATUS_FILLED
	case matching.Cancelled:
		return pb.OrderStatus_ORDER_STATUS_CANCELLED
	case matching.Rejected:
		return pb.OrderStatus_ORDER_STATUS_REJECTED
	case matching.Expired:
		return pb.OrderStatus_ORDER_STATUS_EXPIRED
	default:
		return pb.OrderStatus_ORDER_STATUS_UNSPECIFIED
	}
}

func matchingTakerSideToProto(s matching.Side) pb.TakerSide {
	if s == matching.Sell {
		return pb.TakerSide_TAKER_SIDE_SELL
	}
	return pb.TakerSide_TAKER_SIDE_BUY
}

func orderToProto(o *matching.Order) *pb.Order {
	if o == nil {
		return nil
	}
	return &pb.Order{
		Id:            o.ID,
		ClientOrderId: o.ClientOrderID,
		UserId:        o.UserID,
		Pair:          o.Pair,
		Side:          matchingSideToProto(o.Side),
		Type:          matchingTypeToProto(o.Type),
		Price:         floatStr(o.Price),
		StopPrice:     floatStr(o.StopPrice),
		Quantity:      floatStr(o.Quantity),
		FilledQty:     floatStr(o.FilledQty),
		RemainingQty:  floatStr(o.RemainingQty),
		IcebergQty:    floatStr(o.IcebergQty),
		VisibleQty:    floatStr(o.VisibleQty),
		TimeInForce:   pb.TimeInForce_TIME_IN_FORCE_GTC,
		Status:        matchingStatusToProto(o.Status),
		CreatedAt:     o.CreatedAt,
		UpdatedAt:     o.UpdatedAt,
	}
}

func tradeToProto(t *matching.Trade) *pb.Trade {
	if t == nil {
		return nil
	}
	return &pb.Trade{
		Id:          t.ID,
		BuyOrderId:  t.BuyOrderID,
		SellOrderId: t.SellOrderID,
		BuyerId:     t.BuyerID,
		SellerId:    t.SellerID,
		Pair:        t.Pair,
		Price:       floatStr(t.Price),
		Quantity:    floatStr(t.Quantity),
		TakerSide:   matchingTakerSideToProto(t.TakerSide),
		Fee:         floatStr(t.Fee),
		CreatedAt:   t.CreatedAt,
	}
}

func floatStr(f *big.Float) string {
	if f == nil {
		return "0"
	}
	return f.Text('f', 8)
}

func (s *MatchingServer) PlaceOrder(ctx context.Context, req *pb.PlaceOrderRequest) (*pb.PlaceOrderResponse, error) {
	e, ok := s.engines[req.Pair]
	if !ok {
		return nil, fmt.Errorf("no engine: %s", req.Pair)
	}
	side := protoSideToMatching(req.Side)
	ot := protoTypeToMatching(req.Type)
	price, _ := new(big.Float).SetString(req.Price)
	if req.Price == "" {
		price = nil
	}
	qty, _ := new(big.Float).SetString(req.Quantity)
	o := matching.NewOrder(req.UserId, req.Pair, side, ot, price, qty)
	o.ClientOrderID = req.ClientOrderId
	if req.StopPrice != "" {
		if sp, ok := new(big.Float).SetString(req.StopPrice); ok {
			o.StopPrice = sp
		}
	}
	if req.TimeInForce == pb.TimeInForce_TIME_IN_FORCE_IOC {
		o.TimeInForce = matching.IOC
	}
	if req.TimeInForce == pb.TimeInForce_TIME_IN_FORCE_FOK {
		o.TimeInForce = matching.FOK
	}
	if ot == matching.Iceberg && req.IcebergQty != "" {
		if iq, ok := new(big.Float).SetString(req.IcebergQty); ok && iq.Sign() > 0 {
			o.IcebergQty = iq
			o.VisibleQty = new(big.Float).Copy(iq)
			if o.VisibleQty.Cmp(o.Quantity) > 0 {
				o.VisibleQty = new(big.Float).Copy(o.Quantity)
			}
		}
	}
	if !e.SubmitOrder(o) {
		return nil, fmt.Errorf("engine busy")
	}
	return &pb.PlaceOrderResponse{Order: orderToProto(o)}, nil
}

func (s *MatchingServer) CancelOrder(ctx context.Context, req *pb.CancelOrderRequest) (*pb.CancelOrderResponse, error) {
	e, ok := s.engines[req.Pair]
	if !ok {
		return nil, fmt.Errorf("no engine: %s", req.Pair)
	}
	if o := e.OrderBook.Get(req.OrderId); o != nil {
		if req.UserId != "" && o.UserID != req.UserId {
			return &pb.CancelOrderResponse{Success: false}, nil
		}
		if _, err := e.Cancel(req.OrderId, req.UserId); err != nil {
			return &pb.CancelOrderResponse{Success: false}, nil
		}
		return &pb.CancelOrderResponse{Success: true, Order: orderToProto(o)}, nil
	}
	return &pb.CancelOrderResponse{Success: false}, nil
}

func (s *MatchingServer) GetOrder(ctx context.Context, req *pb.GetOrderRequest) (*pb.GetOrderResponse, error) {
	e, ok := s.engines[req.Pair]
	if !ok {
		return nil, fmt.Errorf("no engine: %s", req.Pair)
	}
	o := e.OrderBook.Get(req.OrderId)
	if o == nil {
		return nil, fmt.Errorf("order not found: %s", req.OrderId)
	}
	return &pb.GetOrderResponse{Order: orderToProto(o)}, nil
}

func (s *MatchingServer) GetOrderbook(ctx context.Context, req *pb.GetOrderbookRequest) (*pb.GetOrderbookResponse, error) {
	e, ok := s.engines[req.Pair]
	if !ok {
		return nil, fmt.Errorf("no engine: %s", req.Pair)
	}
	limit := int(req.Limit)
	if limit <= 0 {
		limit = 100
	}
	depth := e.OrderBook.Depth(limit)
	pbDepth := &pb.OrderBookDepth{Pair: depth.Pair, SeqNo: int64(depth.SeqNo)}
	for _, b := range depth.Bids {
		pbDepth.Bids = append(pbDepth.Bids, &pb.PriceLevel{Price: floatStr(b.Price), Quantity: floatStr(b.Quantity), Count: int64(b.Count)})
	}
	for _, a := range depth.Asks {
		pbDepth.Asks = append(pbDepth.Asks, &pb.PriceLevel{Price: floatStr(a.Price), Quantity: floatStr(a.Quantity), Count: int64(a.Count)})
	}
	return &pb.GetOrderbookResponse{Depth: pbDepth}, nil
}

func (s *MatchingServer) GetTicker(ctx context.Context, req *pb.GetTickerRequest) (*pb.GetTickerResponse, error) {
	e, ok := s.engines[req.Pair]
	if !ok {
		return nil, fmt.Errorf("no engine: %s", req.Pair)
	}
	ticker := engineTicker(e)
	return &pb.GetTickerResponse{Ticker: tickerToProto(req.Pair, ticker)}, nil
}

// engineTicker builds a Ticker from the engine's market-data recorder and the
// current top-of-book. It tolerates a nil recorder (returns an empty ticker).
func engineTicker(e *matching.MatchingEngine) *matching.Ticker {
	bid := e.OrderBook.BestBid()
	ask := e.OrderBook.BestAsk()
	var bidP, askP *big.Float
	if bid != nil {
		bidP = bid.Price
	}
	if ask != nil {
		askP = ask.Price
	}
	if e.MD == nil {
		return &matching.Ticker{Pair: e.Pair, Bid: bidP, Ask: askP}
	}
	return e.MD.Ticker(bidP, askP)
}

func tickerToProto(pair string, t *matching.Ticker) *pb.Ticker {
	if t == nil {
		return &pb.Ticker{Pair: pair}
	}
	return &pb.Ticker{
		Pair:            pair,
		LastPrice:       floatStr(t.LastPrice),
		Bid:             floatStr(t.Bid),
		Ask:             floatStr(t.Ask),
		Spread:          floatStr(t.Spread),
		Volume_24H:      floatStr(t.Volume24H),
		QuoteVolume_24H: floatStr(t.QuoteVolume24H),
		High_24H:        floatStr(t.High24H),
		Low_24H:         floatStr(t.Low24H),
		Open_24H:        floatStr(t.Open24H),
		Change_24H:      floatStr(t.Change24H),
		ChangePct_24H:   floatStr(t.ChangePct24H),
		Timestamp:       t.Timestamp,
	}
}

func (s *MatchingServer) GetCandles(ctx context.Context, req *pb.GetCandlesRequest) (*pb.GetCandlesResponse, error) {
	e, ok := s.engines[req.Pair]
	if !ok {
		return nil, fmt.Errorf("no engine: %s", req.Pair)
	}
	limit := int(req.Limit)
	if limit <= 0 {
		limit = 500
	}
	var candles []*matching.Candle
	if e.MD != nil {
		candles = e.MD.Candles(req.Interval, limit, req.StartTime, req.EndTime)
	}
	out := make([]*pb.Candle, 0, len(candles))
	for _, c := range candles {
		out = append(out, &pb.Candle{
			Pair:      c.Pair,
			Interval:  c.Interval,
			Open:      floatStr(c.Open),
			High:      floatStr(c.High),
			Low:       floatStr(c.Low),
			Close:     floatStr(c.Close),
			Volume:    floatStr(c.Volume),
			Timestamp: c.Timestamp,
			CloseTime: c.CloseTime,
		})
	}
	return &pb.GetCandlesResponse{Candles: out}, nil
}

func (s *MatchingServer) ListTrades(ctx context.Context, req *pb.ListTradesRequest) (*pb.ListTradesResponse, error) {
	e, ok := s.engines[req.Pair]
	if !ok {
		return nil, fmt.Errorf("no engine: %s", req.Pair)
	}
	limit := int(req.Limit)
	if limit <= 0 {
		limit = 100
	}
	trades := e.RecentTrades(limit)
	out := make([]*pb.Trade, 0, len(trades))
	for _, t := range trades {
		out = append(out, tradeToProto(t))
	}
	return &pb.ListTradesResponse{Trades: out}, nil
}

type StreamServer struct {
	pb.UnimplementedStreamServiceServer
	engines map[string]*matching.MatchingEngine
}

func NewStreamServer(engines map[string]*matching.MatchingEngine) *StreamServer {
	return &StreamServer{engines: engines}
}

func (s *StreamServer) StreamTrades(req *pb.StreamTradesRequest, stream pb.StreamService_StreamTradesServer) error {
	e, ok := s.engines[req.Pair]
	if !ok {
		return fmt.Errorf("no engine: %s", req.Pair)
	}
	for {
		select {
		case t, ok := <-e.Trades:
			if !ok {
				return nil
			}
			if err := stream.Send(&pb.StreamTradesResponse{Trade: tradeToProto(t)}); err != nil {
				return err
			}
		case <-stream.Context().Done():
			return stream.Context().Err()
		}
	}
}

func (s *StreamServer) StreamOrderbook(req *pb.StreamOrderbookRequest, stream pb.StreamService_StreamOrderbookServer) error {
	e, ok := s.engines[req.Pair]
	if !ok {
		return fmt.Errorf("no engine: %s", req.Pair)
	}
	limit := int(req.Limit)
	if limit <= 0 {
		limit = 100
	}
	last := e.OrderBook.Depth(limit).SeqNo
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			depth := e.OrderBook.Depth(limit)
			if depth.SeqNo == last {
				continue
			}
			last = depth.SeqNo
			pbDepth := &pb.OrderBookDepth{Pair: depth.Pair, SeqNo: int64(depth.SeqNo)}
			for _, b := range depth.Bids {
				pbDepth.Bids = append(pbDepth.Bids, &pb.PriceLevel{Price: floatStr(b.Price), Quantity: floatStr(b.Quantity), Count: int64(b.Count)})
			}
			for _, a := range depth.Asks {
				pbDepth.Asks = append(pbDepth.Asks, &pb.PriceLevel{Price: floatStr(a.Price), Quantity: floatStr(a.Quantity), Count: int64(a.Count)})
			}
			if err := stream.Send(&pb.StreamOrderbookResponse{Depth: pbDepth}); err != nil {
				return err
			}
		case <-stream.Context().Done():
			return stream.Context().Err()
		}
	}
}

func (s *StreamServer) StreamTicker(req *pb.StreamTickerRequest, stream pb.StreamService_StreamTickerServer) error {
	e, ok := s.engines[req.Pair]
	if !ok {
		return fmt.Errorf("no engine: %s", req.Pair)
	}
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			t := engineTicker(e)
			if err := stream.Send(&pb.StreamTickerResponse{Ticker: tickerToProto(req.Pair, t)}); err != nil {
				return err
			}
		case <-stream.Context().Done():
			return stream.Context().Err()
		}
	}
}

// --- Authentication & TLS ---------------------------------------------------

// isDevelopment reports whether the process runs in the development
// environment (mirrors internal/config: ENVIRONMENT defaults to
// "development").
func isDevelopment() bool {
	env := strings.ToLower(strings.TrimSpace(os.Getenv("ENVIRONMENT")))
	return env == "" || env == "development"
}

// authorize checks the shared bearer token from incoming metadata in constant
// time.
func authorize(ctx context.Context, token string) error {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return status.Error(codes.Unauthenticated, "missing metadata")
	}
	vals := md.Get("authorization")
	if len(vals) == 0 {
		return status.Error(codes.Unauthenticated, "missing authorization header")
	}
	authz := strings.TrimSpace(vals[0])
	const prefix = "Bearer "
	if !strings.HasPrefix(authz, prefix) {
		return status.Error(codes.Unauthenticated, "invalid authorization format")
	}
	provided := strings.TrimPrefix(authz, prefix)
	if subtle.ConstantTimeCompare([]byte(provided), []byte(token)) != 1 {
		return status.Error(codes.Unauthenticated, "invalid token")
	}
	return nil
}

func authUnaryInterceptor(token string) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		if err := authorize(ctx, token); err != nil {
			return nil, err
		}
		return handler(ctx, req)
	}
}

func authStreamInterceptor(token string) grpc.StreamServerInterceptor {
	return func(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		if err := authorize(ss.Context(), token); err != nil {
			return err
		}
		return handler(srv, ss)
	}
}

// SecuredServerOptions builds grpc.ServerOptions enforcing shared-token
// authentication (GRPC_SHARED_TOKEN) and optional TLS (GRPC_TLS_CERT /
// GRPC_TLS_KEY). In non-development environments a missing token aborts the
// process; without TLS credentials the server serves plaintext with a warning
// (internal-network deployments only).
func SecuredServerOptions() []grpc.ServerOption {
	var opts []grpc.ServerOption
	token := strings.TrimSpace(os.Getenv("GRPC_SHARED_TOKEN"))
	if token != "" {
		opts = append(opts,
			grpc.UnaryInterceptor(authUnaryInterceptor(token)),
			grpc.StreamInterceptor(authStreamInterceptor(token)),
		)
	} else if !isDevelopment() {
		slog.Error("GRPC_SHARED_TOKEN is not set and ENVIRONMENT is not development: refusing to start without authentication")
		os.Exit(1)
	} else {
		slog.Warn("GRPC_SHARED_TOKEN not set; authentication disabled (development only)")
	}
	certFile, keyFile := os.Getenv("GRPC_TLS_CERT"), os.Getenv("GRPC_TLS_KEY")
	if certFile != "" && keyFile != "" {
		cert, err := tls.LoadX509KeyPair(certFile, keyFile)
		if err != nil {
			slog.Error("failed to load TLS keypair", "cert", certFile, "key", keyFile, "err", err)
			os.Exit(1)
		}
		opts = append(opts, grpc.Creds(credentials.NewTLS(&tls.Config{
			Certificates: []tls.Certificate{cert},
			MinVersion:   tls.VersionTLS12,
		})))
		slog.Info("gRPC TLS enabled")
	} else {
		slog.Warn("GRPC_TLS_CERT/GRPC_TLS_KEY not set; serving plaintext (internal network only)")
	}
	return opts
}

// NewSecuredServer returns a grpc.Server wired with authentication/TLS
// options derived from the environment. Prefer this over grpc.NewServer() in
// every service entrypoint.
func NewSecuredServer() *grpc.Server {
	return grpc.NewServer(SecuredServerOptions()...)
}

func StartGRPCServer(addr string, engines map[string]*matching.MatchingEngine) error {
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	s := NewSecuredServer()
	pb.RegisterExchangeServiceServer(s, NewMatchingServer(engines))
	pb.RegisterStreamServiceServer(s, NewStreamServer(engines))
	if isDevelopment() {
		reflection.Register(s)
	}
	slog.Info("gRPC listening", "addr", addr)
	return s.Serve(lis)
}
