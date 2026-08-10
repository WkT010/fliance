import { LegalDocPage, type LegalDocContent } from './LegalDocPage';

const zh: LegalDocContent = {
  title: '隐私政策',
  subtitle: 'Fliance（梵响）如何收集、使用、存储与保护您的个人信息',
  effective: '生效日期：2026年8月1日 · 版本 1.0',
  intro: [
    '凌嘉凡响网络科技有限公司（Canival Institute Inc.，以下简称"我们"）作为 Fliance（梵响）数字资产交易平台的运营主体，深知个人信息对您的重要性，并将严格遵守相关法律法规，审慎处理您的个人信息。',
    '本政策适用于您通过网页端访问和使用 Fliance（梵响）平台的全部场景。请您在使用我们的服务前完整阅读并充分理解本政策，您开始使用我们的服务即表示同意本政策的全部内容。',
  ],
  sections: [
    {
      heading: '适用范围',
      body: [
        '本政策适用于 Fliance（梵响）平台提供的全部服务，包括但不限于账户注册、现货与合约交易、AMM 流动性服务、钱包充值与提现、账户设置及客户服务。',
        '若本政策与单独签署的协议或特定服务的专项说明存在冲突，以该等协议或专项说明为准。',
      ],
    },
    {
      heading: '我们收集的信息',
      body: [
        '注册与身份信息：注册邮箱、密码（加密存储）、实名认证材料（如依法需要开展身份核验时提供的证件信息）。',
        '交易与资产信息：订单记录、成交记录、充提币记录、余额与持仓数据。',
        '设备与日志信息：IP 地址、浏览器类型、操作系统、访问时间、页面浏览记录及会话信息，用于安全防护与故障排查。',
        '沟通信息：您与客服联系时提供的邮件内容及相关凭证。',
        '我们仅收集实现服务目的所必需的最少信息，不会收集与服务无关的个人信息。',
      ],
    },
    {
      heading: '信息的使用方式',
      body: [
        '为您提供核心服务：完成账户注册、订单撮合、资产结算、行情展示与对账。',
        '安全与合规：身份核验、反洗钱审查、风险监测、异常行为识别与审计留痕。',
        '服务改进：分析平台运行情况，优化产品功能与用户体验。',
        '消息通知：发送订单成交、充提到账、安全提醒等与服务直接相关的通知。',
        '未经您的单独同意，我们不会将您的个人信息用于营销推送或本政策未载明的其他用途。',
      ],
    },
    {
      heading: 'Cookie 与同类技术',
      body: [
        '我们使用 Cookie 及浏览器本地存储（localStorage / sessionStorage）来保存您的登录会话与语言偏好，以保障服务的基本可用性。',
        '关于 Cookie 的具体类型与管理方式，请参阅我们的《Cookie 政策》。',
      ],
    },
    {
      heading: '第三方共享与披露',
      body: [
        '我们不会向任何第三方出售您的个人信息。仅在以下必要情形下，我们可能依法共享或披露您的部分信息：',
        '（一）为遵守法律法规、监管要求或司法机关、行政机关的强制性命令；（二）为履行反洗钱、反恐怖融资等法定义务向有权机构报送；（三）为完成链上结算而必须向相应区块链网络广播的交易数据；（四）经您明确授权同意的其他情形。',
        '除上述情形外，我们不会与任何公司、组织或个人共享您的个人信息。',
      ],
    },
    {
      heading: '信息存储与保护',
      body: [
        '我们采用行业标准的安全措施保护您的个人信息，包括但不限于传输层加密（HTTPS/TLS）、密码哈希存储、访问权限最小化、操作审计日志与 7×24 小时安全监控。',
        '您的访问令牌仅保存在浏览器会话存储中，会话结束即失效；我们不会在日志中记录任何凭证明文。',
        '尽管我们已尽最大努力，请理解互联网环境并非百分之百安全。如您发现账户存在异常，请立即修改密码并联系客服。',
      ],
    },
    {
      heading: '信息保存期限',
      body: [
        '我们仅在实现服务目的所必需的最短期限内保存您的个人信息。账户注销后，除法律法规要求留存的交易与审计记录外，我们将删除或匿名化处理您的个人信息。',
        '因反洗钱及金融监管要求，部分交易记录可能依法保存不少于五年。',
      ],
    },
    {
      heading: '您的权利',
      body: [
        '访问与复制：您可在"账户"与"钱包"页面随时查看您的个人资料、持仓与交易记录。',
        '更正与补充：发现信息有误时，您可通过客服申请更正。',
        '删除与注销：您有权申请删除个人信息或注销账户，我们将在核实身份后的合理期限内完成处理。',
        '撤回同意：您可通过停止使用服务或注销账户的方式撤回对本政策的同意，但不影响撤回前已进行处理的合法性。',
      ],
    },
    {
      heading: '未成年人保护',
      body: [
        '我们的服务仅面向年满 18 周岁且具有完全民事行为能力的自然人。我们不会主动收集未成年人的个人信息，如发现误收集情形，将立即删除。',
      ],
    },
    {
      heading: '政策的更新',
      body: [
        '我们可能根据法律法规变化或业务调整适时修订本政策。重大变更将以平台公告或站内通知形式告知您，修订后的政策自公布之日起生效。',
        '变更生效后您继续使用我们的服务，即视为接受修订后的政策。',
      ],
    },
    {
      heading: '联系我们',
      body: [
        '如您对本政策或您的个人信息处理有任何疑问、意见或投诉，请联系凌嘉凡响网络科技有限公司，我们将于收到请求后的合理期限内予以答复。',
      ],
    },
  ],
};

