import { useEffect, useRef } from 'react';

export function usePolling(fn: () => void | Promise<void>, delay: number, deps: unknown[] = []) {
  const savedRef = useRef(fn);
  savedRef.current = fn;

  useEffect(() => {
    let mounted = true;
    const tick = async () => {
      if (!mounted) return;
      try { await savedRef.current(); } catch { /* ignore */ }
    };
    tick();
    const id = setInterval(tick, delay);
    return () => {
      mounted = false;
      clearInterval(id);
    };
  }, [delay, ...deps]);
}
