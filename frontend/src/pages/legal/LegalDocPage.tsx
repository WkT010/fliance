import { useEffect } from 'react';
import { Link, useLocation } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { Layout } from '@/components/Layout';
import { cls } from '@/utils/format';

/** Operating entity shown across every legal document (full bilingual form). */
export const OPERATOR = '凌嘉凡响网络科技有限公司（Canival Institute Inc.）';
/** English-only form of the operating entity, used in English contexts. */
export const OPERATOR_EN = 'Canival Institute Inc.';
/** Full brand display form. */
export const BRAND = 'Fliance（梵响）';

export interface LegalSection {
  heading: string;
  body: string[];
  /** Render as a highlighted risk / warning callout. */
  warning?: boolean;
}

export interface LegalDocContent {
  title: string;
  subtitle: string;
  effective: string;
  intro: string[];
  sections: LegalSection[];
}

export interface LegalDocMeta {
  path: string;
  zh: string;
  en: string;
  zhDesc: string;
  enDesc: string;
}

export const LEGAL_DOCS: LegalDocMeta[] = [
  {
    path: '/legal/terms',
    zh: '服务条款',
    en: 'Terms of Service',
    zhDesc: '平台使用规则、账户义务与责任限制',
    enDesc: 'Platform usage rules, account obligations and liability limits',
  },
  {
    path: '/legal/privacy',
    zh: '隐私政策',
    en: 'Privacy Policy',
    zhDesc: '信息收集、使用、存储与用户权利',
    enDesc: 'How we collect, use, store and protect your data',
  },
  {
    path: '/legal/risk',
    zh: '风险披露',
    en: 'Risk Disclosure',
    zhDesc: '数字资产交易风险与损失警示',
    enDesc: 'Risks of digital asset trading and loss warnings',
  },
  {
    path: '/legal/aml',
    zh: '反洗钱政策',
    en: 'AML Policy',
    zhDesc: 'KYC 身份核验、交易监控与可疑上报',
    enDesc: 'KYC verification, transaction monitoring and reporting',
  },
  {
    path: '/legal/cookies',
    zh: 'Cookie 政策',
    en: 'Cookie Policy',
    zhDesc: 'Cookie 类型、用途与管理方式',
    enDesc: 'Cookie types, purposes and how to manage them',
  },
];

/**
 * Shared editorial-style renderer for all policy documents. Content is
 * inlined per page (bilingual zh/en) and follows the active UI language.
 */
export function LegalDocPage({ zh, en }: { zh: LegalDocContent; en: LegalDocContent }) {
  const { i18n } = useTranslation();
  const location = useLocation();
  const isEn = i18n.language === 'en';
  const doc = isEn ? en : zh;

  // Scroll the main pane back to the top whenever the document changes.
  useEffect(() => {
    document.querySelector('main')?.scrollTo({ top: 0 });
  }, [location.pathname]);

  return (
    <Layout>
      <div className="mx-auto flex w-full max-w-6xl gap-8 px-4 py-8 md:px-6">
        {/* ── Sidebar: document index ── */}
        <aside className="sticky top-0 hidden h-fit w-56 flex-shrink-0 lg:block">
          <div className="mb-3 text-[11px] font-semibold uppercase tracking-[0.18em] text-nexa-500">
            {isEn ? 'Legal Documents' : '法律文件'}
          </div>
          <nav className="space-y-1">
            {LEGAL_DOCS.map((d) => {
              const active = location.pathname === d.path;
              return (
                <Link
                  key={d.path}
                  to={d.path}
                  className={cls(
                    'block rounded-lg border px-3 py-2 text-sm transition-colors',
                    active
                      ? 'border-accent/40 bg-accent/10 font-semibold text-accent'
                      : 'border-transparent text-nexa-300 hover:bg-nexa-800/60 hover:text-nexa-100'
                  )}
                >
                  {isEn ? d.en : d.zh}
                </Link>
              );
            })}
          </nav>
          <div className="mt-6 rounded-lg border border-nexa-700/70 bg-nexa-900/40 p-3 text-xs leading-relaxed text-nexa-500">
            <div className="mb-1 font-medium text-nexa-300">{isEn ? 'Operator' : '运营主体'}</div>
            {OPERATOR}
          </div>
        </aside>

        {/* ── Document body ── */}
        <article className="min-w-0 flex-1">
          <header className="border-b border-nexa-700/70 pb-6">
            <div className="mb-3 flex flex-wrap items-center gap-2">
              <span className="rounded-full border border-accent/40 bg-accent/10 px-3 py-1 text-[11px] font-semibold uppercase tracking-wider text-accent">
                {BRAND}
              </span>
              <span className="rounded-full border border-nexa-700 bg-nexa-900/60 px-3 py-1 text-[11px] text-nexa-400">
                {doc.effective}
              </span>
            </div>
            <h1 className="text-3xl font-bold tracking-tight text-nexa-100">{doc.title}</h1>
            <p className="mt-2 text-sm text-nexa-400">{doc.subtitle}</p>
          </header>

          <div className="mt-6 space-y-4 text-sm leading-relaxed text-nexa-300">
            {doc.intro.map((p, i) => (
              <p key={i}>{p}</p>
            ))}
          </div>

          <ol className="mt-8 space-y-8">
            {doc.sections.map((s, i) => (
              <li key={i}>
                {s.warning ? (
                  <div className="rounded-xl border border-down/40 bg-down/10 p-5">
                    <h2 className="mb-3 flex items-start gap-3 text-base font-bold text-nexa-100">
                      <span className="mt-0.5 flex h-6 w-6 flex-shrink-0 items-center justify-center rounded-md bg-down/20 font-mono text-xs font-bold text-down">
                        {i + 1}
                      </span>
                      {s.heading}
                    </h2>
                    <div className="space-y-3 pl-9 text-sm leading-relaxed text-nexa-300">
                      {s.body.map((p, j) => (
                        <p key={j} className={cls(j === s.body.length - 1 && 'font-semibold text-down')}>{p}</p>
                      ))}
                    </div>
                  </div>
                ) : (
                  <section>
                    <h2 className="mb-3 flex items-start gap-3 text-base font-bold text-nexa-100">
                      <span className="mt-0.5 flex h-6 w-6 flex-shrink-0 items-center justify-center rounded-md bg-accent/10 font-mono text-xs font-bold text-accent">
                        {i + 1}
                      </span>
                      {s.heading}
                    </h2>
                    <div className="space-y-3 pl-9 text-nexa-300">
                      {s.body.map((p, j) => (
                        <p key={j}>{p}</p>
                      ))}
                    </div>
                  </section>
                )}
              </li>
            ))}
          </ol>

          <footer className="mt-12 border-t border-nexa-700/70 pt-6 text-xs leading-relaxed text-nexa-500">
            <p>
              {isEn
                ? `This document is issued by ${OPERATOR_EN}, the operator of ${BRAND}. In the event of any discrepancy between the Chinese and English versions, the Chinese version shall prevail.`
                : `本文件由 ${BRAND} 平台运营主体 ${OPERATOR} 发布。如中英文版本存在任何不一致之处，以中文版本为准。`}
            </p>
          </footer>
        </article>
      </div>
    </Layout>
  );
}
