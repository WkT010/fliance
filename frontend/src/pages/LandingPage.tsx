import { Header } from '@/components/Header';
import { Hero } from '@/components/landing/Hero';
import { MarketTabs } from '@/components/landing/MarketTabs';
import { TrustSection } from '@/components/landing/TrustSection';
import { FaqSection } from '@/components/landing/FaqSection';
import { BottomCta } from '@/components/landing/BottomCta';
import { LandingFooter } from '@/components/landing/LandingFooter';
import { useLandingMarket } from '@/components/landing/useLandingMarket';

/**
 * Fliance public landing page — Binance-style dark design system.
 * Section rhythm: Hero (#202630) → Markets (#202630) → Security (#181A20)
 * → FAQ (#202630) → CTA band (#FCD535) → Footer (#0B0E11).
 */
export function LandingPage() {
  const market = useLandingMarket();

  return (
    <div className="min-h-screen bg-bg1 text-pri">
      <Header />
      <main>
        <Hero market={market} />
        <MarketTabs market={market} />
        <TrustSection />
        <FaqSection />
        <BottomCta />
      </main>
      <LandingFooter />
    </div>
  );
}
