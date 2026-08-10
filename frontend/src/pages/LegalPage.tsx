import { Link } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { Layout } from '@/components/Layout';
import { LEGAL_DOCS, OPERATOR, OPERATOR_EN, BRAND } from '@/pages/legal/LegalDocPage';

export function LegalPage() {
  const { i18n } = useTranslation();
  const isEn = i18n.language === 'en';

  return (
    <Layout>
      <div className="p-4 pb-8">
        <div className="mx-auto max-w-4xl space-y-6 pb-8">
          <div className="space-y-1">
            <h1 className="text-2xl font-bold text-nexa-100">
              {isEn ? 'Legal & Compliance' : '法律与合规'}
            </h1>
            <p className="text-sm text-nexa-400">
              {isEn
                ? `${BRAND} · Version 1.0 — Effective August 2026`
                : `${BRAND} · 版本 1.0 — 2026年8月生效`}
            </p>
          </div>

          <div className="rounded-xl border border-nexa-700/70 bg-nexa-800/60 p-5 text-sm leading-relaxed text-nexa-300">
            <p>
              {isEn
                ? `${BRAND} is operated by ${OPERATOR_EN}. The following documents set out the rules and disclosures governing your use of the platform. Please read them carefully before trading.`
                : `${BRAND} 平台由 ${OPERATOR} 运营。以下文件规定了您使用本平台的规则与相关披露，请在交易前仔细阅读。`}
            </p>
          </div>

          <div className="grid gap-4 sm:grid-cols-2">
            {LEGAL_DOCS.map((d) => (
              <Link
                key={d.path}
                to={d.path}
                className="group rounded-xl border border-nexa-700/70 bg-nexa-800/60 p-5 shadow-lg shadow-black/20 transition-all duration-200 hover:-translate-y-0.5 hover:border-accent/40 hover:shadow-xl hover:shadow-black/30"
              >
                <div className="mb-2 flex items-center justify-between">
                  <span className="text-base font-semibold text-nexa-100 transition-colors group-hover:text-accent">
                    {isEn ? d.en : d.zh}
                  </span>
                  <svg viewBox="0 0 24 24" fill="none" className="h-4 w-4 text-nexa-500 transition-all group-hover:translate-x-0.5 group-hover:text-accent">
                    <path d="M9 6l6 6-6 6" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
                  </svg>
                </div>
                <p className="text-xs leading-relaxed text-nexa-400">{isEn ? d.enDesc : d.zhDesc}</p>
              </Link>
            ))}
          </div>

          <div className="rounded-xl border border-nexa-700/70 bg-nexa-900/40 p-4 text-xs leading-relaxed text-nexa-500">
            <p>
              {isEn
                ? `Operator: ${OPERATOR_EN}. In the event of any discrepancy between the Chinese and English versions of these documents, the Chinese version shall prevail.`
                : `运营主体：${OPERATOR}。如各文件的中英文版本存在任何不一致之处，以中文版本为准。`}
            </p>
          </div>
        </div>
      </div>
    </Layout>
  );
}
