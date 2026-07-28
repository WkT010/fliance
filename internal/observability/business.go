package observability

// Pre-registered business metrics for the exchange. These are incremented from
// the matching engine, order handler and wallet service.
var (
	OrdersReceivedTotal = MustCounter(
		"orders_received_total",
		"Total orders submitted to the API (accepted or rejected).",
	)
	OrdersAcceptedTotal = MustCounter(
		"orders_accepted_total",
		"Orders that passed validation and were submitted to the matching engine.",
	)
	OrdersRejectedTotal = MustCounter(
		"orders_rejected_total",
		"Orders rejected by validation (bad params, insufficient funds, etc.).",
	)
	OrdersCancelledTotal = MustCounter(
		"orders_cancelled_total",
		"Orders cancelled by the user.",
	)
	TradesExecutedTotal = MustCounter(
		"trades_executed_total",
		"Total number of trade fills produced by the matching engine.",
	)
	TradeVolumeBase = MustGauge(
		"trade_volume_base_total",
		"Cumulative base-asset volume traded (running total; reset on process restart).",
	)
	TradeVolumeQuote = MustGauge(
		"trade_volume_quote_total",
		"Cumulative quote-asset volume traded (running total; reset on process restart).",
	)
	DepositsTotal = MustCounter(
		"deposits_total",
		"Total number of completed deposits.",
	)
	WithdrawalsTotal = MustCounter(
		"withdrawals_total",
		"Total number of completed withdrawals.",
	)
	SettlementsTotal = MustCounter(
		"settlements_total",
		"Total number of trade settlements processed by the wallet service.",
	)
	SettlementFailuresTotal = MustCounter(
		"settlement_failures_total",
		"Number of trade settlements that failed and were rolled back.",
	)
	LoginAttemptsTotal = MustCounter(
		"login_attempts_total",
		"Total login attempts.",
	)
	LoginFailuresTotal = MustCounter(
		"login_failures_total",
		"Failed login attempts.",
	)
	AccountsLockedTotal = MustCounter(
		"accounts_locked_total",
		"Number of accounts locked due to repeated failed logins.",
	)
	RegistrationsTotal = MustCounter(
		"registrations_total",
		"Total successful user registrations.",
	)
)
