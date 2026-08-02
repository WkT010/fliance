import { WS_URL, WS_CHANNELS } from '@/utils/constants';
import { useAuthStore } from '@/store/authStore';
import { useMarketStore } from '@/store/marketStore';
import type { Orderbook, Trade, FillMessage } from '@/types';

export class NexaSocket {
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
    const token = useAuthStore.getState().token;
    const url = token ? `${WS_URL}?token=${token}` : WS_URL;
    // Capture the instance so handlers can detect whether they belong to the
    // currently-active connection. Without this, a late onclose from a previous
    // (superseded) socket triggers spurious reconnects — especially under React
    // StrictMode, which mounts/unmounts/remounts and shares this singleton.
    const ws = new WebSocket(url);
    this.ws = ws;

    ws.onopen = () => {
      if (this.ws !== ws) return;
      this.reconnectAttempts = 0;
      this.subscribe(WS_CHANNELS.ORDERBOOK, [this.pair!]);
      this.subscribe(WS_CHANNELS.TRADES, [this.pair!]);
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

export const socket = new NexaSocket();
