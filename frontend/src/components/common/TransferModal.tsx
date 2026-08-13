import { useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { isAxiosError } from 'axios';
import { transfer } from '@/api/wallet';
import { Button } from '@/components/common/Button';
import { Input } from '@/components/common/Input';
import { Select } from '@/components/common/Select';
import { formatQty, cls } from '@/utils/format';
import { toast } from '@/store/toastStore';
import type { AccountType, Balance } from '@/types';

export const ACCOUNT_TYPES: AccountType[] = ['spot', 'futures', 'funding'];

interface TransferModalProps {
  open: boolean;
  onClose: () => void;
  /** All balance rows across sub-accounts (used to derive assets + available). */
  balances: Balance[];
  initialFrom?: AccountType;
  initialTo?: AccountType;
  initialAsset?: string;
  /** Called after a successful transfer so callers can refresh their data. */
  onSuccess?: () => void;
}

/** Maps backend 400 payloads (and transport errors) to a localized message. */
function useTransferErrorMessage() {
  const { t } = useTranslation();
  return (err: unknown): string => {
    if (isAxiosError(err)) {
      const data = err.response?.data as { error?: string } | undefined;
      if (data?.error === 'insufficient balance') return t('transfer.insufficientBalance');
      if (data?.error) return data.error;
    }
    if (err instanceof Error && err.message) return err.message;
    return t('transfer.failed');
  };
}

export function TransferModal({
  open,
  onClose,
  balances,
  initialFrom,
  initialTo,
  initialAsset,
  onSuccess,
}: TransferModalProps) {
  const { t } = useTranslation();
  const errorMessage = useTransferErrorMessage();

  const [from, setFrom] = useState<AccountType>(initialFrom ?? 'spot');
  const [to, setTo] = useState<AccountType>(initialTo ?? 'futures');
  const [asset, setAsset] = useState(initialAsset ?? 'USDT');
  const [amount, setAmount] = useState('');
  const [submitting, setSubmitting] = useState(false);

  // (Re)initialize the form every time the modal opens.
  useEffect(() => {
    if (!open) return;
    const f = initialFrom ?? 'spot';
    setFrom(f);
    setTo(initialTo && initialTo !== f ? initialTo : ACCOUNT_TYPES.find((a) => a !== f) ?? 'futures');
    setAsset(initialAsset ?? 'USDT');
    setAmount('');
  }, [open, initialFrom, initialTo, initialAsset]);

  // Close on Escape.
  useEffect(() => {
    if (!open) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose();
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [open, onClose]);

  const accountLabel = (a: AccountType) => t(`transfer.account.${a}`);

  /** Rows of the source account, keeping only assets that actually hold a balance. */
  const fromAssets = useMemo(() => {
    return (balances || []).filter(
      (b) => b.account_type === from && Number.parseFloat(b.balance || '0') > 0
    );
  }, [balances, from]);

  // Keep the selected asset valid whenever the source account changes.
  useEffect(() => {
    if (!open) return;
    if (!fromAssets.some((b) => b.asset === asset)) {
      setAsset(fromAssets[0]?.asset ?? '');
      setAmount('');
    }
  }, [open, fromAssets, asset]);

  const sourceRow = useMemo(
    () => fromAssets.find((b) => b.asset === asset),
    [fromAssets, asset]
  );
  const available = Number.parseFloat(sourceRow?.available || '0') || 0;

  const changeFrom = (next: AccountType) => {
    setFrom(next);
    if (next === to) {
      setTo(ACCOUNT_TYPES.find((a) => a !== next) ?? 'futures');
    }
  };

  const swapAccounts = () => {
    setFrom(to);
    setTo(from);
    setAmount('');
  };

  const fillAll = () => {
    if (available > 0) setAmount(String(available));
  };

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (submitting) return;
    const amt = Number.parseFloat(amount);
    if (!asset || !Number.isFinite(amt) || amt <= 0) {
      toast.error(t('transfer.invalidAmount'));
      return;
    }
    if (amt > available) {
      toast.error(t('transfer.insufficientBalance'));
      return;
    }
    setSubmitting(true);
    try {
      await transfer(from, to, asset, amount);
      toast.success(
        t('transfer.success'),
        `${accountLabel(from)} → ${accountLabel(to)} · ${amount} ${asset}`
      );
      onSuccess?.();
      onClose();
    } catch (err: unknown) {
      toast.error(errorMessage(err));
    } finally {
      setSubmitting(false);
    }
  };

  if (!open) return null;

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/70 p-4 backdrop-blur-sm"
      onClick={onClose}
    >
      <div
        className="animate-in w-full max-w-md rounded-xl border border-nexa-700 bg-nexa-900 shadow-2xl shadow-black/50"
        onClick={(e) => e.stopPropagation()}
      >
        {/* Header */}
        <div className="flex items-center justify-between border-b border-nexa-700/70 bg-nexa-900/60 px-5 py-3.5">
          <div className="flex items-center gap-2 text-sm font-semibold text-nexa-100">
            <svg viewBox="0 0 24 24" fill="none" className="h-4 w-4 text-cta">
              <path d="M4 7h13m0 0l-3-3m3 3l-3 3M20 17H7m0 0l3-3m-3 3l3 3" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
            </svg>
            {t('transfer.title')}
          </div>
          <button
            type="button"
            onClick={onClose}
            aria-label={t('common.close')}
            className="rounded-md p-1 text-nexa-400 transition-colors hover:bg-nexa-800 hover:text-nexa-100"
          >
            <svg viewBox="0 0 24 24" fill="none" className="h-4 w-4">
              <path d="M6 6l12 12M18 6L6 18" stroke="currentColor" strokeWidth="2" strokeLinecap="round" />
            </svg>
          </button>
        </div>

        <form onSubmit={handleSubmit} className="space-y-4 p-5">
          {/* From → To */}
          <div className="space-y-1.5">
            <Select
              label={t('transfer.from')}
              value={from}
              onChange={(e) => changeFrom(e.target.value as AccountType)}
              options={ACCOUNT_TYPES.map((a) => ({ value: a, label: accountLabel(a) }))}
            />
            <div className="flex justify-center">
              <button
                type="button"
                onClick={swapAccounts}
                title={t('transfer.swap')}
                className="rounded-full border border-nexa-700 bg-nexa-800 p-1.5 text-nexa-300 transition-all hover:border-cta/60 hover:text-cta-bright active:scale-95"
              >
                <svg viewBox="0 0 24 24" fill="none" className="h-3.5 w-3.5">
                  <path d="M7 4v13m0 0l-3-3m3 3l3-3M17 20V7m0 0l-3 3m3-3l3 3" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
                </svg>
              </button>
            </div>
            <Select
              label={t('transfer.to')}
              value={to}
              onChange={(e) => setTo(e.target.value as AccountType)}
            >
              {ACCOUNT_TYPES.map((a) => (
                <option key={a} value={a} disabled={a === from}>
                  {accountLabel(a)}
                </option>
              ))}
            </Select>
          </div>

          {/* Same-account guard is enforced by disabling the option above. */}
          {from === to && (
            <p className="text-xs text-down">{t('transfer.sameAccount')}</p>
          )}

          {/* Asset (only assets held in the source account) */}
          {fromAssets.length > 0 ? (
            <Select
              label={t('transfer.asset')}
              value={asset}
              onChange={(e) => setAsset(e.target.value)}
              options={fromAssets.map((b) => ({ value: b.asset, label: b.asset }))}
            />
          ) : (
            <div className="rounded-lg border border-nexa-700/70 bg-nexa-800/50 px-3 py-2.5 text-xs text-nexa-400">
              {t('transfer.noAssets')}
            </div>
          )}

          {/* Amount with Max shortcut + live available */}
          <div>
            <div className="mb-1 flex items-center justify-between">
              <span className="text-xs font-medium text-nexa-300">{t('transfer.amount')}</span>
              <button
                type="button"
                onClick={fillAll}
                disabled={available <= 0}
                className="text-xs font-semibold text-cta-bright transition-colors hover:text-cta disabled:opacity-40"
              >
                {t('transfer.all')}
              </button>
            </div>
            <Input
              type="number"
              step="0.000001"
              min="0"
              placeholder="0.00"
              value={amount}
              onChange={(e) => setAmount(e.target.value)}
              disabled={!asset}
              suffix={asset || ''}
              required
            />
            <div className="mt-1.5 flex items-center justify-between text-xs">
              <span className="text-nexa-500">{t('transfer.available')}</span>
              <span className={cls('font-mono', available > 0 ? 'text-nexa-200' : 'text-nexa-500')}>
                {formatQty(sourceRow?.available || '0', 8)} {asset}
              </span>
            </div>
          </div>

          <div className="rounded-lg border border-nexa-700/60 bg-nexa-800/40 px-3 py-2 text-xs text-nexa-400">
            {t('transfer.hint')}
          </div>

          <Button
            type="submit"
            block
            isLoading={submitting}
            disabled={!asset || fromAssets.length === 0 || from === to}
          >
            {t('transfer.confirm')}
          </Button>
        </form>
      </div>
    </div>
  );
}
