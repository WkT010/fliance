import { useEffect } from 'react';
import { cls } from '@/utils/format';

export type ToastType = 'success' | 'error' | 'info' | 'warning';

export interface Toast {
  id: string;
  type: ToastType;
  title?: string;
  message: string;
  duration?: number;
}

const ICONS: Record<ToastType, React.ReactNode> = {
  success: (
    <svg viewBox="0 0 24 24" fill="none" className="h-5 w-5">
      <path d="M5 13l4 4L19 7" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  ),
  error: (
    <svg viewBox="0 0 24 24" fill="none" className="h-5 w-5">
      <path d="M12 8v5M12 16v.5" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round" />
      <circle cx="12" cy="12" r="9" stroke="currentColor" strokeWidth="2" />
    </svg>
  ),
  info: (
    <svg viewBox="0 0 24 24" fill="none" className="h-5 w-5">
      <path d="M12 16V11M12 8v.5" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round" />
      <circle cx="12" cy="12" r="9" stroke="currentColor" strokeWidth="2" />
    </svg>
  ),
  warning: (
    <svg viewBox="0 0 24 24" fill="none" className="h-5 w-5">
      <path d="M12 9v4M12 16v.5" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round" />
      <path d="M2.5 20L12 4l9.5 16H2.5z" stroke="currentColor" strokeWidth="2" strokeLinejoin="round" />
    </svg>
  ),
};

const STYLES: Record<ToastType, { ring: string; icon: string; bar: string; bg: string }> = {
  success: {
    ring: 'ring-up/40',
    icon: 'text-up bg-up/15',
    bar: 'bg-up',
    bg: 'bg-nexa-900/95',
  },
  error: {
    ring: 'ring-down/40',
    icon: 'text-down bg-down/15',
    bar: 'bg-down',
    bg: 'bg-nexa-900/95',
  },
  info: {
    ring: 'ring-accent/40',
    icon: 'text-accent bg-accent/15',
    bar: 'bg-accent',
    bg: 'bg-nexa-900/95',
  },
  warning: {
    ring: 'ring-cta/40',
    icon: 'text-cta bg-cta/15',
    bar: 'bg-cta',
    bg: 'bg-nexa-900/95',
  },
};

interface ToastItemProps {
  toast: Toast;
  onClose: (id: string) => void;
}

function ToastItem({ toast, onClose }: ToastItemProps) {
  const style = STYLES[toast.type];
  const duration = toast.duration ?? (toast.type === 'error' ? 6000 : 3500);

  useEffect(() => {
    const timer = setTimeout(() => onClose(toast.id), duration);
    return () => clearTimeout(timer);
  }, [toast.id, duration, onClose]);

  return (
    <div
      role="status"
      className={cls(
        'pointer-events-auto relative flex w-80 max-w-[90vw] items-start gap-3 overflow-hidden rounded-xl',
        'border border-nexa-700/80 shadow-2xl backdrop-blur-md ring-1',
        style.ring,
        style.bg,
        'animate-in slide-in-from-right-full fade-in duration-200'
      )}
    >
      <div className={cls('absolute left-0 top-0 h-full w-1', style.bar)} />
      <div className={cls('ml-3 mt-3 flex h-8 w-8 flex-shrink-0 items-center justify-center rounded-lg', style.icon)}>
        {ICONS[toast.type]}
      </div>
      <div className="min-w-0 flex-1 py-3 pr-3">
        {toast.title && <div className="text-sm font-semibold text-nexa-100">{toast.title}</div>}
        <div className={cls('text-sm text-nexa-200', toast.title && 'mt-0.5')}>{toast.message}</div>
      </div>
      <button
        onClick={() => onClose(toast.id)}
        className="m-2 flex h-7 w-7 flex-shrink-0 items-center justify-center rounded-md text-nexa-500 transition-colors hover:bg-nexa-800 hover:text-nexa-100"
        aria-label="Close"
      >
        <svg viewBox="0 0 24 24" fill="none" className="h-4 w-4">
          <path d="M6 6l12 12M6 18L18 6" stroke="currentColor" strokeWidth="2" strokeLinecap="round" />
        </svg>
      </button>
    </div>
  );
}

interface ToastContainerProps {
  toasts: Toast[];
  onClose: (id: string) => void;
}

export function ToastContainer({ toasts, onClose }: ToastContainerProps) {
  if (toasts.length === 0) return null;
  return (
    <div className="pointer-events-none fixed right-4 top-4 z-[100] flex flex-col gap-2 sm:right-6 sm:top-6">
      {toasts.map((t) => (
        <ToastItem key={t.id} toast={t} onClose={onClose} />
      ))}
    </div>
  );
}
