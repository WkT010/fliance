import { Link } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { LANGS, setLanguage } from '@/i18n';
import { cls } from '@/utils/format';

export function LandingFooter() {
  const { t, i18n } = useTranslation();

  const legalLinks = [
    { to: '/legal/terms', labelKey: 'footer.terms' },
    { to: '/legal/privacy', labelKey: 'footer.privacy' },
    { to: '/legal/risk', labelKey: 'footer.risk' },
    { to: '/legal/aml', labelKey: 'footer.aml' },
    { to: '/legal/cookies', labelKey: 'footer.cookies' },
  ];

  return (
    <footer className="bg-bg3">
      <div className="mx-auto max-w-6xl px-6 py-16">
        <div className="flex flex-col gap-10 md:flex-row md:items-start md:justify-between">
          {/* Brand */}
          <div className="max-w-xs">
            <div className="flex items-center gap-2">
              <img src="/fliance-logo.png" alt="Fliance" className="h-8 w-8 rounded" />
              <span className="text-xl font-semibold text-pri">
                Fliance<span className="ml-1.5 text-xs font-medium text-third">梵响</span>
              </span>
            </div>
            <p className="mt-4 text-sm leading-relaxed text-third">{t('landing.footerSlogan')}</p>
          </div>

          {/* Legal */}
          <div>
            <div className="text-xs font-semibold uppercase tracking-wider text-third">
              {t('footer.legal')}
            </div>
            <nav className="mt-4 grid grid-cols-1 gap-2.5 sm:grid-cols-2 sm:gap-x-12">
              {legalLinks.map((l) => (
                <Link
                  key={l.to}
                  to={l.to}
                  className="text-sm text-sec transition-colors hover:text-pri"
                >
                  {t(l.labelKey)}
                </Link>
              ))}
            </nav>
          </div>

          {/* Language */}
          <div>
            <div className="text-xs font-semibold uppercase tracking-wider text-third">
              {t('common.language')}
            </div>
            <div className="mt-4 inline-flex items-center rounded border border-line bg-bg2 p-0.5 text-sm">
              {LANGS.map((l) => (
                <button
                  key={l.code}
                  onClick={() => setLanguage(l.code)}
                  className={cls(
                    'rounded px-4 py-1.5 font-medium transition-all',
                    i18n.language === l.code
                      ? 'bg-cta/15 text-cta'
                      : 'text-sec hover:text-pri'
                  )}
                  aria-pressed={i18n.language === l.code}
                >
                  {l.label}
                </button>
              ))}
            </div>
          </div>
        </div>

        <div className="mt-12 border-t border-line pt-6">
          <p className="text-xs text-third">{t('footer.company')}</p>
          <p className="mt-2 text-xs text-third">{t('footer.rights')}</p>
          <p className="mt-2 text-xs text-third/70">{t('footer.trademark')}</p>
        </div>
      </div>
    </footer>
  );
}
