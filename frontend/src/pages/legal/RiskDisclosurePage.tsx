import { LegalDocPage, type LegalDocContent } from './LegalDocPage';

const zh: LegalDocContent = {
  title: '风险披露',
  subtitle: '数字资产交易存在重大风险，请务必在充分了解后审慎决策',
  effective: '生效日期：2026年8月1日 · 版本 1.0',
  intro: [
    '凌嘉凡响网络科技有限公司（Canival Institute Inc.，以下简称"我们"）作为 Fliance（梵响）数字资产交易平台的运营主体，特此向您披露与数字资产交易相关的重大风险。',
    '本风险披露是《服务条款》的组成部分。您使用本平台进行交易，即表示您已完整阅读、充分理解并自愿承担本文件所述的全部风险。',
  ],
  sections: [
    {
      heading: '总体风险警示',
      body: [
        '数字资产价格波动剧烈且难以预测。交易数字资产可能导致重大损失，并不适合所有投资者。',
        '您应当根据自身的财务状况、投资经验、风险承受能力和投资目标，独立判断交易是否适合您，必要时咨询专业的独立财务顾问。',
        '您不应使用借贷资金或无法承受损失的资金进行交易。',
      ],
      warning: true,
    },
    {
      heading: '价格波动风险',
      body: [
        '数字资产市场全天候运行，价格可能在短时间内出现大幅波动，单日涨跌幅远超传统金融资产的情形屡见不鲜。',
        '历史价格走势不代表未来表现，任何分析或预测均不构成对未来价格的保证。',
        '在最不利情形下，您可能损失全部投入本金。',
      ],
      warning: true,
    },
    {
      heading: '流动性风险',
      body: [
        '部分交易对的市场深度有限，大额订单可能无法按预期价格全部成交，或需要以显著不利的价格成交。',
        '在极端行情下，市场流动性可能迅速枯竭，导致订单无法成交、无法平仓或无法及时变现资产。',
      ],
    },
    {
      heading: '监管与合规风险',
      body: [
        '全球各司法辖区对数字资产的监管框架仍在快速演变。法律法规或监管政策的变化可能对数字资产的价值、可交易性及可提现性产生重大不利影响。',
        '若您的所在地出台禁止或限制性政策，您可能无法继续使用本平台的部分或全部服务。',
      ],
    },
    {
      heading: '技术与安全风险',
      body: [
        '数字资产依赖信息技术系统运行，可能遭受黑客攻击、恶意软件、钓鱼欺诈等安全威胁。尽管我们采取多层安全防护，仍无法保证绝对安全。',
        '区块链网络可能发生拥堵、分叉、出块延迟或 51% 攻击等情形，影响链上交易的确认与结算。',
        '私钥、助记词一旦丢失或泄露，相关资产可能无法找回。请务必妥善保管，切勿向任何人透露。',
      ],
    },
    {
      heading: '杠杆与衍生品风险',
      body: [
        '合约交易使用杠杆机制，会同时放大收益与亏损。小幅度的价格波动也可能导致您的保证金被全部耗尽。',
        '当持仓亏损达到强平线时，平台将按规则执行强制平仓，您可能因此损失全部保证金甚至承担穿仓损失。',
        '资金费率机制可能导致持仓方定期支付或收取费用，请充分理解后再建立仓位。',
      ],
    },
    {
      heading: 'AMM 与智能合约风险',
      body: [
        '为流动性池提供流动性存在无常损失风险：当池内资产价格比发生变化时，您的资产价值可能低于单纯持有资产的价值。',
        '智能合约可能存在未被发现的漏洞或缺陷，即使经过审计也无法完全排除，相关风险由参与者自行承担。',
      ],
    },
    {
      heading: '网络与结算风险',
      body: [
        '充提币依赖相应区块链网络的正常运行。网络拥堵、手续费过低或地址填写错误，均可能导致转账延迟、失败或资产永久丢失。',
        '请务必在充值时核对资产类型与网络。向不支持的地址或网络充值可能导致资产无法找回。',
      ],
    },
    {
      heading: '市场操纵与信息风险',
      body: [
        '数字资产市场存在市场操纵、虚假消息、项目方跑路等风险。请勿轻信社交媒体上的投资建议或"保证收益"宣传。',
        '平台展示的行情与资讯仅供参考，不构成任何投资建议、要约或推荐。',
      ],
    },
    {
      heading: '税务与合规义务',
      body: [
        '您应自行了解并履行因使用本平台而产生的全部纳税申报义务。我们不提供税务、法律或投资建议。',
      ],
    },
    {
      heading: '风险确认',
      body: [
        '您确认：已完整阅读并理解本风险披露的全部内容；理解数字资产交易可能导致重大损失，包括但不限于损失全部本金；系基于自身独立判断自愿参与交易，并自行承担全部风险与后果。',
        '本披露未穷尽与数字资产交易相关的全部风险。我们建议您持续关注市场与监管动态，审慎控制仓位与风险敞口。',
      ],
    },
  ],
};

