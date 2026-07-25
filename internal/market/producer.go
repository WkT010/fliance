package market

import (
	"context"
	"encoding/json"
	"time"
	"github.com/WkT010/nexa-exchange/internal/matching"
	"github.com/segmentio/kafka-go"
)

type Producer struct{ writer *kafka.Writer }

func NewProducer(brokers []string, topic string) *Producer {
	return &Producer{writer: &kafka.Writer{Addr: kafka.TCP(brokers...), Topic: topic, Balancer: &kafka.Hash{}, BatchTimeout: 50 * time.Millisecond, BatchSize: 100, Async: true}}
}

func (p *Producer) PublishTrade(t *matching.Trade) error {
	d, _ := json.Marshal(t)
	return p.writer.WriteMessages(context.Background(), kafka.Message{Key: []byte(t.ID), Value: d, Headers: []kafka.Header{{Key: "event_type", Value: []byte(EventTrade)}, {Key: "pair", Value: []byte(t.Pair)}}})
}

func (p *Producer) PublishOrderbook(d *matching.OrderBookDepth) error {
	dat, _ := json.Marshal(d)
	return p.writer.WriteMessages(context.Background(), kafka.Message{Key: []byte(d.Pair), Value: dat, Headers: []kafka.Header{{Key: "event_type", Value: []byte(EventOrderbook)}, {Key: "pair", Value: []byte(d.Pair)}}})
}

func (p *Producer) Close() error { return p.writer.Close() }
