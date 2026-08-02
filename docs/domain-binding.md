# 固定公网 IP 并绑定域名

本指南把跑在本机的 Nexa Exchange（api-gateway 监听 `127.0.0.1:8080` 或 `:8080`）暴露到一个固定域名上，并用 Caddy 自动签发 HTTPS 证书。完成后前端走 `https://你的域名`，WebSocket 走 `wss://你的域名/ws`。

## 0. 前置条件

- api-gateway 已能本地访问（`curl http://127.0.0.1:8080/api/v2/ping` 返回 ok）。
- 前端已构建并放到 `STATIC_DIR`（默认 `./frontend/dist`），由 gateway 直接托管；或单独部署到 CDN/静态托管。
- 你有一个域名（任何注册商均可：Cloudflare / 阿里云 / Namecheap / 腾讯云…）。

## 1. 确定公网 IP

```bash
curl ifconfig.me
```

- **家庭宽带**：多数是动态 IP。两种固定方式：
  1. **DDNS（推荐）**：用 Cloudflare DNS + DDNS 脚本（或路由器内置的 DDNS）把域名指向变化的公网 IP。Cloudflare 免费且 API 好用。
  2. **申请固定 IP**：联系运营商开通固定公网 IP（企业宽带/专线一般可以，家宽通常需要付费或不可得）。
- **云主机 / 企业专线**：一般是静态公网 IP，直接用即可。

> 注意：运营商给你的是“公网 IP”还是“大内网 IP”？
> - 公网 IP：`curl ifconfig.me` 的结果在路由器 WAN 口上能对上，外网可直接访问。
> - 大内网 IP（CGNAT）：`ifconfig.me` 显示一个 IP，但路由器 WAN 不一致 → 入站不可达，必须用 DDNS + 端口映射方案不可行，需要联系运营商要公网 IP 或改用 frp/cloudflared 隧道。

## 2. DNS A 记录指向你的公网 IP

到域名注册商的 DNS 管理，加一条 A 记录：

| 类型 | 名称 | 值 | TTL | 代理 |
|------|------|-----|-----|------|
| A | trade | 你的公网 IP | Auto | 先关橙云（DNS only）|

- **Cloudflare 用户**：建议先**关掉橙云**（DNS only，灰云图标）直接验证，让 Caddy 自己签证书；或开橙云用 Cloudflare 的 443 终端（此时后端走 8080 即可，Caddy 可省略，但 WebSocket 需在 Cloudflare 开启 WebSocket 支持）。
- 等待 DNS 生效（`nslookup trade.yourdomain.com` 返回你的 IP 即可，通常 1–10 分钟）。

## 3. 端口转发 / 安全组放行

需要让外网能访问你机器的 80 + 443（Caddy 用），或直接 8080（不走 Caddy 时）。

- **家庭宽带**：路由器把外网 `80` 和 `443` 端口转发到跑 Caddy 的机器内网 IP（如 `192.168.1.x:80` / `:443`）。Caddy 再反代到本机 `127.0.0.1:8080`。
- **云主机**：在云厂商的安全组放行入站 `80/tcp` 和 `443/tcp`。

> api-gateway 本身监听 `:8080`（绑所有接口）。如果你用 Caddy，建议把 gateway 改成只监听 `127.0.0.1:8080`（`LISTEN_ADDR=127.0.0.1:8080`），外网只通过 Caddy 进来，更安全。

## 4. 用 Caddy 反代 + 自动 HTTPS

Caddy 单文件、零配置证书，最适合本场景。

### 安装 Caddy
见 https://caddyserver.com/docs/install （Windows / Linux / macOS 都有）。

### 配置
复制 `deploy/docker/Caddyfile.example` 为 `./Caddyfile`，把 `trade.yourdomain.com` 改成你的域名。

```caddyfile
trade.yourdomain.com {
  tls { protocols tls1.2 tls1.3 }
  encode zstd gzip
  reverse_proxy 127.0.0.1:8080 {
    flush_interval -1
    header_up Host {host}
    header_up X-Real-IP {remote_host}
    header_up X-Forwarded-For {remote_host}
    header_up X-Forwarded-Proto {scheme}
  }
}
```

