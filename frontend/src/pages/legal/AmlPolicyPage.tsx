import { LegalDocPage, type LegalDocContent } from './LegalDocPage';

const zh: LegalDocContent = {
  title: '反洗钱政策',
  subtitle: 'Fliance（梵响）反洗钱（AML）与反恐怖融资（CFT）合规政策',
  effective: '生效日期：2026年8月1日 · 版本 1.0',
  intro: [
    '凌嘉凡响网络科技有限公司（Canival Institute Inc.，以下简称"我们"）严格遵守适用的反洗钱与反恐怖融资法律法规，对洗钱、恐怖融资及其他非法金融活动采取零容忍态度。',
    '本政策说明 Fliance（梵响）平台为防范非法资金流动所采取的身份核验、交易监控与报告措施。使用本平台即表示您同意配合本政策项下的全部合规要求。',
  ],
  sections: [
    {
      heading: '政策声明',
      body: [
        '我们承诺建立并持续维护有效的反洗钱合规体系，覆盖用户准入、交易监控、可疑行为报告与记录保存全流程。',
        '本平台严禁被用于洗钱、恐怖融资、诈骗、逃税、非法资金转移或其他任何形式的违法犯罪活动。一经发现，我们将立即采取限制措施并依法报告。',
      ],
    },
    {
      heading: 'KYC 身份核验',
      body: [
        '依据风险状况与法律要求，我们可能要求用户提供身份核验（KYC）资料，包括但不限于：真实姓名、有效身份证件、证件照片、住址证明及人脸核验信息。',
        '未完成相应等级身份核验的账户，其功能（如提现额度）可能受到限制。',
        '您保证所提供的一切 KYC 资料真实、准确、有效，不得使用伪造、变造或他人资料。',
      ],
    },
    {
      heading: '持续尽职调查',
      body: [
        'KYC 并非一次性流程。我们可能对用户资料进行定期复核与更新，并在账户行为出现重大变化时触发重新核验。',
        '当用户资料过期、存疑或与交易行为明显不符时，我们可能要求补充证明材料。',
      ],
    },
    {
      heading: '交易监控',
      body: [
        '我们部署自动化风控与交易监控系统，对充值、提现、交易等行为进行实时与离线监测，识别包括但不限于下列异常模式：频繁小额拆分转账、与交易习惯明显不符的大额资金流动、高频对敲、快进快出、与被标记地址的关联往来。',
        '监控系统会根据风险信号对账户进行分级，并触发相应的人工复核流程。',
      ],
    },
    {
      heading: '可疑交易报告',
      body: [
        '当发现涉嫌洗钱、恐怖融资或其他违法活动的交易时，我们将依照适用法律向有权主管机关提交可疑交易报告或进行其他必要通报。',
        '依照法律要求，我们可能在报告时不另行通知用户（"禁止通风报信"原则）。',
      ],
    },
    {
      heading: '制裁名单与受限地区',
      body: [
        '我们不向位于受国际制裁国家或地区的用户提供服务，也不与任何制裁名单所列的个人或实体开展业务。',
        '如核实用户属于受限主体，我们将按规定冻结或限制相关账户并依法处理。',
      ],
    },
    {
      heading: '记录保存',
      body: [
        '我们将依照法律要求保存用户身份资料与交易记录，保存期限不少于适用法律规定的最低年限（通常不少于五年）。',
        '相关记录仅在依法配合监管、司法调查或内部审计时使用。',
      ],
    },
    {
      heading: '用户配合义务',
      body: [
        '您应配合我们依法依规提出的核验与调查要求，如实回答问题并在要求期限内提供材料。',
        '拒绝配合、提供虚假材料或试图规避风控措施，将导致账户功能受限、暂停或终止服务。',
      ],
    },
    {
      heading: '账户限制措施',
      body: [
        '对于触发风险规则的账户，我们有权视风险程度采取下列一项或多项措施：延迟处理充提、限制提现额度、暂停交易、要求补充核验、冻结账户内资产，以及向有权机关报告。',
        '上述措施旨在防范非法资金流动，我们因依法采取限制措施造成的损失在法律允许范围内不承担责任。',
      ],
    },
    {
      heading: '培训与内部控制',
      body: [
        '我们为合规与风控岗位人员提供定期反洗钱培训，并建立内部审计机制，定期评估合规体系的有效性并持续改进。',
      ],
    },
    {
      heading: '政策更新',
      body: [
        '我们可能根据法律法规变化或监管要求修订本政策，修订后的政策将通过平台公告发布并自公布之日起生效。',
      ],
    },
  ],
};

