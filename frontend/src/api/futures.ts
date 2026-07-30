import { api } from './client';
import type { FuturesPosition, FuturesOrder, MarkPrice, OrderSide, OrderType, MarginMode } from '@/types';

export async function getMarkPrice(pair: string): Promise<MarkPrice> {
  const res = await api.get(`/futures/mark-price/${encodeURIComponent(pair)}`);
  return res.data;
}

export async function getFuturesPositions(): Promise<FuturesPosition[]> {
  const res = await api.get('/futures/positions');
  return res.data.positions || [];
}

export interface OpenPositionReq {
  pair: string;
  side: 'long' | 'short';
  leverage: number;
  margin_mode: MarginMode;
  quantity: string;
  price?: string;
}

export async function openFuturesPosition(data: OpenPositionReq): Promise<FuturesPosition> {
  const res = await api.post('/futures/positions', data);
  return res.data.position;
}

export async function closeFuturesPosition(id: string): Promise<FuturesPosition> {
  const res = await api.post(`/futures/positions/${id}/close`);
  return res.data.position;
}

export interface PlaceFuturesOrderReq {
  pair: string;
  side: OrderSide;
  type: OrderType;
  quantity: string;
  price?: string;
  stop_price?: string;
  leverage: number;
  margin_mode: MarginMode;
  tp_price?: string;
  sl_price?: string;
}

export async function placeFuturesOrder(data: PlaceFuturesOrderReq): Promise<FuturesOrder> {
  const res = await api.post('/futures/orders', data);
  return res.data.order;
}

export async function getFuturesOrders(): Promise<FuturesOrder[]> {
  const res = await api.get('/futures/orders');
  return res.data.orders || [];
}
