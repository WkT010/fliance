package grpc

import (
	"context"
	"fmt"
	"log"
	"math/big"
	"net"
	"time"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
	pb "github.com/WkT010/nexa-exchange/proto/exchange/v1"
	"github.com/WkT010/nexa-exchange/internal/matching"
)

type MatchingServer struct {
	pb.UnimplementedExchangeServiceServer
	engines map[string]*matching.MatchingEngine
}

func NewMatchingServer(engines map[string]*matching.MatchingEngine) *MatchingServer { return &MatchingServer{engines: engines} }

func (s *MatchingServer) PlaceOrder(ctx context.Context, req *pb.PlaceOrderRequest) (*pb.PlaceOrderResponse, error) {
	e, ok := s.engines[req.Pair]
	if !ok { return nil, fmt.Errorf("no engine: %s", req.Pair) }
	side := matching.Buy; if req.Side == pb.Side_SELL { side = matching.Sell }
	var ot matching.OrderType
	switch req.Type { case pb.OrderType_MARKET: ot=matching.Market; case pb.OrderType_LIMIT: ot=matching.Limit; case pb.OrderType_FOK: ot=matching.FillOrKill; case pb.OrderType_IOC: ot=matching.ImmediateOrCancel; default: ot=matching.Limit }
	price,_ := new(big.Float).SetString(req.Price)
	qty,_ := new(big.Float).SetString(req.Quantity)
	o := matching.NewOrder(req.UserId, req.Pair, side, ot, price, qty)
	if req.TimeInForce == pb.TimeInForce_IOC { o.TimeInForce = matching.IOC }
	if !e.SubmitOrder(o) { return nil, fmt.Errorf("busy") }
	return &pb.PlaceOrderResponse{Order: &pb.Order{Id:o.ID,UserId:o.UserID,Pair:o.Pair,Side:req.Side,Type:req.Type,Price:o.Price.Text('f',8),Quantity:o.Quantity.Text('f',8),Status:pb.OrderStatus_NEW,CreatedAt:o.CreatedAt}}, nil
}

func (s *MatchingServer) CancelOrder(ctx context.Context, req *pb.CancelOrderRequest) (*pb.CancelOrderResponse, error) {
	e, ok := s.engines[req.Pair]
	if !ok { return nil, fmt.Errorf("no engine: %s", req.Pair) }
	if o := e.OrderBook.Get(req.OrderId); o != nil { e.OrderBook.Remove(req.OrderId); return &pb.CancelOrderResponse{Success: true}, nil }
	return &pb.CancelOrderResponse{Success: false}, nil
}

func (s *MatchingServer) GetOrderbook(ctx context.Context, req *pb.GetOrderbookRequest) (*pb.GetOrderbookResponse, error) {
	e, ok := s.engines[req.Pair]
	if !ok { return nil, fmt.Errorf("no engine: %s", req.Pair) }
	d := e.OrderBook.Depth(int(req.Limit))
	pbD := &pb.OrderBookDepth{Pair: d.Pair, SeqNo: int64(d.SeqNo)}
	for _, b := range d.Bids { pbD.Bids = append(pbD.Bids, &pb.PriceLevel{Price:b.Price.Text('f',8), Quantity:b.Quantity.Text('f',8), Count:int64(b.Count)}) }
	for _, a := range d.Asks { pbD.Asks = append(pbD.Asks, &pb.PriceLevel{Price:a.Price.Text('f',8), Quantity:a.Quantity.Text('f',8), Count:int64(a.Count)}) }
	return &pb.GetOrderbookResponse{Depth: pbD}, nil
}

type StreamServer struct {
	pb.UnimplementedStreamServiceServer
	engines map[string]*matching.MatchingEngine
}

func NewStreamServer(engines map[string]*matching.MatchingEngine) *StreamServer { return &StreamServer{engines: engines} }

func (s *StreamServer) StreamTrades(req *pb.StreamTradesRequest, stream pb.StreamService_StreamTradesServer) error {
	e, _ := s.engines[req.Pair]
	for {
		select {
		case t := <-e.Trades:
			stream.Send(&pb.StreamTradesResponse{Trade: &pb.Trade{Id:t.ID,Pair:t.Pair,Price:t.Price.Text('f',8),Quantity:t.Quantity.Text('f',8),CreatedAt:t.CreatedAt}})
		case <-stream.Context().Done(): return stream.Context().Err()
		}
	}
}

func (s *StreamServer) StreamOrderbook(req *pb.StreamOrderbookRequest, stream pb.StreamService_StreamOrderbookServer) error {
	e, _ := s.engines[req.Pair]
	last := e.OrderBook.Depth(100).SeqNo
	t := time.NewTicker(100 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-t.C:
			d := e.OrderBook.Depth(100)
			if d.SeqNo == last { continue }; last = d.SeqNo
			pbD := &pb.OrderBookDepth{Pair:d.Pair,SeqNo:int64(d.SeqNo)}
			for _,b := range d.Bids { pbD.Bids = append(pbD.Bids, &pb.PriceLevel{Price:b.Price.Text('f',8),Quantity:b.Quantity.Text('f',8),Count:int64(b.Count)}) }
			for _,a := range d.Asks { pbD.Asks = append(pbD.Asks, &pb.PriceLevel{Price:a.Price.Text('f',8),Quantity:a.Quantity.Text('f',8),Count:int64(a.Count)}) }
			stream.Send(&pb.StreamOrderbookResponse{Depth: pbD})
		case <-stream.Context().Done(): return stream.Context().Err()
		}
	}
}

func StartGRPCServer(addr string, engines map[string]*matching.MatchingEngine) error {
	lis, err := net.Listen("tcp", addr)
	if err != nil { return fmt.Errorf("listen: %w", err) }
	s := grpc.NewServer()
	pb.RegisterExchangeServiceServer(s, NewMatchingServer(engines))
	pb.RegisterStreamServiceServer(s, NewStreamServer(engines))
	reflection.Register(s)
	log.Printf("[gRPC] listening on %s", addr)
	return s.Serve(lis)
}
