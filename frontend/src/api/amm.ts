import { api } from './client';

export interface AmmPool {
  id: string;
  pair: string;
  token0: string;
  token1: string;
  reserve0: string;
  reserve1: string;
  lp_shares: string;
  fee_rate: string;
  status: string;
  created_at: number;
  updated_at: number;
}

export interface AmmPosition {
  id: string;
  user_id: string;
  pool_id: string;
  shares: string;
  created_at: number;
  updated_at: number;
}

export interface AmmSwap {
  id: string;
  pool_id: string;
  user_id: string;
  token_in: string;
  token_out: string;
  amount_in: string;
  amount_out: string;
  fee: string;
  created_at: number;
}

export interface CreatePoolReq {
  pair: string;
  token0: string;
  token1: string;
  fee_rate?: number;
}

export interface AddLiquidityReq {
  amount0: string;
  amount1: string;
}

export interface RemoveLiquidityReq {
  shares: string;
}

export interface SwapQuoteReq {
  pool_id: string;
  token_in: string;
  amount_in: string;
}

export interface SwapQuote {
  pool_id: string;
  token_in: string;
  amount_in: string;
  amount_out: string;
  fee: string;
}

export interface SwapReq {
  pool_id: string;
  token_in: string;
  amount_in: string;
}

export async function getAmmPools(): Promise<AmmPool[]> {
  const res = await api.get<{ pools: AmmPool[] }>('/amm/pools');
  return res.data.pools;
}

export async function getAmmPool(id: string): Promise<AmmPool> {
  const res = await api.get<AmmPool>(`/amm/pools/${id}`);
  return res.data;
}

export async function createAmmPool(data: CreatePoolReq): Promise<AmmPool> {
  const res = await api.post<AmmPool>('/amm/pools', data);
  return res.data;
}

export async function addLiquidity(
  poolId: string,
  data: AddLiquidityReq
): Promise<{ pool: AmmPool; position: AmmPosition; amount0: string; amount1: string; shares_minted: string }> {
  const res = await api.post(`/amm/pools/${poolId}/add-liquidity`, data);
  return res.data;
}

export async function removeLiquidity(
  poolId: string,
  data: RemoveLiquidityReq
): Promise<{ pool: AmmPool; amount0: string; amount1: string; shares: string }> {
  const res = await api.post(`/amm/pools/${poolId}/remove-liquidity`, data);
  return res.data;
}

export async function getAmmPosition(poolId: string): Promise<AmmPosition | null> {
  const res = await api.get<{ position: AmmPosition | null }>(`/amm/pools/${poolId}/position`);
  return res.data.position;
}

export async function getAmmPositions(): Promise<AmmPosition[]> {
  const res = await api.get<{ positions: AmmPosition[] }>('/amm/positions');
  return res.data.positions;
}

export async function getAmmSwaps(poolId: string, limit = 50, offset = 0): Promise<AmmSwap[]> {
  const res = await api.get<{ swaps: AmmSwap[] }>(`/amm/pools/${poolId}/swaps?limit=${limit}&offset=${offset}`);
  return res.data.swaps;
}

export async function quoteAmmSwap(data: SwapQuoteReq): Promise<SwapQuote> {
  const res = await api.post<SwapQuote>('/amm/swap/quote', data);
  return res.data;
}

export async function executeAmmSwap(data: SwapReq): Promise<AmmSwap> {
  const res = await api.post<AmmSwap>('/amm/swap', data);
  return res.data;
}
