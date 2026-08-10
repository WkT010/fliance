export interface User { id: string; email: string; role: 'user' | 'admin'; created_at?: number; }
export interface AuthState { token: string | null; refreshToken: string | null; user: User | null; isAdmin: boolean; }
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
  tp_price?: string; sl_price?: string;
}
export interface Balance { asset: string; available: string; locked: string; total: string; }
export interface APIKey { id: string; name: string; key?: string; created_at: number; }

export interface FillMessage {
  order_id: string;
  pair: string;
  price: string;
  quantity: string;
  side: 'buy' | 'sell';
  time: number;
}

export interface PriceComparison {
  pair: string;
  internal: { available: boolean; last: string };
  uniswap: { available: boolean; last?: string; error?: string };
}

export type FuturesSide = 'long' | 'short';
export type MarginMode = 'isolated' | 'cross';

export interface FuturesPosition {
  id: string;
  pair: string;
  side: FuturesSide;
  leverage: number;
  margin_mode: MarginMode;
  entry_price: string;
  mark_price: string;
  quantity: string;
  margin: string;
  pnl: string;
  pnl_pct: string;
  liq_price: string;
  tp_price?: string;
  sl_price?: string;
  status: 'open' | 'closed' | 'liquidated';
  created_at: number;
  updated_at: number;
}

export interface FuturesOrder {
  id: string;
  pair: string;
  side: OrderSide;
  type: OrderType;
  price?: string;
  stop_price?: string;
  quantity: string;
  leverage: number;
  margin_mode: MarginMode;
  tp_price?: string;
  sl_price?: string;
  status: string;
  created_at: number;
}

export interface MarkPrice {
  pair: string;
  mark_price: string;
  index_price: string;
  funding_rate: string;
  next_funding: number;
}

export interface PnLHistoryItem { date: string; realized: string; }
export interface PnLSummary {
  total_realized: string;
  unrealized: string;
  portfolio_value: string;
  total_fees: string;
  today_realized: string;
  positions: Array<{ asset: string; qty: string; avg_cost: string }>;
}
export interface PnLHistory { history: PnLHistoryItem[]; }

export interface Transaction {
  id: string;
  type: 'deposit' | 'withdrawal';
  asset: string;
  amount: string;
  status: string;
  created_at: number;
}

export interface WithdrawReq { asset: string; address: string; amount: string; }

export interface WithdrawalReviewItem {
  id: string;
  user_id: string;
  asset: string;
  amount: string;
  status: string;
  created_at: number;
}

export interface AddressBookEntry {
  id: string;
  user_id: string;
  asset: string;
  address: string;
  label?: string;
  created_at: number;
}

export interface PairRiskConfig {
  pair: string;
  trading_enabled: boolean;
  market_orders_enabled: boolean;
}
