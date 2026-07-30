import { Layout } from '@/components/Layout';
import { Card } from '@/components/common/Card';

export function LegalPage() {
  return (
    <Layout>
      <div className="h-full overflow-y-auto p-4">
        <div className="mx-auto max-w-4xl space-y-6 pb-8">
          <div className="space-y-1">
            <h1 className="text-2xl font-bold text-nexa-100">Legal & Compliance</h1>
            <p className="text-sm text-nexa-400">
              Version 1.0 — Effective Date: 2026-07-28
            </p>
          </div>

          <Card title="License Summary">
            <div className="space-y-3 p-4 text-sm leading-relaxed text-nexa-300">
              <p>
                <strong className="text-nexa-100">Nexa Exchange</strong> is a proprietary
                cryptocurrency exchange platform developed and owned by{' '}
                <strong className="text-nexa-100">Canival Studios</strong>. All rights are reserved.
              </p>
              <ul className="list-disc space-y-1 pl-5">
                <li>
                  This project is licensed under the{' '}
                  <strong className="text-nexa-100">Canival Studios Proprietary License Agreement</strong>.
                </li>
                <li>
                  No license, express or implied, is granted merely by accessing, viewing, cloning,
                  downloading, or possessing the Software.
                </li>
                <li>
                  Commercial licensing is available for organizations that wish to use, modify, deploy,
                  or distribute the Software.
                </li>
                <li>
                  Current one-time source code license fee:{' '}
                  <strong className="text-nexa-100">USD $2,999.00</strong>.
                </li>
                <li>
                  To request a commercial license, email{' '}
                  <a
                    href="mailto:canival.b2b@hotmail.com"
                    className="text-accent hover:underline"
                  >
                    canival.b2b@hotmail.com
                  </a>{' '}
                  with the subject line &quot;Nexa Exchange Source Code License Request&quot;.
                </li>
              </ul>
            </div>
          </Card>

          <Card title="Terms of Service">
            <div className="space-y-3 p-4 text-sm leading-relaxed text-nexa-300">
              <p>
                These Terms of Service apply to any individual or entity that accesses, uses, or
                interacts with Nexa Exchange.
              </p>
              <ul className="list-disc space-y-1 pl-5">
                <li>
                  You must be of legal age and comply with all applicable laws in your jurisdiction.
                </li>
                <li>
                  You agree not to reverse engineer, decompile, or attempt to extract source code
                  except as expressly permitted in writing by Canival Studios.
                </li>
                <li>
                  Canival Studios may suspend or terminate access for violations of these terms or
                  applicable law.
                </li>
                <li>
                  The platform is provided &quot;as is&quot; without warranties of any kind, either express
                  or implied.
                </li>
              </ul>
            </div>
          </Card>

          <Card title="Privacy Policy">
            <div className="space-y-3 p-4 text-sm leading-relaxed text-nexa-300">
              <p>
                Canival Studios respects your privacy and is committed to protecting personal data
                processed through Nexa Exchange.
              </p>
              <ul className="list-disc space-y-1 pl-5">
                <li>
                  We collect only the information necessary to provide, secure, and improve the
                  platform.
                </li>
                <li>
                  Personal data is stored securely and is not sold or shared with third parties
                  except as required by law.
                </li>
                <li>
                  Users may request access, correction, or deletion of their personal data by
                  contacting us.
                </li>
              </ul>
            </div>
          </Card>

          <Card title="Risk Disclosure">
            <div className="space-y-3 p-4 text-sm leading-relaxed text-nexa-300">
              <p>
                Cryptocurrency trading involves substantial risk. You should carefully consider
                whether trading is suitable for you in light of your experience, objectives, and
                financial resources.
              </p>
              <ul className="list-disc space-y-1 pl-5">
                <li>
                  Digital assets are highly volatile and may result in significant losses.
                </li>
                <li>
                  Past performance is not indicative of future results.
                </li>
                <li>
                  Canival Studios is not responsible for losses arising from market conditions,
                  user error, or unauthorized account access.
                </li>
              </ul>
            </div>
          </Card>

          <Card title="Intellectual Property">
            <div className="space-y-3 p-4 text-sm leading-relaxed text-nexa-300">
              <p>
                All intellectual property rights in and to Nexa Exchange, including but not limited
                to source code, object code, designs, trademarks, logos, documentation, and trading
                logic, are owned exclusively by Canival Studios.
              </p>
              <ul className="list-disc space-y-1 pl-5">
                <li>
                  <strong className="text-nexa-100">Nexa Exchange™</strong> and{' '}
                  <strong className="text-nexa-100">NEXA™</strong> are trademarks of Canival Studios.
                </li>
                <li>
                  Unauthorized use, reproduction, or distribution of any materials is strictly
                  prohibited.
                </li>
              </ul>
            </div>
          </Card>

          <Card title="Contact">
            <div className="space-y-3 p-4 text-sm leading-relaxed text-nexa-300">
              <p>
                For licensing, legal, or compliance inquiries, please contact:
              </p>
              <div className="rounded border border-nexa-700 bg-nexa-900/50 p-3">
                <div className="text-nexa-100">Canival Studios</div>
                <div>
                  Email:{" "}
                  <a
                    href="mailto:canival.b2b@hotmail.com"
                    className="text-accent hover:underline"
                  >
                    canival.b2b@hotmail.com
                  </a>
                </div>
                <div className="text-nexa-400">
                  Subject: &quot;Nexa Exchange Source Code License Request&quot;
                </div>
              </div>
            </div>
          </Card>
        </div>
      </div>
    </Layout>
  );
}
