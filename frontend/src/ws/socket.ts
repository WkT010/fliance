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

  connect(pair: string) {
    this.pair = pair;
    this.manualClose = false;
    const token = useAuthStore.getState().token;
    const url = token ? `${WS_URL}?token=${token}` : WS_URL;
    this.ws = new WebSocket(url);

    this.ws.onopen = () => {
      this.subscribe(WS_CHANNELS.ORDERBOOK, [pair]);
      this.subscribe(WS_CHANNELS.TRADES, [pair]);
      this.startHeartbeat();
    };

    this.ws.onmessage = (event) => {
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

    this.ws.onclose = () => {
      this.stopHeartbeat();
      if (!this.manualClose) this.reconnectTimer = setTimeout(() => this.pair && this.connect(this.pair), 3000);
    };

    this.ws.onerror = () => { this.ws?.close(); };
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
    if (msg.type === 'snapshot' && msg.bids && msg.asks) store.setOrderbook(msg as unknown as Orderbook);
    else if (msg.type === 'trade') store.addTrade(msg as unknown as Trade);
    else if (msg.type === 'fill') store.addFill(msg as unknown as FillMessage);
  }

  switchPair(pair: string) {
    if (this.pair) {
      this.send({ type: 'unsubscribe', channel: WS_CHANNELS.ORDERBOOK, pairs: [this.pair] });
      this.send({ type: 'unsubscribe', channel: WS_CHANNELS.TRADES, pairs: [this.pair] });
    }
    this.pair = pair;
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