const en: LegalDocContent = {
  title: 'Risk Disclosure',
  subtitle: 'Digital asset trading involves substantial risk — make informed decisions',
  effective: 'Effective Date: August 1, 2026 · Version 1.0',
  intro: [
    'Canival Institute Inc. ("we", "us"), the operator of the Fliance digital asset trading platform, hereby discloses to you the material risks associated with digital asset trading.',
    'This Risk Disclosure forms part of the Terms of Service. By trading on the Platform, you acknowledge that you have read, understood and voluntarily assumed all risks described herein.',
  ],
  sections: [
    {
      heading: 'General Risk Warning',
      body: [
        'Digital asset prices are extremely volatile and unpredictable. Trading may result in substantial losses and is not suitable for everyone.',
        'You should independently assess whether trading is appropriate for you given your financial situation, experience, risk tolerance and objectives, and consult an independent financial adviser where necessary.',
        'Never trade with borrowed funds or money you cannot afford to lose.',
      ],
      warning: true,
    },
    {
      heading: 'Price Volatility Risk',
      body: [
        'Digital asset markets run around the clock, and prices may swing sharply within very short periods; daily moves far exceeding those of traditional assets are common.',
        'Past performance does not indicate future results, and no analysis or forecast guarantees future prices.',
        'In the worst case, you may lose your entire invested capital.',
      ],
      warning: true,
    },
    {
      heading: 'Liquidity Risk',
      body: [
        'Some trading pairs have limited market depth; large orders may not fill at expected prices, or may only fill at significantly worse prices.',
        'In extreme conditions, liquidity may evaporate rapidly, making it impossible to fill orders, close positions or convert assets in time.',
      ],
    },
    {
      heading: 'Regulatory Risk',
      body: [
        'Regulatory frameworks for digital assets are evolving rapidly worldwide. Changes in laws or policies may materially and adversely affect the value, tradability or withdrawability of digital assets.',
        'If restrictive policies are introduced in your jurisdiction, you may lose access to some or all Platform services.',
      ],
    },
    {
      heading: 'Technology & Security Risk',
      body: [
        'Digital assets depend on information systems and may be exposed to hacking, malware and phishing. Despite multi-layer security, absolute safety cannot be guaranteed.',
        'Blockchain networks may suffer congestion, forks, block delays or 51% attacks, affecting confirmation and settlement of on-chain transactions.',
        'If private keys or seed phrases are lost or leaked, the associated assets may be unrecoverable. Keep them safe and never disclose them to anyone.',
      ],
    },
    {
      heading: 'Leverage & Derivatives Risk',
      body: [
        'Futures trading uses leverage, which magnifies both gains and losses; even small price movements can exhaust your entire margin.',
        'When losses reach the liquidation threshold, positions are force-closed per Platform rules; you may lose all margin or even owe a shortfall.',
        'Funding-rate mechanisms cause position holders to periodically pay or receive fees — understand them fully before opening positions.',
      ],
    },
    {
      heading: 'AMM & Smart Contract Risk',
      body: [
        'Providing liquidity carries impermanent-loss risk: when the price ratio of pooled assets changes, your holdings may be worth less than simply holding the assets.',
        'Smart contracts may contain undiscovered flaws that audits cannot fully eliminate; participants bear the related risks.',
      ],
    },
    {
      heading: 'Network & Settlement Risk',
      body: [
        'Deposits and withdrawals depend on the relevant blockchain networks. Congestion, insufficient fees or wrong addresses may cause delays, failures or permanent loss of assets.',
        'Always verify the asset type and network before depositing. Deposits to unsupported addresses or networks may be unrecoverable.',
      ],
    },
    {
      heading: 'Market Manipulation & Information Risk',
      body: [
        'Digital asset markets are exposed to manipulation, misinformation and project abandonment. Do not trust investment advice or "guaranteed return" claims on social media.',
        'Market data and information shown on the Platform are for reference only and do not constitute investment advice, offers or recommendations.',
      ],
    },
    {
      heading: 'Tax & Compliance Obligations',
      body: [
        'You are solely responsible for understanding and fulfilling all tax filing obligations arising from your use of the Platform. We do not provide tax, legal or investment advice.',
      ],
    },
    {
      heading: 'Risk Acknowledgement',
      body: [
        'You acknowledge that you have read and understood this Risk Disclosure in full; that digital asset trading may cause substantial losses, including loss of your entire principal; and that you trade voluntarily based on your own independent judgement, bearing all risks and consequences.',
        'This disclosure does not exhaust all risks of digital asset trading. We encourage you to keep following market and regulatory developments and to manage positions and exposures prudently.',
      ],
    },
  ],
};

export function RiskDisclosurePage() {
  return <LegalDocPage zh={zh} en={en} />;
}
