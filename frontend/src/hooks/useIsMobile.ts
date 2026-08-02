import { useEffect, useState } from 'react';

// useIsMobile reports whether the viewport matches the mobile breakpoint
// (<= 768px). Used to switch layouts between desktop multi-column and mobile
// stacked/scrollable variants, and to gate touch-friendly affordances.
export function useIsMobile(breakpoint = 768): boolean {
  const [isMobile, setIsMobile] = useState(() => {
    if (typeof window === 'undefined') return false;
    return window.matchMedia(`(max-width: ${breakpoint}px)`).matches;
  });

  useEffect(() => {
    if (typeof window === 'undefined') return;
    const mql = window.matchMedia(`(max-width: ${breakpoint}px)`);
    const handler = (e: MediaQueryListEvent) => setIsMobile(e.matches);
    // addEventListener is the modern API; older Safari uses addListener.
    if (mql.addEventListener) {
      mql.addEventListener('change', handler);
      return () => mql.removeEventListener('change', handler);
    }
    mql.addListener(handler);
    return () => mql.removeListener(handler);
  }, [breakpoint]);

  return isMobile;
}
