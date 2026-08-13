import { useEffect, useRef, useState, type ReactNode } from 'react';
import { cls } from '@/utils/format';

interface RevealProps {
  children: ReactNode;
  className?: string;
  /** Stagger offset in ms applied via transition-delay. */
  delay?: number;
}

/**
 * MetaMask-style scroll reveal: fades in + slides up once when the block
 * enters the viewport (IntersectionObserver, threshold 0.15, fires once).
 * Falls back to always-visible when reduced motion is preferred or the
 * IntersectionObserver API is unavailable.
 */
export function Reveal({ children, className, delay = 0 }: RevealProps) {
  const ref = useRef<HTMLDivElement>(null);
  const [visible, setVisible] = useState(() =>
    typeof window !== 'undefined' &&
    typeof window.matchMedia === 'function' &&
    window.matchMedia('(prefers-reduced-motion: reduce)').matches
  );

  useEffect(() => {
    if (visible) return;
    const el = ref.current;
    if (!el || typeof IntersectionObserver === 'undefined') {
      setVisible(true);
      return;
    }
    const io = new IntersectionObserver(
      (entries) => {
        if (entries.some((e) => e.isIntersecting)) {
          setVisible(true);
          io.disconnect(); // reveal only once
        }
      },
      { threshold: 0.15 }
    );
    io.observe(el);
    return () => io.disconnect();
  }, [visible]);

  return (
    <div
      ref={ref}
      className={cls('reveal', visible && 'reveal-visible', className)}
      style={delay ? { transitionDelay: `${delay}ms` } : undefined}
    >
      {children}
    </div>
  );
}
