package market

import (
	"context"
	"log"

	"github.com/segmentio/kafka-go"
)

type Consumer struct {
	reader  *kafka.Reader
	handler func(MarketEvent) error
	done    chan struct{}
}

func NewConsumer(brokers []string, topic, groupID string, handler func(MarketEvent) error) *Consumer {
	return &Consumer{reader: kafka.NewReader(kafka.ReaderConfig{Brokers: brokers, Topic: topic, GroupID: groupID, MinBytes: 1, MaxBytes: 10e6, StartOffset: kafka.LastOffset}), handler: handler, done: make(chan struct{})}
}

func (c *Consumer) Start() {
	go func() {
		for {
			m, err := c.reader.ReadMessage(context.Background())
			if err != nil {
				select { case <-c.done: return; default: log.Printf("consumer err: %v", err); continue }
			}
			var et, pair string
			for _, h := range m.Headers {
				switch h.Key { case "event_type": et = string(h.Value); case "pair": pair = string(h.Value) }
			}
			c.handler(MarketEvent{Type: et, Pair: pair, Data: m.Value, Timestamp: m.Time.UnixNano()})
		}
	}()
}

func (c *Consumer) Stop() error { close(c.done); return c.reader.Close() }
