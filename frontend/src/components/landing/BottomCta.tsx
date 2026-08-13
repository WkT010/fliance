import { Link } from 'react-router-dom';
import { useTranslation } from 'react-i18next';

export function BottomCta() {
  const { t } = useTranslation();
  return (
    <section className="bg-cta">
      <div className="mx-auto flex max-w-6xl flex-col items-center justify-between gap-6 px-6 py-14 md:flex-row lg:py-16">
        <div>
          <h2 className="text-2xl font-semibold text-bg1 lg:text-3xl">{t('landing.ctaTitle')}</h2>
          <p className="mt-2 text-sm font-medium text-bg1/90">{t('landing.ctaSubtitle')}</p>
        </div>
        <Link
          to="/register"
          className="inline-flex h-12 w-[180px] flex-shrink-0 items-center justify-center rounded-lg bg-bg1 text-base font-semibold text-cta transition-all duration-200 hover:-translate-y-0.5 hover:bg-bg3 hover:shadow-[0_10px_30px_-6px_rgba(11,14,17,0.55)]"
        >
          {t('landing.ctaRegister')}
        </Link>
      </div>
    </section>
  );
}