### 启动

```bash
caddy run --config ./Caddyfile
# 或后台运行：
caddy start --config ./Caddyfile
```

Caddy 会自动：
1. 向 Let's Encrypt 申请 `trade.yourdomain.com` 的证书。
2. 续期（到期前 30 天自动 renew）。
3. 把 `https://trade.yourdomain.com` 反代到 `127.0.0.1:8080`，包括 WebSocket（`wss://` 走 `/ws`）。

> 首次启动时 Caddy 需要能从外网通过 80 端口被 Let's Encrypt 访问到（HTTP-01 challenge），所以 80 端口必须先放行。

## 5. 后端环境变量（生产）

启动 api-gateway 时设置（Windows PowerShell 示例）：

```powershell
$env:ENVIRONMENT = "production"
$env:LISTEN_ADDR = "127.0.0.1:8080"        # 只让 Caddy 进
$env:JWT_SECRET = "一个足够长的随机串"
$env:CORS_ALLOW_ORIGINS = "https://trade.yourdomain.com"
$env:CORS_ALLOW_CREDENTIALS = "true"
$env:STATIC_DIR = "./frontend/dist"
# 可选：保留外部价格源作为回退（默认关闭，完全自包含）
# $env:ENABLE_EXTERNAL_PRICE_FALLBACK = "true"
# 可选：调整 AMM 模拟器节奏
# $env:MARKET_SIM_INTERVAL = "5s"
```

> 因为前端和 API 同源（都在 `https://trade.yourdomain.com`），CORS 其实是 no-op，但配成你的域名是安全最佳实践，方便以后前端单独部署到 CDN。

## 6. 前端 API/WS 地址

`frontend/src/utils/constants.ts` 已用相对路径：

```ts
export const API_BASE = '/api/v2';
export const WS_URL = '/ws';
```

同源部署下，浏览器会自动用 `https://trade.yourdomain.com/api/v2` 和 `wss://trade.yourdomain.com/ws`，无需改代码、不会有 mixed-content。

如需把前端单独部署到别的域名/CDN，改 `constants.ts` 为绝对地址并加环境变量支持：

```ts
export const API_BASE = import.meta.env.VITE_API_BASE || '/api/v2';
export const WS_URL = import.meta.env.VITE_WS_URL || '/ws';
```

并在 `frontend/.env.production` 写：
```
VITE_API_BASE=https://trade.yourdomain.com/api/v2
VITE_WS_URL=wss://trade.yourdomain.com/ws
```

## 7. 验证

```bash
# 1. HTTP 探活
curl https://trade.yourdomain.com/api/v2/ping

# 2. 自包含行情（应返回 5 个 AMM 池的 ticker）
curl https://trade.yourdomain.com/api/v2/tickers

# 3. 订单簿
curl "https://trade.yourdomain.com/api/v2/orderbook/BTC%2FUSDT"

# 4. 等 10s 再请求 tickers，价格应有变化（模拟器在跑）
```

浏览器打开 `https://trade.yourdomain.com`：
- 控制台无 mixed-content / CORS 报错。
- 订单簿 / 成交 / K 线持续更新（证明不依赖外部源）。
- WebSocket 连上 `wss://trade.yourdomain.com/ws`（DevTools Network → WS）。

## 8. 常见问题

- **Caddy 签证书失败**：检查 80 端口是否对外可达（Let's Encrypt HTTP-01）、DNS 是否已生效、域名是否拼错。
- **WebSocket 连不上**：Caddy 的 `flush_interval -1` 必须设；Cloudflare 橙云用户需在 Cloudflare 开启 WebSocket（Network → WebSockets: On）。
- **家庭宽带 80/443 被封**：国内运营商常封家用 80。改 Caddy 监听 `:8443` + DNS 指向，访问 `https://trade.yourdomain.com:8443`；或用 Cloudflare 橙云代理（Cloudflare 用 443 回源到你任意开放端口）。
- **CGNAT / 没公网 IP**：用 Cloudflare Tunnel（`cloudflared`）把本机暴露出去，无需公网 IP / 端口转发，Caddy 可省略。
