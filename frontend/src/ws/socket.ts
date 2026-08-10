import { WS_URL, WS_CHANNELS } from '@/utils/constants';
import { useAuthStore } from '@/store/authStore';
import { useMarketStore } from '@/store/marketStore';
import type { Orderbook, Trade, FillMessage } from '@/types';

export class FlianceSocket {
  private ws: WebSocket | null = null;
  private reconnectTimer: ReturnType<typeof setTimeout> | null = null;
  private heartbeatTimer: ReturnType<typeof setInterval> | null = null;
  private pair: string | null = null;
  private manualClose = false;
  private reconnectAttempts = 0;
  // No upper bound on retries: a transiently-unavailable backend should not
  // permanently sever the stream. Backoff is capped (MAX_BACKOFF_MS), so the
  // attempt rate stays bounded and recovers automatically when the server
  // comes back. Previously the client gave up after 5 tries, leaving the page
  // silently disconnected until a full reload.
  private MAX_BACKOFF_MS = 30000;

  connect(pair: string) {
    this.pair = pair;
    this.manualClose = false;
    this.reconnectAttempts = 0;
    if (this.reconnectTimer) { clearTimeout(this.reconnectTimer); this.reconnectTimer = null; }
    this.doConnect();
  }

  private doConnect() {
    // SECURITY: never put the JWT in the connection URL — query parameters
    // leak into server/proxy access logs and browser history. The legacy
    // ?token= / ?user_id= parameters were removed by the backend; identity
    // is now established by an in-band auth message after the socket opens.
    const ws = new WebSocket(WS_URL);
    this.ws = ws;
    this.authed = false;

    ws.onopen = () => {
      if (this.ws !== ws) return;
      this.reconnectAttempts = 0;
      // Public market-data channels need no authentication.
      this.subscribe(WS_CHANNELS.ORDERBOOK, [this.pair!]);
      this.subscribe(WS_CHANNELS.TRADES, [this.pair!]);
      // Authenticated sessions: identify the connection with the first
      // message ({"op":"auth","token":...}). Private channels (user fills /
      // order updates) are only available after the server answers auth_ok.
      const token = useAuthStore.getState().token;
      if (token) this.send({ op: 'auth', token });
      this.startHeartbeat();
    };

    ws.onmessage = (event) => {
      if (this.ws !== ws) return;
      try {
        const lines = event.data.split('\n');
        for (const line of lines) {
          if (!line) continue;
          const msg = JSON.parse(line);
          this.resetHeartbeat();
          // Connection-level ops (auth_ok / auth_error / error) carry no
          // "type" field and never reach the market-data handler.
          if (typeof msg.op === 'string') { this.handleOp(msg); continue; }
          if (msg.type === 'pong') continue;
          this.handleMessage(msg);
        }
      } catch (err) { /* ignore */ }
    };

    ws.onclose = () => {
      // Ignore closes from a superseded socket — a newer connection (or an
      // explicit close()) has already taken over.
      if (this.ws !== ws) return;
      this.stopHeartbeat();
      if (!this.manualClose) {
        this.reconnectAttempts++;
        const delay = Math.min(1000 * Math.pow(2, this.reconnectAttempts), this.MAX_BACKOFF_MS);
        console.warn(`[WS] Reconnecting in ${delay}ms (attempt ${this.reconnectAttempts})`);
        this.reconnectTimer = setTimeout(() => this.doConnect(), delay);
      }
    };

    ws.onerror = () => { if (this.ws === ws) ws.close(); };
  }

  // Whether the current connection completed the {"op":"auth"} handshake.
  // Reset on every doConnect() so reconnects re-authenticate from scratch.
  private authed = false;

  private handleOp(msg: Record<string, unknown>) {
    if (msg.op === 'auth_ok') {
      this.authed = true;
      this.subscribePrivateChannels();
    } else if (msg.op === 'auth_error') {
      // The stored JWT was rejected (expired / revoked / wrong type). Drop
      // the session; the public market stream keeps running anonymously.
      this.authed = false;
      console.warn('[WS] authentication rejected by server — signing out');
      useAuthStore.getState().logout();
    }
  }

  // Private subscriptions must only run after auth_ok — the server refuses
  // user:* channels on anonymous connections. Note the server auto-joins an
  // authenticated connection to its user:<uid> room, so fills/order updates
  // arrive without an explicit subscribe; explicit private subscriptions go
  // here if the backend adds them.
  private subscribePrivateChannels() {
    if (!this.authed || !useAuthStore.getState().token) return;
  }

  private startHeartbeat() {
    this.stopHeartbeat();
    this.heartbeatTimer = setInterval(() => this.send({ type: 'ping' }), 30000);
  }

  private stopHeartbeat() {
    if (this.heartbeatTimer) { clearInterval(this.heartbeatTimer); this.heartbeatTimer = null; }
  }

  private resetHeartbeat() {
    if (this.heartbeatTimer) {
      clearInterval(this.heartbeatTimer);
      this.heartbeatTimer = setInterval(() => this.send({ type: 'ping' }), 30000);
    }
  }

  private subscribe(channel: string, pairs: string[]) { this.send({ type: 'subscribe', channel, pairs }); }
  private send(payload: unknown) { if (this.ws?.readyState === WebSocket.OPEN) this.ws.send(JSON.stringify(payload)); }

  private handleMessage(msg: Record<string, unknown>) {
    const store = useMarketStore.getState();
    if (msg.type === 'snapshot' && msg.bids && msg.asks) {
      store.setOrderbook(msg as unknown as Orderbook);
    } else if (msg.type === 'trade') {
      store.addTrade(msg as unknown as Trade);
    } else if (msg.type === 'fill') {
      store.addFill(msg as unknown as FillMessage);
    }
  }

  switchPair(pair: string) {
    if (this.pair) {
      this.send({ type: 'unsubscribe', channel: WS_CHANNELS.ORDERBOOK, pairs: [this.pair] });
      this.send({ type: 'unsubscribe', channel: WS_CHANNELS.TRADES, pairs: [this.pair] });
    }
    this.pair = pair;
    this.reconnectAttempts = 0;
    this.subscribe(WS_CHANNELS.ORDERBOOK, [pair]);
    this.subscribe(WS_CHANNELS.TRADES, [pair]);
  }

  close() {
    this.manualClose = true;
    this.stopHeartbeat();
    if (this.reconnectTimer) clearTimeout(this.reconnectTimer);
    this.ws?.close();
  }
}

export const socket = new FlianceSocket();
