import { useEffect, useRef, useState } from 'react';

interface CountUpProps {
  /** Target numeric value; animates 0 → value on mount, then smoothly to each new value. */
  value: number;
  /** Formats the currently displayed number. */
  format: (n: number) => string;
}

const easeOutCubic = (t: number) => 1 - Math.pow(1 - t, 3);

/**
 * requestAnimationFrame count-up used by the hero stats strip.
 * First run animates 0 → target over ~1.2s (easeOut); later polling updates
 * glide from the currently displayed value over 0.6s. Honors reduced motion.
 */
export function CountUp({ value, format }: CountUpProps) {
  const [display, setDisplay] = useState(0);
  const fromRef = useRef(0);
  const rafRef = useRef(0);
  const mountedRef = useRef(false);

  useEffect(() => {
    const target = Number.isFinite(value) ? value : 0;

    if (typeof window.matchMedia === 'function' && window.matchMedia('(prefers-reduced-motion: reduce)').matches) {
      fromRef.current = target;
      setDisplay(target);
      return;
    }

    const from = fromRef.current;
    if (from === target) {
      setDisplay(target);
      return;
    }

    const duration = mountedRef.current ? 600 : 1200;
    const start = performance.now();
    const tick = (now: number) => {
      const p = Math.min(1, (now - start) / duration);
      const cur = from + (target - from) * easeOutCubic(p);
      fromRef.current = cur;
      setDisplay(cur);
      if (p < 1) rafRef.current = requestAnimationFrame(tick);
    };
    rafRef.current = requestAnimationFrame(tick);
    mountedRef.current = true;
    return () => cancelAnimationFrame(rafRef.current);
  }, [value]);

  return <>{format(display)}</>;
}
