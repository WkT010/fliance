# **NEXA Exchange** (nexa-exchange) v2.0

Production-grade cryptocurrency exchange engine in Go. Lock-free matching engine, WebSocket streaming, multi-asset wallet (Alchemy blockchain integration). Supports 50K-100K concurrent users.

## Quick Start
```bash
git clone https://github.com/WkT010/nexa-exchange.git
cd nexa-exchange
make infra-up              # Start PG16 + Redis7 + Kafka
make run-engine            # Matching engine (8 pairs by default)
make run-api               # API gateway :8080
make run-wallet            # Wallet service :8082
make migrate               # DB migrations
make test && make bench    # Tests + benchmarks
```

## Features
- Multi-pair matching engine (8 built-in pairs, configurable via TRADING_PAIRS)
- Order types: Limit, Market, FOK, IOC, Iceberg, Stop-Loss, Stop-Limit
- Lock-free MPSC ring buffer (CAS-based concurrent enqueue)
- Adaptive backoff (1ms idle / 10µs active) prevents CPU spinning
- WebSocket real-time: orderbook, trades, ticker per pair
- JWT auth + bcrypt passwords + API Key support
- PostgreSQL persistence (orders, trades, users, wallets)
- Kafka market data streaming (producer/consumer)
- gRPC server for order placement and streaming
- Docker Compose + Kubernetes deployment
- CI/CD (GitHub Actions: lint, test, build, docker)

## API
| Method | Path | Description |
|---|---|---|
| GET | /health | Liveness probe |
| GET | /ready | Readiness probe |
| GET | /metrics | Runtime metrics |
| GET | /api/v2/pairs | All trading pairs |
| GET | /api/v2/orderbook/:pair | Order book depth |
| GET | /api/v2/ticker/:pair | Ticker with spread |
| GET | /api/v2/trades/:pair | Trade history |
| POST | /api/v2/auth/login | Login (returns JWT) |
| POST | /api/v2/auth/register | Register |
| POST | /api/v2/auth/refresh | Refresh token |
| POST | /api/v2/order | Place order |
| DELETE | /api/v2/order/:id?pair= | Cancel order |
| GET | /api/v2/order/:id?pair= | Get order |
| GET | /api/v2/orders | List orders |
| WS | /ws | WebSocket |

## WebSocket
```json
{"type":"subscribe","channel":"trades","pairs":["BTC/USDT","ETH/USDT"]}
{"type":"subscribe","channel":"orderbook","pairs":["BTC/USDT"]}
```

## License MIT
