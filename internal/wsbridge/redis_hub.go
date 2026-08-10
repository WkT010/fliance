package wsbridge

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/WkT010/nexa-exchange/pkg/websocket"
	"github.com/redis/go-redis/v9"
)

type RedisHub struct {
	local   *websocket.Hub
	client  *redis.Client
	pubsub  *redis.PubSub
	channel string
	done    chan struct{}
}

type RedisMessage struct {
	Type, Channel, UserID string
	Data                  json.RawMessage `json:"data"`
}

func NewRedisHub(local *websocket.Hub, addr, pass string) *RedisHub {
	return &RedisHub{
		local: local, channel: "nexa:ws", done: make(chan struct{}),
		client: redis.NewClient(&redis.Options{Addr: addr, Password: pass, PoolSize: 100, MinIdleConns: 10}),
	}
}

func (rh *RedisHub) Start() {
	rh.pubsub = rh.client.Subscribe(context.Background(), rh.channel)
	rh.pubsub.Receive(context.Background())
	slog.Info("redis-hub listening", "channel", rh.channel)
	go func() {
		for msg := range rh.pubsub.Channel() {
			var rm RedisMessage
			json.Unmarshal([]byte(msg.Payload), &rm)
			rh.local.BroadcastToRoom(rm.Channel, rm.Data)
		}
	}()
}

func (rh *RedisHub) Stop() {
	close(rh.done)
	if rh.pubsub != nil {
		rh.pubsub.Close()
	}
	rh.client.Close()
}

func (rh *RedisHub) Publish(room string, data []byte) {
	payload, _ := json.Marshal(RedisMessage{Type: "update", Channel: room, Data: data})
	rh.client.Publish(context.Background(), rh.channel, payload)
	rh.local.BroadcastToRoom(room, data)
}
