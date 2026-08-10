# **Fliance（梵响）** v2.0

Fliance（梵响） is a production-grade cryptocurrency exchange engine in Go, operated by 凌嘉凡响网络科技有限公司 (Canival Institute Inc.). Lock-free matching engine, WebSocket streaming, multi-asset wallet (Alchemy blockchain integration). Supports 50K-100K concurrent users.

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

## Deployment: Supabase（可选托管数据库 / Optional Managed Database）

Instead of self-hosting PostgreSQL, you can point the exchange at a managed
Supabase project:

1. Create a Supabase project at https://supabase.com and wait for it to finish provisioning.
2. Open **Project Settings → Database → Connection string** and copy the **Session pooler / Direct connection** string (port `5432`). Do **not** use the Transaction pooler string (port `6543`).
3. Fill `.env` with the connection string, replacing the password placeholder and keeping `sslmode=require` (Supabase enforces TLS):
   ```bash
   POSTGRES_DSN=postgresql://postgres.[PROJECT-REF]:[YOUR-PASSWORD]@aws-0-<region>.pooler.supabase.com:5432/postgres?sslmode=require
   ```
4. Run the migrations against Supabase (they are versioned via the `schema_migrations` table):
   ```bash
   make migrate   # or: go run ./scripts/migrate
   ```
5. Start the services as usual (`make run-engine`, `make run-api`, `make run-wallet`); they read `POSTGRES_DSN` at startup.

> Note: the transaction pooler (PgBouncer, port 6543) is **incompatible** with
> this codebase — lib/pq prepared statements fail in transaction-pooling mode
> with `unnamed prepared statement does not exist`. Always use the session
> pooler / direct connection (port 5432).

## License MIT
