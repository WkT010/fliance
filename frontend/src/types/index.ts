export interface User {
  id: string;
  email: string;
  role: 'user' | 'admin';
  created_at?: number;
}

export interface AuthState {
  token: string | null;
  user: User | null;
  isAdmin: boolean;
}

export interface LoginReq {
  email: string;
  password: string;
}

export interface RegisterReq {
  email: string;
  password: string;
}

export interface Ticker {
  pair: string;
  last: string;
  bid?: string;
  ask?: string;
  source?: string;
  timestamp?: number;
  change_24h?: string;
  change_pct_24h?: string;
  volume_24h?: string;
}

export interface PriceComparison {
  pair: string;
  internal: { available: boolean; last?: string; source?: string; timestamp?: number };
  uniswap: { available: boolean; last?: string; source?: string; timestamp?: number; error?: string };
}

export interface OrderbookLevel {
  price: string;
  quantity: string;
  count: number;
}

export interface Orderbook {
  pair: string;
  bids: OrderbookLevel[];
  asks: OrderbookLevel[];
  seq: number;
}

export interface Trade {
  pair: string;
  price: string;
  quantity: string;
  time: number;
  side?: 'buy' | 'sell';
}

export interface FillMessage {
  type: 'fill';
  pair: string;
  taker_order_id: string;
  maker_order_id: string;
  side: string;
  price: string;
  qty: string;
  remaining: string;
  time: number;
}

export type OrderSide = 'buy' | 'sell';
export type OrderType = 'limit' | 'market' | 'ioc' | 'fok' | 'post_only' | 'stop_loss' | 'stop_limit' | 'iceberg';

export interface Order {
  id: string;
  client_order_id?: string;
  user_id: string;
  pair: string;
  side: OrderSide;
  type: OrderType;
  price?: string;
  stop_price?: string;
  quantity: string;
  filled_qty?: string;
  remaining_qty?: string;
  status: 'new' | 'partially_filled' | 'filled' | 'cancelled' | 'rejected' | 'expired';
  created_at: number;
  updated_at: number;
}

export interface PlaceOrderReq {
  pair: string;
  side: OrderSide;
  type: OrderType;
  price?: string;
  stop_price?: string;
  quantity: string;
  time_in_force?: 'gtc' | 'ioc' | 'fok';
  client_order_id?: string;
}

export interface Balance {
  asset: string;
  available: string;
  locked: string;
  total: string;
}

export interface Transaction {
  id: string;
  type: 'deposit' | 'withdrawal';
  asset: string;
  amount: string;
  fee?: string;
  status: string;
  tx_hash?: string;
  to_address?: string;
  confirmations?: number;
  created_at: number;
}

export interface WithdrawReq {
  asset: string;
  address: string;
  amount: string;
}

export interface PnLPosition {
  asset: string;
  qty: string;
  avg_cost: string;
  realized_pnl: string;
  total_fees: string;
  last_fill_time: number;
}

export interface PnLSummary {
  user_id: string;
  date: string;
  today_realized: string;
  total_realized: string;
  unrealized: string;
  total_fees: string;
  portfolio_value: string;
  positions: PnLPosition[];
  reference_prices: Record<string, string>;
}

export interface PnLHistoryItem {
  date: string;
  realized: string;
}

export interface PnLHistory {
  history: PnLHistoryItem[];
  days: number;
}

export interface APIKey {
  id: string;
  name: string;
  key?: string;
  created_at: number;
}

export interface WithdrawalReviewItem {
  id: string;
  user_id: string;
  asset: string;
  amount: string;
  status: string;
  to_address?: string;
  created_at: number;
}

export interface AddressBookEntry {
  id: string;
  asset: string;
  address: string;
  label?: string;
  created_at?: number;
}

export interface PairRiskConfig {
  pair: string;
  min_notional?: string;
  max_notional?: string;
  min_qty?: string;
  max_qty?: string;
  tick_size?: string;
  lot_size?: string;
  price_band_pct?: string;
  circuit_breaker_pct?: string;
  reference_price?: string;
  market_orders_enabled?: boolean;
  trading_enabled?: boolean;
}

export interface UserRiskLimit {
  user_id: string;
  max_open_orders?: number;
  orders_per_minute?: number;
  orders_per_hour?: number;
  orders_per_day?: number;
  daily_order_notional?: Record<string, string>;
  max_position?: Record<string, string>;
}

export interface Candle {
  open: string;
  high: string;
  low: string;
  close: string;
  volume: string;
  timestamp: number;
}
