/** Static display metadata for the coins listed on Fliance. */
export const COIN_COLORS: Record<string, string> = {
  BTC: '#F7931A',
  ETH: '#627EEA',
  SOL: '#9945FF',
  BNB: '#F0B90B',
  ADA: '#2A6AFF',
  USDT: '#26A17B',
};

export const COIN_NAMES: Record<string, string> = {
  BTC: 'Bitcoin',
  ETH: 'Ethereum',
  SOL: 'Solana',
  BNB: 'BNB',
  ADA: 'Cardano',
  USDT: 'Tether',
};

export function coinColor(symbol: string): string {
  return COIN_COLORS[symbol] ?? '#5E6675';
}

export function coinName(symbol: string): string {
  return COIN_NAMES[symbol] ?? symbol;
}

export function CoinIcon({ symbol, size = 'md' }: { symbol: string; size?: 'md' | 'lg' }) {
  const cls =
    size === 'lg'
      ? 'flex h-9 w-9 items-center justify-center rounded-full text-sm font-bold text-white'
      : 'flex h-7 w-7 items-center justify-center rounded-full text-xs font-bold text-white';
  return (
    <span className={cls} style={{ backgroundColor: coinColor(symbol) }}>
      {symbol.slice(0, 1)}
    </span>
  );
}
