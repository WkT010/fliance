export interface User { id: string; email: string; role: 'user' | 'admin'; created_at?: number; }
export interface AuthState { token: string | null; user: User | null; isAdmin: boolean; }
export interface LoginReq { email: string; password: string; }
export interface RegisterReq { email: string; password: string; }
export interface Ticker {
  pair: string; last: string; bid?: string; ask?: string; source?: string; timestamp?: number;
  change_24h?: string; change_pct_24h?: string; volume_24h?: string; high_24h?: string; low_24h?: string; open_24h?: string;
}
export interface OrderbookLevel { price: string; quantity: string; count: number; }
export interface Orderbook { pair: string; bids: OrderbookLevel[]; asks: OrderbookLevel[]; seq: number; }
export interface Trade { pair: string; price: string; quantity: string; time: number; side?: 'buy' | 'sell'; }
export interface Candle { open: string; high: string; low: string; close: string; volume: string; timestamp: number; }
export type OrderSide = 'buy' | 'sell';
export type OrderType = 'limit' | 'market' | 'ioc' | 'fok' | 'post_only' | 'stop_loss' | 'stop_limit' | 'iceberg';
export interface Order {
  id: string; client_order_id?: string; user_id: string; pair: string; side: OrderSide; type: OrderType;
  price?: string; stop_price?: string; quantity: string; filled_qty?: string; remaining_qty?: string;
  status: 'new' | 'partially_filled' | 'filled' | 'cancelled' | 'rejected' | 'expired';
  created_at: number; updated_at: number;
}
export interface PlaceOrderReq {
  pair: string; side: OrderSide; type: OrderType; price?: string; stop_price?: string;
  quantity: string; time_in_force?: 'gtc' | 'ioc' | 'fok'; client_order_id?: string;
}
export interface Balance { asset: string; available: string; locked: string; total: string; }
export interface APIKey { id: string; name: string; key?: string; created_at: number; }