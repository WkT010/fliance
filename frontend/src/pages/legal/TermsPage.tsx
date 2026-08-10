import { LegalDocPage, type LegalDocContent } from './LegalDocPage';

const zh: LegalDocContent = {
  title: '服务条款',
  subtitle: '使用 Fliance（梵响）平台前请仔细阅读本条款',
  effective: '生效日期：2026年8月1日 · 版本 1.0',
  intro: [
    '本服务条款（以下简称"本条款"）由凌嘉凡响网络科技有限公司（Canival Institute Inc.，以下简称"我们"）与所有访问、注册或使用 Fliance（梵响）数字资产交易平台（以下简称"平台"）的用户（以下简称"您"）共同订立。',
    '您点击注册、登录或以任何方式使用本平台，即视为您已完整阅读、充分理解并同意接受本条款的全部内容。如您不同意本条款的任何部分，请立即停止使用本平台。',
  ],
  sections: [
    {
      heading: '条款的接受与效力',
      body: [
        '本条款构成您与我们之间关于平台使用的完整协议，取代此前的任何口头或书面约定。',
        '我们发布的《隐私政策》《风险披露》《反洗钱政策》《Cookie 政策》等文件均为本条款不可分割的组成部分，与本条款具有同等法律效力。',
      ],
    },
    {
      heading: '服务说明',
      body: [
        '平台为用户提供数字资产现货交易、永续合约交易、AMM 流动性池、钱包充提及行情数据等服务。具体服务内容和功能以平台实际提供的为准。',
        '我们有权根据法律法规、监管政策、市场状况及技术条件，自主决定新增、调整、暂停或终止部分或全部服务，并以公告形式通知用户。',
      ],
    },
    {
      heading: '用户资格',
      body: [
        '您确认并保证：您为年满 18 周岁、具有完全民事行为能力的自然人，或依法设立并有效存续的法人组织；您所在司法辖区的法律允许您使用本平台服务。',
        '您应自行了解并遵守所在地关于数字资产交易的全部法律法规。若您所在司法辖区禁止或限制使用本平台服务，您不得使用本平台。',
      ],
    },
    {
      heading: '账户注册与管理',
      body: [
        '注册时您应提供真实、准确、完整的资料，并在资料变更时及时更新。因资料不实导致的后果由您自行承担。',
        '您应妥善保管账户邮箱和密码。凡通过您账户发出的指令及达成的交易，均视为您本人的行为，您应对此承担全部责任。',
        '如发现账户被未经授权访问，请立即通知我们。在我们采取行动前已发生的交易损失，在法律允许范围内由您自行承担。',
        '未经我们书面同意，您的账户不得以任何方式转让、出借、出租或出售。',
      ],
    },
    {
      heading: '交易规则与订单执行',
      body: [
        '平台依据"价格优先、时间优先"原则对订单进行撮合。订单一经成交即不可撤销。',
        '市价订单按当前市场最优可得价格成交，实际成交价格可能与您的预期存在差异。',
        '合约交易的杠杆倍数、保证金、强平规则及资金费率以平台公示的规则为准。',
        '在极端行情、系统故障或其他不可抗力情形下，平台可能出现延迟、中断或无法成交，我们不对由此产生的损失承担责任。',
      ],
    },
    {
      heading: '费用',
      body: [
        '平台有权就交易、充提等服务收取费用，具体费率以平台公示为准。费率调整前我们将提前公告。',
        '链上转账产生的网络手续费（Gas 费）由相应区块链网络收取，与平台收取的服务费用相互独立。',
      ],
    },
    {
      heading: '禁止行为',
      body: [
        '您承诺不从事下列行为：利用平台进行洗钱、恐怖融资、欺诈或其他违法犯罪活动；操纵市场、对敲、幌骗等扰乱交易秩序的行为；未经授权访问平台系统、接口或数据；上传恶意代码或发起拒绝服务攻击；规避平台风控措施；以及其他违反法律法规或损害平台及其他用户合法权益的行为。',
        '如发现您存在上述行为，我们有权视情节轻重采取警告、限制功能、冻结账户、取消交易、上报有权机关等措施，且不承担任何责任。',
      ],
    },
    {
      heading: '知识产权',
      body: [
        'Fliance（梵响）平台的全部软件、技术、程序、界面设计、商标与标识等知识产权，均归凌嘉凡响网络科技有限公司所有，受法律保护。',
        '未经我们事先书面许可，任何人不得复制、修改、反向工程、传播或以任何商业目的使用上述内容。',
      ],
    },
    {
      heading: '免责声明与责任限制',
      body: [
        '平台按"现状"与"现有"基础提供服务，我们在法律允许的最大范围内不就服务的持续性、准确性、完整性作出任何明示或默示的保证。',
        '对于因市场行情波动、用户操作失误、账户被盗、网络故障、监管政策变化等原因造成的损失，我们在法律允许的范围内不承担赔偿责任。',
        '在任何情况下，我们对您的全部累计赔偿责任不超过您因使用平台服务而实际向我们支付的费用。',
      ],
    },
    {
      heading: '服务的暂停与终止',
      body: [
        '因系统维护、升级或紧急情况，我们可能临时暂停部分或全部服务，并将尽可能提前公告。',
        '您有权随时申请注销账户以终止服务；在满足法律法规及平台规则的前提下，我们将为您办理注销并处理账户资产。',
        '如您违反本条款或法律法规，我们有权立即中止或终止向您提供服务，并保留追究相应责任的权利。',
      ],
    },
    {
      heading: '条款的变更',
      body: [
        '我们有权根据法律法规、监管要求或业务需要不时修订本条款。修订后的条款将通过平台公告发布，重大变更将单独提示。',
        '修订生效后您继续使用平台，即视为接受修订后的条款；若您不同意修订内容，应停止使用平台并妥善处理账户资产。',
      ],
    },
    {
      heading: '法律适用与争议解决',
      body: [
        '本条款的订立、履行、解释及争议解决均适用中华人民共和国法律（为本条款之目的，不含港澳台地区法律）。',
        '因本条款引起的或与之相关的争议，双方应友好协商解决；协商不成的，任何一方可向凌嘉凡响网络科技有限公司所在地有管辖权的人民法院提起诉讼。',
      ],
    },
    {
      heading: '其他条款',
      body: [
        '本条款任何条款被认定无效或不可执行的，不影响其他条款的效力。',
        '我们未及时行使或执行本条款项下的任何权利，不构成对该权利的放弃。',
      ],
    },
  ],
};

