import { Header } from '@/components/Header';
import { Hero } from '@/components/landing/Hero';
import { MarketTabs } from '@/components/landing/MarketTabs';
import { TrustSection } from '@/components/landing/TrustSection';
import { FaqSection } from '@/components/landing/FaqSection';
import { BottomCta } from '@/components/landing/BottomCta';
import { LandingFooter } from '@/components/landing/LandingFooter';
import { Reveal } from '@/components/landing/Reveal';
import { useLandingMarket } from '@/components/landing/useLandingMarket';

/**
 * Fliance public landing page — dark design system with teal brand tokens.
 * Section rhythm: Hero (#202630) → Markets (#202630) → Security (#181A20)
 * → FAQ (#202630) → CTA band (#14B8A6) → Footer (#0B0E11).
 * Below-the-fold sections enter with a one-shot scroll reveal.
 */
export function LandingPage() {
  const market = useLandingMarket();

  return (
    <div className="min-h-screen bg-bg1 text-pri">
      <Header />
      <main>
        <Hero market={market} />
        <Reveal>
          <MarketTabs market={market} />
        </Reveal>
        <Reveal>
          <TrustSection />
        </Reveal>
        <Reveal>
          <FaqSection />
        </Reveal>
        <Reveal>
          <BottomCta />
        </Reveal>
      </main>
      <LandingFooter />
    </div>
  );
}
