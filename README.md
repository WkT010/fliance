# **NEXA Exchange** (nexa-exchange)

生产级加密货币交易所引擎，Go 语言构建。自研无锁撮合引擎、实时 WebSocket 行情推送、多资产钱包（Alchemy 区块链集成）。支撑 50K–100K 并发用户。

---

## 快速开始

```bash
git clone https://github.com/WkT010/nexa-exchange.git
cd nexa-exchange

make infra-up                            # 启动 PG16 + Redis7 + Kafka
make run-engine                          # 撮合引擎（5 交易对）
make run-api                             # API 网关 :8080
make run-wallet                          # 钱包（Alchemy）

go run scripts/migrate/main.go          # 数据库迁移
make test && make bench                  # 测试 + 基准
```

## Docker 部署

```bash
docker compose -f deploy/docker/docker-compose.prod.yml up -d
```

## Kubernetes 部署

```bash
kubectl apply -k deploy/k8s/
kubectl get pods -n nexa
```

## API

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/health` | 存活探针 |
| GET | `/ready` | 就绪探针 |
| GET | `/metrics` | 运行时指标 |
| POST | `/api/v2/auth/login` | 登录 |
| POST | `/api/v2/order` | 下单 |
| DELETE | `/api/v2/order/:id` | 撤单 |
| GET | `/api/v2/orderbook/:pair` | 订单簿 |
| GET | `/api/v2/ticker/:pair` | Ticker |
| WS | `/ws` | WebSocket |

### 下单
```json
{"pair":"BTC/USDT","side":"buy","type":"limit","price":"50000","quantity":"0.01"}
```

### WebSocket 订阅
```json
{"type":"subscribe","channel":"orderbook","pairs":["BTC/USDT"]}
```

## 撮合引擎

- 买盘 Max-Heap / 卖盘 Min-Heap（价格-时间优先）
- 每交易对独立 goroutine + MPSC 无锁环形缓冲区（CAS）
- Market, Limit, Iceberg, FOK, IOC, Stop-Loss
- p50 < 200µs, 单对 200K 订单/秒

## 钱包

Alchemy JSON-RPC: `eth_getBalance`, `eth_getTransactionReceipt`, `eth_estimateGas`

## 错误处理

统一 JSON: `{"error":"...","code":4xx/5xx}`
panic 由 `ErrorHandler` 中间件捕获，返回 500 + 堆栈日志。

## License MIT