const en: LegalDocContent = {
  title: 'Terms of Service',
  subtitle: 'Please read these terms carefully before using Fliance',
  effective: 'Effective Date: August 1, 2026 · Version 1.0',
  intro: [
    'These Terms of Service ("Terms") are entered into between Canival Institute Inc. ("we", "us") and every user ("you") who accesses, registers with, or uses the Fliance digital asset trading platform ("the Platform").',
    'By registering, logging in, or otherwise using the Platform, you are deemed to have read, understood and accepted these Terms in full. If you do not agree with any part of these Terms, please stop using the Platform immediately.',
  ],
  sections: [
    {
      heading: 'Acceptance & Effect',
      body: [
        'These Terms constitute the entire agreement between you and us regarding use of the Platform, superseding any prior oral or written arrangements.',
        'The Privacy Policy, Risk Disclosure, AML Policy and Cookie Policy we publish are integral parts of these Terms and carry equal effect.',
      ],
    },
    {
      heading: 'Description of Services',
      body: [
        'The Platform provides digital asset spot trading, perpetual futures trading, AMM liquidity pools, wallet deposits/withdrawals and market data. The services actually available on the Platform prevail.',
        'We may, at our discretion and in accordance with laws, regulations, market conditions and technical circumstances, add, adjust, suspend or terminate some or all services, with notice given by announcement.',
      ],
    },
    {
      heading: 'Eligibility',
      body: [
        'You represent and warrant that you are a natural person aged 18 or above with full civil capacity, or a duly established legal entity, and that the laws of your jurisdiction permit you to use the Platform.',
        'You are solely responsible for understanding and complying with all laws and regulations on digital asset trading in your jurisdiction. If your jurisdiction prohibits or restricts the use of the Platform, you must not use it.',
      ],
    },
    {
      heading: 'Account Registration & Management',
      body: [
        'You must provide true, accurate and complete information at registration and keep it up to date. You bear the consequences of providing false information.',
        'You must keep your email and password confidential. All instructions and trades made through your account are deemed your own acts, and you are fully responsible for them.',
        'If you discover unauthorised access to your account, notify us immediately. Losses from trades executed before we act are borne by you to the extent permitted by law.',
        'Your account may not be transferred, lent, rented or sold in any way without our prior written consent.',
      ],
    },
    {
      heading: 'Trading Rules & Order Execution',
      body: [
        'Orders are matched on a price-priority, then time-priority basis. Once filled, an order cannot be revoked.',
        'Market orders execute at the best available price; actual fill prices may differ from your expectation.',
        'Leverage tiers, margin requirements, liquidation rules and funding rates for futures follow the rules published on the Platform.',
        'Under extreme market conditions, system failures or force majeure, the Platform may experience delays, interruptions or failed execution; we are not liable for resulting losses.',
      ],
    },
    {
      heading: 'Fees',
      body: [
        'We may charge fees for trading, deposits and withdrawals as published on the Platform. Changes to fee schedules will be announced in advance.',
        'On-chain network fees (gas) are levied by the respective blockchain networks and are separate from the service fees charged by the Platform.',
      ],
    },
    {
      heading: 'Prohibited Conduct',
      body: [
        'You undertake not to: use the Platform for money laundering, terrorist financing, fraud or any other unlawful activity; engage in market manipulation, wash trading or spoofing; access Platform systems, APIs or data without authorisation; upload malicious code or launch denial-of-service attacks; circumvent risk controls; or otherwise violate laws or harm the lawful interests of the Platform or other users.',
        'If any such conduct is detected, we may, depending on severity, issue warnings, restrict features, freeze accounts, cancel trades or report to competent authorities, without any liability.',
      ],
    },
    {
      heading: 'Intellectual Property',
      body: [
        'All software, technology, code, interface designs, trademarks and logos of Fliance are owned by Canival Institute Inc. and protected by law.',
        'Without our prior written permission, no one may copy, modify, reverse engineer, distribute or use such content for any commercial purpose.',
      ],
    },
    {
      heading: 'Disclaimers & Limitation of Liability',
      body: [
        'The Platform is provided on an "as is" and "as available" basis. To the maximum extent permitted by law, we make no express or implied warranties regarding continuity, accuracy or completeness of the services.',
        'We are not liable, to the extent permitted by law, for losses arising from market volatility, user errors, account theft, network failures or regulatory changes.',
        'In no event shall our aggregate liability exceed the fees you actually paid to us for using the Platform.',
      ],
    },
    {
      heading: 'Suspension & Termination',
      body: [
        'For maintenance, upgrades or emergencies, we may temporarily suspend some or all services and will announce where possible.',
        'You may request account closure at any time; subject to laws and Platform rules, we will process the closure and remaining assets.',
        'If you breach these Terms or applicable laws, we may immediately suspend or terminate services and reserve the right to pursue remedies.',
      ],
    },
    {
      heading: 'Changes to These Terms',
      body: [
        'We may revise these Terms from time to time in light of laws, regulations or business needs. Revised Terms will be published by announcement; material changes will be highlighted.',
        'Your continued use of the Platform after a revision takes effect constitutes acceptance. If you disagree, stop using the Platform and settle your account assets.',
      ],
    },
    {
      heading: 'Governing Law & Dispute Resolution',
      body: [
        'These Terms are governed by the laws of the People\'s Republic of China (excluding, for this purpose, the laws of Hong Kong SAR, Macao SAR and the Taiwan region).',
        'Disputes arising from these Terms shall first be resolved through friendly negotiation; failing that, either party may file suit before the competent People\'s Court at the location of Canival Institute Inc.',
      ],
    },
    {
      heading: 'Miscellaneous',
      body: [
        'If any provision of these Terms is held invalid or unenforceable, the remaining provisions continue in full force.',
        'Our failure to promptly exercise any right under these Terms does not constitute a waiver of that right.',
      ],
    },
  ],
};

export function TermsPage() {
  return <LegalDocPage zh={zh} en={en} />;
}
