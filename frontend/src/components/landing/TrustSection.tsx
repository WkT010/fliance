import { useTranslation } from 'react-i18next';

interface SecurityItem {
  stat: string;
  titleKey: string;
  descKey: string;
  statKey: string;
  icon: React.ReactNode;
}

export function TrustSection() {
  const { t } = useTranslation();

  const items: SecurityItem[] = [
    {
      stat: '150+',
      statKey: 'landing.security.risk.statLabel',
      titleKey: 'landing.security.risk.title',
      descKey: 'landing.security.risk.desc',
      icon: (
        <svg viewBox="0 0 24 24" fill="none" className="h-6 w-6">
          <path d="M12 3l7 3v5c0 4.5-3 8.2-7 10-4-1.8-7-5.5-7-10V6l7-3z" stroke="currentColor" strokeWidth="1.8" strokeLinejoin="round" />
          <path d="M9 12l2 2 4-4" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" />
        </svg>
      ),
    },
    {
      stat: '95%',
      statKey: 'landing.security.cold.statLabel',
      titleKey: 'landing.security.cold.title',
      descKey: 'landing.security.cold.desc',
      icon: (
        <svg viewBox="0 0 24 24" fill="none" className="h-6 w-6">
          <rect x="4" y="10" width="16" height="9" rx="2" stroke="currentColor" strokeWidth="1.8" />
          <path d="M8 10V7a4 4 0 018 0v3" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" />
          <circle cx="12" cy="14.5" r="1.4" fill="currentColor" />
        </svg>
      ),
    },
    {
      stat: '100%',
      statKey: 'landing.security.audit.statLabel',
      titleKey: 'landing.security.audit.title',
      descKey: 'landing.security.audit.desc',
      icon: (
        <svg viewBox="0 0 24 24" fill="none" className="h-6 w-6">
          <circle cx="11" cy="11" r="6" stroke="currentColor" strokeWidth="1.8" />
          <path d="M20 20l-4.5-4.5" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" />
          <path d="M8.5 11l1.8 1.8 3.2-3.6" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" />
        </svg>
      ),
    },
    {
      stat: '1:1',
      statKey: 'landing.security.reserves.statLabel',
      titleKey: 'landing.security.reserves.title',
      descKey: 'landing.security.reserves.desc',
      icon: (
        <svg viewBox="0 0 24 24" fill="none" className="h-6 w-6">
          <rect x="3" y="10" width="8" height="8" rx="1.5" stroke="currentColor" strokeWidth="1.8" />
          <rect x="13" y="6" width="8" height="8" rx="1.5" stroke="currentColor" strokeWidth="1.8" />
          <path d="M11 14h2M13 10h-2" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" />
        </svg>
      ),
    },
  ];

  return (
    <section className="bg-bg2">
      <div className="mx-auto max-w-6xl px-6 py-16 lg:py-24">
        <div className="max-w-2xl">
          <div className="inline-flex items-center gap-2 rounded bg-cta/10 px-2.5 py-1 text-[11px] font-semibold uppercase tracking-wider text-cta">
            {t('landing.securityBadge')}
          </div>
          <h2 className="mt-4 text-2xl font-semibold text-pri lg:text-3xl">
            {t('landing.securityTitle')}
          </h2>
          <p className="mt-3 text-sm leading-relaxed text-sec lg:text-base">
            {t('landing.securitySubtitle')}
          </p>
        </div>

        <div className="mt-12 grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
          {items.map((it) => (
            <div
              key={it.titleKey}
              className="rounded-lg border border-line bg-bg2 p-6 transition-all duration-300 hover:border-cta/50 hover:bg-container lg:p-8"
            >
              <div className="flex h-10 w-10 items-center justify-center rounded bg-cta/10 text-cta">
                {it.icon}
              </div>
              <div className="mt-6 text-3xl font-medium text-cta lg:text-4xl">{it.stat}</div>
              <div className="mt-1 text-xs font-medium uppercase tracking-wider text-third">
                {t(it.statKey)}
              </div>
              <h3 className="mt-4 text-base font-semibold text-pri">{t(it.titleKey)}</h3>
              <p className="mt-2 text-sm leading-relaxed text-sec">{t(it.descKey)}</p>
            </div>
          ))}
        </div>
      </div>
    </section>
  );
}
