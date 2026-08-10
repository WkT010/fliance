import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { cls } from '@/utils/format';

const FAQ_KEYS = ['faqRegister', 'faqDeposit', 'faqMatching', 'faqSecurity', 'faqProducts'];

export function FaqSection() {
  const { t } = useTranslation();
  const [open, setOpen] = useState<number | null>(0);

  return (
    <section className="bg-bg1">
      <div className="mx-auto max-w-3xl px-6 py-16 lg:py-24">
        <h2 className="text-center text-2xl font-semibold text-pri lg:text-3xl">
          {t('landing.faqTitle')}
        </h2>
        <p className="mt-3 text-center text-sm text-sec">{t('landing.faqSubtitle')}</p>

        <div className="mt-10">
          {FAQ_KEYS.map((key, idx) => {
            const expanded = open === idx;
            return (
              <div key={key} className="border-b border-line">
                <button
                  onClick={() => setOpen(expanded ? null : idx)}
                  aria-expanded={expanded}
                  className="flex w-full items-center justify-between gap-4 py-5 text-left"
                >
                  <span
                    className={cls(
                      'text-base transition-colors',
                      expanded ? 'font-semibold text-pri' : 'font-medium text-sec hover:text-pri'
                    )}
                  >
                    {t(`landing.faq.${key}.q`)}
                  </span>
                  <svg
                    viewBox="0 0 24 24"
                    fill="none"
                    className={cls(
                      'h-4 w-4 flex-shrink-0 text-third transition-transform duration-200',
                      expanded && 'rotate-45 text-cta'
                    )}
                  >
                    <path d="M12 5v14M5 12h14" stroke="currentColor" strokeWidth="2" strokeLinecap="round" />
                  </svg>
                </button>
                {expanded && (
                  <div className="animate-fade-in pb-5 pr-8 text-sm leading-relaxed text-sec">
                    {t(`landing.faq.${key}.a`)}
                  </div>
                )}
              </div>
            );
          })}
        </div>
      </div>
    </section>
  );
}
