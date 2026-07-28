import { api } from './client';
import type { Order, PlaceOrderReq } from '@/types';

export async function placeOrder(data: PlaceOrderReq): Promise<Order> {
  const res = await api.post<Order>('/order', data);
  return res.data;
}

export async function cancelOrder(id: string): Promise<void> {
  await api.delete(`/order/${id}`);
}

export async function cancelAllOrders(): Promise<void> {
  await api.delete('/orders');
}

export async function getOrder(id: string): Promise<Order> {
  const res = await api.get<Order>(`/order/${id}`);
  return res.data;
}

export async function listOrders(pair?: string): Promise<Order[]> {
  const params = pair ? `?pair=${encodeURIComponent(pair)}` : '';
  const res = await api.get<{ orders: Order[] }>(`/orders${params}`);
  return res.data.orders || [];
}
