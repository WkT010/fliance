# **NEXA Exchange**

**NEXA** 是一个生产级加密货币交易所引擎，使用 **Go** 语言构建。自研无锁撮合引擎、实时 WebSocket 行情推送、多资产钱包（Alchemy 区块链集成）、微服务架构、Kubernetes 就绪。

## 快速开始

```bash
# 克隆
git clone https://github.com/WkT010/nexa-exchange.git && cd nexa-exchange

# 启动基础设施
make infra-up

# 数据库迁移
go run scripts/migrate/main.go

# 启动服務（三个终端）
make run-engine    # 5 个交易对
make run-api       # REST :8080 + WS
make run-wallet    # Alchemy ETH/Polygon

# 测试
make test && make bench
```

## API

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/health` | 存活探针 |
| GET | `/ready` | 就绪探针 |
| GET | `/metrics` | 运行时指标 |
| POST | `/api/v2/order` | 下单 (<-pair,side,type,price,quantity>) |
| DELETE | `/api/v2/order/:id` | 撤单 |
| GET | `/api/v2/orderbook/:pair` | 订单簿 |
| WS | `/ws` | WebSocket (<-subscribe orderbook:BTC/USDT>) |

## 架构

```
Client → API Gateway (Gin) → Matching Engine → WS Bridge → Hub → Client
                             ↘ PostgreSQL (wallet/orders/users)
                             ↘ Alchemy RPC (ETH/Polygon)
```

## 项目

- `/internal/matching/` — 自研撮合引擎（无锁、价格-时间优先、7 种订单类型）
- `/internal/wallet/alchemy.go` — Alchemy JSON-RPC (eth_getBalance, eth_getTransactionReceipt)
- `/internal/store/` — PostgreSQL 存储实现 (WalletStore, OrderStore, UserStore)
- `/internal/grpc/` — gRPC ExchangeService + StreamService
- `/internal/wsbridge/` — 撮合成交 → WebSocket 广播桥接
- `/internal/api/errors.go` — 结构化错误处理与 panic 恢复
- `/deploy/{docker,k8s}/` — 多阶段 Docker 构建 + Kustomize K8s 清单

## License MIT
