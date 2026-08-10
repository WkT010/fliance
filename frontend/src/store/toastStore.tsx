import { create } from 'zustand';
import { useEffect } from 'react';
import { ToastContainer, type Toast, type ToastType } from '@/components/common/Toast';

interface ToastState {
  toasts: Toast[];
  show: (msg: string | { type?: ToastType; title?: string; message: string; duration?: number }, opts?: { type?: ToastType; title?: string; duration?: number }) => string;
  dismiss: (id: string) => void;
  clear: () => void;
}

let _id = 0;
const nextId = () => `toast-${++_id}-${Date.now()}`;

export const useToastStore = create<ToastState>((set, get) => ({
  toasts: [],
  show: (msg, opts) => {
    const id = nextId();
    let toast: Toast;
    if (typeof msg === 'string') {
      toast = { id, type: opts?.type ?? 'info', title: opts?.title, message: msg, duration: opts?.duration };
    } else {
      toast = { id, type: msg.type ?? 'info', title: msg.title, message: msg.message, duration: msg.duration };
    }
    set({ toasts: [...get().toasts, toast] });
    return id;
  },
  dismiss: (id) => set({ toasts: get().toasts.filter((t) => t.id !== id) }),
  clear: () => set({ toasts: [] }),
}));

/** Convenience helpers for the common cases. */
export const toast = {
  success: (message: string, title?: string) => useToastStore.getState().show({ type: 'success', message, title }),
  error: (message: string, title?: string) => useToastStore.getState().show({ type: 'error', message, title }),
  info: (message: string, title?: string) => useToastStore.getState().show({ type: 'info', message, title }),
  warning: (message: string, title?: string) => useToastStore.getState().show({ type: 'warning', message, title }),
};

export function ToastHost() {
  const toasts = useToastStore((s) => s.toasts);
  const dismiss = useToastStore((s) => s.dismiss);
  useEffect(() => {
    // Auto-clear on unmount not needed; container unmounts with host.
  }, []);
  return <ToastContainer toasts={toasts} onClose={dismiss} />;
}
