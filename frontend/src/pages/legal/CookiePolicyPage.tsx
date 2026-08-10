import { LegalDocPage, type LegalDocContent } from './LegalDocPage';

const zh: LegalDocContent = {
  title: 'Cookie 政策',
  subtitle: 'Fliance（梵响）如何使用 Cookie 与浏览器本地存储',
  effective: '生效日期：2026年8月1日 · 版本 1.0',
  intro: [
    '凌嘉凡响网络科技有限公司（Canival Institute Inc.，以下简称"我们"）在 Fliance（梵响）平台中使用 Cookie 及浏览器本地存储（localStorage / sessionStorage）等同类技术，以保障服务的正常运行并提升使用体验。',
    '本政策说明我们使用的存储技术类型、具体用途以及您可以采取的管理方式。继续使用本平台即表示您同意本政策所述的存储使用方式。',
  ],
  sections: [
    {
      heading: '什么是 Cookie',
      body: [
        'Cookie 是网站在您访问时存储于浏览器中的小型文本文件，用于记住您的状态与偏好，使网站能够在后续访问中识别您的浏览器。',
        '同类技术还包括 localStorage 与 sessionStorage，它们同样保存在您的浏览器中，但容量更大且不会随每个请求发送到服务器。',
      ],
    },
    {
      heading: '我们使用的存储类型',
      body: [
        '必要性存储（必需）：保障平台核心功能运行，无法关闭。包括登录会话令牌（sessionStorage，键名如 fliance-auth）与界面语言偏好（localStorage，键名如 fliance-lang）。',
        '功能性存储（必需）：保存界面交互状态，例如页面内的临时表单输入与视图偏好，用于提升使用流畅度。',
        '分析性存储（有限）：我们目前不部署第三方广告追踪类 Cookie；如未来引入用于产品改进的匿名化统计，将提前更新本政策并在必要时征求您的同意。',
      ],
    },
    {
      heading: '具体用途',
      body: [
        '维持登录会话：在您登录后于会话期间保持身份状态，关闭浏览器后会话自动失效。',
        '记住语言偏好：保存您选择的界面语言（中文/英文），下次访问时自动应用。',
        '安全防护：辅助识别异常会话行为，配合风控系统保障账户安全。',
      ],
    },
    {
      heading: '第三方 Cookie',
      body: [
        '平台加载的字体等第三方资源可能附带其自身的 Cookie 或缓存策略，我们对此无控制权，建议您参阅相应第三方的隐私政策。',
        '我们不与广告联盟或数据经纪商共享您的浏览行为数据。',
      ],
    },
    {
      heading: '如何管理 Cookie',
      body: [
        '大多数浏览器允许您在设置中查看、删除或拒绝 Cookie：您可在浏览器"设置 — 隐私与安全"中清除浏览数据，或配置拒绝第三方 Cookie。',
        'localStorage 与 sessionStorage 可通过浏览器开发者工具（Application/存储面板）查看与清除；清除 fliance-auth 与 fliance-lang 键将分别重置您的登录会话与语言偏好。',
      ],
    },
    {
      heading: '禁用存储的影响',
      body: [
        '必要性存储是平台登录与交易功能的基础。若将其禁用或删除，您可能无法登录、保持会话或正常使用下单等功能。',
        '删除语言偏好存储后，界面语言将恢复为默认语言（中文）。',
      ],
    },
    {
      heading: '数据保存期限',
      body: [
        '会话类存储（sessionStorage）在浏览器会话结束时自动清除；本地偏好存储（localStorage）将一直保留直至您手动删除。',
        '我们不会通过上述存储收集超出服务必要范围的个人信息。',
      ],
    },
    {
      heading: '政策更新',
      body: [
        '我们可能根据技术演进或法律要求修订本政策。重大变更将以平台公告形式通知您，修订后的政策自公布之日起生效。',
      ],
    },
    {
      heading: '联系我们',
      body: [
        '如您对本政策或 Cookie 的使用有任何疑问，请联系凌嘉凡响网络科技有限公司，我们将在合理期限内予以答复。',
      ],
    },
  ],
};

const en: LegalDocContent = {
  title: 'Cookie Policy',
  subtitle: 'How Fliance uses cookies and browser storage',
  effective: 'Effective Date: August 1, 2026 · Version 1.0',
  intro: [
    'Canival Institute Inc. ("we", "us") uses cookies and similar browser storage technologies (localStorage / sessionStorage) on the Fliance platform to keep the service running properly and to improve your experience.',
    'This policy explains the types of storage we use, their purposes, and how you can manage them. By continuing to use the Platform you consent to the storage practices described here.',
  ],
  sections: [
    {
      heading: 'What Are Cookies',
      body: [
        'Cookies are small text files stored by your browser when you visit a website; they remember your state and preferences so the site can recognise your browser on later visits.',
        'Similar technologies include localStorage and sessionStorage, which also live in your browser but hold more data and are not sent to the server with every request.',
      ],
    },
    {
      heading: 'Types of Storage We Use',
      body: [
        'Strictly necessary storage: required for core functionality and cannot be disabled. Includes the login session token (sessionStorage, key such as fliance-auth) and the interface language preference (localStorage, key such as fliance-lang).',
        'Functional storage (required): saves interface state such as temporary form input and view preferences to keep the experience smooth.',
        'Analytics storage (limited): we currently deploy no third-party advertising or tracking cookies. If we introduce anonymised product analytics in the future, this policy will be updated in advance and consent will be sought where required.',
      ],
    },
    {
      heading: 'Specific Purposes',
      body: [
        'Maintaining your login session: keeps you authenticated during the session; the session expires automatically when the browser closes.',
        'Remembering your language: stores the interface language you choose (Chinese/English) and applies it on your next visit.',
        'Security: helps identify abnormal session behaviour and works with our risk controls to protect your account.',
      ],
    },
    {
      heading: 'Third-Party Cookies',
      body: [
        'Third-party resources loaded by the Platform (such as fonts) may set their own cookies or caching policies; we have no control over them and suggest you review the relevant third-party privacy policies.',
        'We do not share your browsing behaviour with ad networks or data brokers.',
      ],
    },
    {
      heading: 'Managing Cookies',
      body: [
        'Most browsers let you view, delete or block cookies under Settings — Privacy & Security; you can clear browsing data or configure the browser to reject third-party cookies.',
        'localStorage and sessionStorage can be inspected and cleared via the browser developer tools (Application/Storage panel). Clearing the fliance-auth and fliance-lang keys resets your login session and language preference respectively.',
      ],
    },
    {
      heading: 'Effects of Disabling Storage',
      body: [
        'Strictly necessary storage underpins login and trading. If you disable or delete it, you may be unable to sign in, keep a session, or place orders.',
        'Deleting the language preference resets the interface language to the default (Chinese).',
      ],
    },
    {
      heading: 'Retention',
      body: [
        'Session storage (sessionStorage) is cleared automatically when the browser session ends; local preferences (localStorage) persist until you delete them manually.',
        'We never collect personal information beyond what the service strictly requires through these mechanisms.',
      ],
    },
    {
      heading: 'Policy Updates',
      body: [
        'We may revise this policy as technology or legal requirements evolve. Material changes will be announced on the Platform and take effect upon publication.',
      ],
    },
    {
      heading: 'Contact Us',
      body: [
        'If you have any questions about this policy or our use of cookies, please contact Canival Institute Inc. We will respond within a reasonable period.',
      ],
    },
  ],
};

export function CookiePolicyPage() {
  return <LegalDocPage zh={zh} en={en} />;
}