const en: LegalDocContent = {
  title: 'Privacy Policy',
  subtitle: 'How Fliance collects, uses, stores and protects your personal data',
  effective: 'Effective Date: August 1, 2026 · Version 1.0',
  intro: [
    'Canival Institute Inc. ("we", "us"), the operator of the Fliance digital asset trading platform, recognises the importance of your personal data and processes it strictly in accordance with applicable laws and regulations.',
    'This policy applies to all scenarios in which you access and use Fliance through the web interface. Please read it carefully before using our services; by using our services you agree to this policy in full.',
  ],
  sections: [
    {
      heading: 'Scope',
      body: [
        'This policy covers every service provided by Fliance, including account registration, spot and futures trading, AMM liquidity services, wallet deposits and withdrawals, account settings and customer support.',
        'Where this policy conflicts with a separately signed agreement or a service-specific notice, that agreement or notice prevails.',
      ],
    },
    {
      heading: 'Information We Collect',
      body: [
        'Registration & identity: your email address, password (stored in encrypted/hashed form) and, where identity verification is legally required, the corresponding verification materials.',
        'Trading & asset data: order history, fill records, deposit/withdrawal records, balances and positions.',
        'Device & log data: IP address, browser type, operating system, access times, page views and session information, used for security and troubleshooting.',
        'Communications: emails and supporting materials you share with our support team.',
        'We collect only the minimum information necessary to deliver the service and never collect data unrelated to it.',
      ],
    },
    {
      heading: 'How We Use Your Information',
      body: [
        'Core services: account registration, order matching, asset settlement, market data display and reconciliation.',
        'Security & compliance: identity verification, anti-money-laundering review, risk monitoring, anomaly detection and audit logging.',
        'Service improvement: analysing platform operations to optimise features and user experience.',
        'Notifications: order fills, deposit/withdrawal confirmations and security alerts directly related to the service.',
        'Without your separate consent, we will never use your personal data for marketing or any purpose not stated in this policy.',
      ],
    },
    {
      heading: 'Cookies & Similar Technologies',
      body: [
        'We use cookies and browser storage (localStorage / sessionStorage) to maintain your login session and language preference, which is essential for basic service availability.',
        'For cookie types and management options, please refer to our Cookie Policy.',
      ],
    },
    {
      heading: 'Sharing & Disclosure to Third Parties',
      body: [
        'We do not sell your personal data to any third party. We may share or disclose limited information only where necessary to:',
        '(1) comply with laws, regulations or mandatory orders of judicial or administrative authorities; (2) fulfil anti-money-laundering and counter-terrorism-financing obligations towards competent authorities; (3) broadcast transaction data to the relevant blockchain network for on-chain settlement; (4) any other scenario with your explicit consent.',
        'Outside these scenarios, we share your personal data with no company, organisation or individual.',
      ],
    },
    {
      heading: 'Storage & Protection',
      body: [
        'We protect your personal data with industry-standard measures, including transport encryption (HTTPS/TLS), hashed password storage, least-privilege access control, audit logging and 24/7 security monitoring.',
        'Your access tokens are kept only in browser session storage and expire with the session; credentials are never written to logs in plain text.',
        'No internet environment is completely secure. If you notice anything abnormal about your account, change your password immediately and contact support.',
      ],
    },
    {
      heading: 'Retention Period',
      body: [
        'We retain your personal data only for as long as necessary to fulfil the purposes described herein. After account closure, we delete or anonymise your personal data except for trading and audit records that must be retained by law.',
        'Certain transaction records may be retained for no less than five years to satisfy AML and financial regulatory requirements.',
      ],
    },
    {
      heading: 'Your Rights',
      body: [
        'Access & copy: view your profile, positions and trading history at any time from the Account and Wallet pages.',
        'Correction: request corrections to inaccurate information via support.',
        'Deletion & closure: you may request deletion of your personal data or closure of your account; we will process it within a reasonable period after verifying your identity.',
        'Withdrawal of consent: you may withdraw consent by ceasing to use the service or closing your account, without affecting the lawfulness of prior processing.',
      ],
    },
    {
      heading: 'Protection of Minors',
      body: [
        'Our services are directed solely at natural persons aged 18 or above with full civil capacity. We do not knowingly collect personal data from minors and will promptly delete any such data if discovered.',
      ],
    },
    {
      heading: 'Changes to This Policy',
      body: [
        'We may revise this policy from time to time in response to legal changes or business adjustments. Material changes will be announced on the platform, and the revised policy takes effect upon publication.',
        'Your continued use of our services after a change constitutes acceptance of the revised policy.',
      ],
    },
    {
      heading: 'Contact Us',
      body: [
        'If you have any questions, comments or complaints about this policy or the handling of your personal data, please contact Canival Institute Inc. We will respond within a reasonable period.',
      ],
    },
  ],
};

export function PrivacyPolicyPage() {
  return <LegalDocPage zh={zh} en={en} />;
}