const en: LegalDocContent = {
  title: 'AML Policy',
  subtitle: 'Anti-money-laundering and counter-terrorism-financing policy of Fliance',
  effective: 'Effective Date: August 1, 2026 · Version 1.0',
  intro: [
    'Canival Institute Inc. ("we", "us") strictly complies with applicable anti-money-laundering (AML) and counter-terrorism-financing (CFT) laws and maintains zero tolerance for money laundering, terrorist financing and other illicit financial activity.',
    'This policy describes the identity verification, transaction monitoring and reporting measures Fliance takes to prevent illicit fund flows. By using the Platform you agree to cooperate with all compliance requirements hereunder.',
  ],
  sections: [
    {
      heading: 'Policy Statement',
      body: [
        'We are committed to building and maintaining an effective AML compliance programme covering user onboarding, transaction monitoring, suspicious-activity reporting and record-keeping.',
        'The Platform must never be used for money laundering, terrorist financing, fraud, tax evasion, illicit fund transfers or any other unlawful activity. Violations trigger immediate restrictions and lawful reporting.',
      ],
    },
    {
      heading: 'KYC Identity Verification',
      body: [
        'Depending on risk levels and legal requirements, we may ask users to provide Know-Your-Customer (KYC) materials, including but not limited to: real name, valid identity documents, document images, proof of address and facial verification.',
        'Accounts that have not completed the corresponding verification tier may face functional limits, such as withdrawal caps.',
        'You warrant that all KYC materials are true, accurate and valid; forged, altered or third-party materials are prohibited.',
      ],
    },
    {
      heading: 'Ongoing Due Diligence',
      body: [
        'KYC is not a one-time process. We may periodically review and refresh user information, and re-verification may be triggered by significant changes in account behaviour.',
        'When user information is outdated, questionable or inconsistent with trading behaviour, we may request supplementary documents.',
      ],
    },
    {
      heading: 'Transaction Monitoring',
      body: [
        'We operate automated risk-control and monitoring systems that watch deposits, withdrawals and trades in real time and offline, detecting patterns such as: frequent small-amount structuring, large fund flows inconsistent with trading history, high-frequency wash trading, rapid in-and-out movements, and links to flagged addresses.',
        'Accounts are risk-tiered based on monitoring signals, with corresponding manual review workflows.',
      ],
    },
    {
      heading: 'Suspicious Activity Reporting',
      body: [
        'When a transaction is suspected of money laundering, terrorist financing or other unlawful activity, we will file suspicious-activity reports or make other required notifications to competent authorities under applicable law.',
        'As required by law, we may file such reports without notifying the user ("no tipping-off").',
      ],
    },
    {
      heading: 'Sanctions & Restricted Regions',
      body: [
        'We do not provide services to users located in internationally sanctioned countries or regions, nor do we deal with individuals or entities on any sanctions list.',
        'If a user is confirmed to be a restricted party, the relevant accounts will be frozen or restricted and handled according to law.',
      ],
    },
    {
      heading: 'Record Retention',
      body: [
        'We retain identity information and transaction records for at least the minimum period required by applicable law (typically no less than five years).',
        'Such records are used only for regulatory or judicial cooperation and internal audits.',
      ],
    },
    {
      heading: 'User Cooperation',
      body: [
        'You must cooperate with verification and investigation requests made in accordance with law, answer questions truthfully and provide materials within the requested timeframe.',
        'Refusal to cooperate, provision of false materials or attempts to bypass risk controls will result in functional limits, suspension or termination of services.',
      ],
    },
    {
      heading: 'Account Restrictions',
      body: [
        'For accounts that trigger risk rules, we may, depending on severity: delay deposits/withdrawals, cap withdrawal amounts, suspend trading, request additional verification, freeze assets, and report to competent authorities.',
        'These measures aim to prevent illicit fund flows; to the extent permitted by law, we are not liable for losses caused by restrictions taken in good faith.',
      ],
    },
    {
      heading: 'Training & Internal Controls',
      body: [
        'We provide regular AML training to compliance and risk staff, and run internal audits to assess and continuously improve the effectiveness of our compliance programme.',
      ],
    },
    {
      heading: 'Policy Updates',
      body: [
        'We may revise this policy in response to legal or regulatory changes. Revised versions will be published by announcement and take effect upon publication.',
      ],
    },
  ],
};

export function AmlPolicyPage() {
  return <LegalDocPage zh={zh} en={en} />;
}
