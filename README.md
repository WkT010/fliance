# **NEXA Exchange** (nexa-exchange)

**NEXA** is a production-grade cryptocurrency exchange engine built in **Go**, designed for **50K–100K concurrent users** with sub-millisecond matching latency. Features a self-developed lock-free matching engine, real-time WebSocket streaming, multi-asset wallet with **Alchemy** blockchain integration, and a microservice architecture ready for horizontal scaling.

---

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                    API Gateway (Gin)                         │
│         REST /api/v2/*    │    WebSocket /ws/*               │
└────────────────┬───────────────────────────┬─────────────────┘
                 │                           │
         ┌───────▼───────┐          ┌────────▼────────┐
         │  Order Service │          │  Market Data     │
         │  (gRPC+Redis)  │          │  Stream (Kafka)  │
         └───────┬───────┘          └────────┬────────┘
                 │                           │
         ┌───────▼───────────────────────────▼────────┐
         │          Matching Engine Cluster             │
         │  Lock-Free Ring Buffer + Price-Time Priority │
         │  ┌──────────┐ ┌──────────┐ ┌──────────┐    │
         │  │ BTC/USDT │ │ ETH/USDT │ │ SOL/USDT │    │
         │  └──────────┘ └──────────┘ └──────────┘    │
         └──────────────────────┬──────────────────────┘
                                │
         ┌──────────────────────▼──────────────────────┐
         │            Wallet Service                     │
         │  ┌──────────┐ ┌──────────┐ ┌──────────┐    │
         │  │ Alchemy  │ │  Mock    │ │  HD Keys │    │
         │  │ ETH/POLY │ │  BTC     │ │  (WIP)   │    │
         │  └──────────┘ └──────────┘ └──────────┘    │
         └─────────────────────────────────────────────┘
```

## Features

| Category | Details |
|---|---|
| **Matching Engine** | Lock-free SPSC/MPSC ring buffer, price-time priority heap, Market/Limit/Iceberg/FOK/IOC |
| **WebSocket** | Room-based pub/sub, orderbook/trades/user channels, batch write coalescing |
| **Market Data** | Kafka-partitioned by trading pair, consumer group support |
| **Auth** | JWT (HS256), API keys with constant-time validation, RBAC-ready Claims |
| **Wallet** | Balance/Locked double-ledger, Alchemy JSON-RPC (ETH/Polygon), Mock client (BTC) |
| **API** | 20+ REST endpoints, gRPC service definitions |
| **Infra** | Docker Compose (PG16 + Redis7 + Kafka), Makefile targets |

## Alchemy Integration

NEXA uses **Alchemy** as its primary blockchain RPC provider for EVM-compatible chains:

```go
clients := map[string]wallet.BlockchainClient{
    "ETH":     wallet.NewAlchemyClient("ETH", "https://eth-mainnet.g.alchemy.com/v2/YOUR_KEY"),
    "POLYGON": wallet.NewAlchemyClient("POLYGON", "https://polygon-mainnet.g.alchemy.com/v2/YOUR_KEY"),
    "BTC":     wallet.NewMockBlockchainClient("BTC"),
}
svc := wallet.NewService(store, clients)
```

## Quick Start

```bash
# Start dependencies
export ALCHEMY_API_KEY="owtgBOQy-6ABQ9Pzd_7Nz"
make infra-up

# Start
make run-engine
make run-api
make run-wallet

# Test
make test
make bench
```

## Performance Targets

| Metric | Target |
|---|---|
| Match latency (p50) | < 200µs |
| Throughput (single pair) | 200,000 orders/sec |
| Concurrent WS | 50,000+ per node |

## License

MIT