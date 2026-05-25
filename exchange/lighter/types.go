package lighter

// Lighter API response types
// Base URL: https://mainnet.zklighter.elliot.ai/api/v1
//
// The Lighter API uses code=200 for success (not code=0). Each endpoint
// returns its data under an endpoint-specific key (e.g. "trades", "deposits",
// "position_fundings") rather than a generic "data" key. Cursor fields also
// differ by endpoint ("next_cursor" for trades, "cursor" for deposits).

// tradesResponse is the response envelope for GET /api/v1/trades.
type tradesResponse struct {
	Code       int            `json:"code"`
	Message    string         `json:"message"`
	NextCursor string         `json:"next_cursor"`
	Trades     []lighterTrade `json:"trades"`
}

// depositsResponse is the response envelope for GET /api/v1/deposit/history.
type depositsResponse struct {
	Code     int              `json:"code"`
	Message  string           `json:"message"`
	Cursor   string           `json:"cursor"`
	Deposits []lighterDeposit `json:"deposits"`
}

// fundingResponse is the response envelope for GET /api/v1/positionFunding.
type fundingResponse struct {
	Code             int               `json:"code"`
	Message          string            `json:"message"`
	NextCursor       string            `json:"next_cursor"`
	PositionFundings []lighterFunding  `json:"position_fundings"`
}

// lighterTrade represents a single trade from GET /api/v1/trades.
// Field names and types match the actual API response.
type lighterTrade struct {
	TradeID      int64  `json:"trade_id"`
	TradeIDStr   string `json:"trade_id_str"`
	MarketID     int    `json:"market_id"`
	AskAccountID int    `json:"ask_account_id"`
	BidAccountID int    `json:"bid_account_id"`
	Price        string `json:"price"`
	Size         string `json:"size"`
	UsdAmount    string `json:"usd_amount"` // notional in USDC (size × price); used to compute fee from the rate fields.
	TakerFee     int64  `json:"taker_fee"`  // rate in micro-percent of notional (200 = 2 bps); fee USDC = rate × usd_amount / 1e6. Null in JSON → 0.
	MakerFee     int64  `json:"maker_fee"`  // same encoding as TakerFee.
	IsMakerAsk   bool   `json:"is_maker_ask"`
	Timestamp    int64  `json:"timestamp"` // Unix milliseconds
	AskID        int64  `json:"ask_id"`
	AskIDStr     string `json:"ask_id_str"`
	BidID        int64  `json:"bid_id"`
	BidIDStr     string `json:"bid_id_str"`
}

// lighterFunding represents a funding payment from GET /api/v1/positionFunding.
// Field names match the actual API response.
type lighterFunding struct {
	FundingID    int64  `json:"funding_id"`
	MarketID     int    `json:"market_id"`
	Change       string `json:"change"` // Signed: negative = paid, positive = received
	Rate         string `json:"rate"`
	PositionSize string `json:"position_size"`
	PositionSide string `json:"position_side"`
	Timestamp    int64  `json:"timestamp"` // Unix seconds
}

// lighterDeposit represents a deposit/withdrawal from GET /api/v1/deposit/history.
// Field names match the actual API response.
type lighterDeposit struct {
	DepositID string `json:"id"`
	AssetID   int    `json:"asset_id"`
	Amount    string `json:"amount"` // Positive = deposit, negative = withdrawal
	Status    string `json:"status"` // "completed", "pending", etc.
	Timestamp int64  `json:"timestamp"`
	TxHash    string `json:"l1_tx_hash"`
}

// lighterAccountResp is the response from GET /api/v1/account
// Reuses the structure observed in portfolio_monitor/fetcher_lighter.go
// Note: the API returns {"code":21100,"message":"account not found"} for unknown addresses
// (HTTP 200 but non-200 code field), so callers must check the Code field.
type lighterAccountResp struct {
	Code     int              `json:"code"`
	Accounts []lighterAccount `json:"accounts"`
}

// lighterAccount represents a single account within the response.
//
// AccountType distinguishes the layout of balance data:
//   - 0 = main account: spot balances live in Assets[].Balance.
//   - 1 = sub-account (cross-margin): Assets[].Balance is always 0; the real
//     USDC collateral is in Collateral / AvailableBalance at the top level.
type lighterAccount struct {
	AccountIndex    int                 `json:"account_index"`
	AccountType     int                 `json:"account_type"`
	L1Address       string              `json:"l1_address"`
	Status          int                 `json:"status"`
	TotalAssetValue string              `json:"total_asset_value"`
	Collateral      string              `json:"collateral"`
	AvailableBalance string             `json:"available_balance"`
	Positions       []lighterPosition   `json:"positions"`
	Assets          []lighterAsset      `json:"assets"`
}

// lighterPosition represents an open position on the account
type lighterPosition struct {
	Symbol          string  `json:"symbol"`
	MarketID        int     `json:"market_id"`
	Position        string  `json:"position"`
	AvgEntryPrice   string  `json:"avg_entry_price"`
	UnrealizedPnl   string  `json:"unrealized_pnl"`
	RealizedPnl     string  `json:"realized_pnl"`
	LiquidationPrice *string `json:"liquidation_price"`
	MarginMode      int     `json:"margin_mode"`
	AllocatedMargin string  `json:"allocated_margin"`
}

// lighterAsset represents an asset balance on the account
type lighterAsset struct {
	Symbol        string `json:"symbol"`
	AssetID       int    `json:"asset_id"`
	Balance       string `json:"balance"`
	LockedBalance string `json:"locked_balance"`
}

// transfersResponse is the response envelope for GET /api/v1/transfer/history.
type transfersResponse struct {
	Code      int               `json:"code"`
	Message   string            `json:"message"`
	Cursor    string            `json:"cursor"`
	Transfers []lighterTransfer `json:"transfers"`
}

// lighterTransfer represents an L2 internal transfer from GET /api/v1/transfer/history.
type lighterTransfer struct {
	ID               string `json:"id"`
	AssetID          int    `json:"asset_id"`
	Amount           string `json:"amount"`
	Fee              string `json:"fee"`
	Timestamp        int64  `json:"timestamp"` // Unix milliseconds
	Type             string `json:"type"`      // L2TransferInflow, L2TransferOutflow, L2SelfTransfer, L2StakeAssetOutflow
	FromL1Address    string `json:"from_l1_address"`
	ToL1Address      string `json:"to_l1_address"`
	FromAccountIndex int64  `json:"from_account_index"`
	ToAccountIndex   int64  `json:"to_account_index"`
	FromRoute        string `json:"from_route"`
	ToRoute          string `json:"to_route"`
	TxHash           string `json:"tx_hash"`
}

// withdrawsResponse is the response envelope for GET /api/v1/withdraw/history.
type withdrawsResponse struct {
	Code      int                `json:"code"`
	Message   string             `json:"message"`
	Cursor    string             `json:"cursor"`
	Withdraws []lighterWithdraw  `json:"withdraws"`
}

// lighterWithdraw represents an L1 withdrawal from GET /api/v1/withdraw/history.
type lighterWithdraw struct {
	ID        string `json:"id"`
	AssetID   int    `json:"asset_id"`
	Amount    string `json:"amount"`
	Timestamp int64  `json:"timestamp"` // Unix milliseconds
	Status    string `json:"status"`    // "completed", "pending", etc.
	Type      string `json:"type"`      // "fast", etc.
	L1TxHash  string `json:"l1_tx_hash"`
}

// liquidationsResponse is the response envelope for GET /api/v1/liquidations.
//
// Lighter exposes a forced-liquidation feed that complements /trades. Every
// liquidation event has a corresponding row in /trades (so the position
// reduction itself is already captured), but /trades reports only the regular
// taker_fee in micro-USDC. The /liquidations feed exposes a separate
// liquidation penalty fee (string field, USDC) charged in addition to the
// underlying fill — that fee is what causes residual snapshot-match gaps when
// not ingested. The fee is emitted as a TypeFee transfer; the embedded trade
// is NOT re-emitted (to avoid double-counting against /trades).
type liquidationsResponse struct {
	Code          int                 `json:"code"`
	Message       string              `json:"message"`
	NextCursor    string              `json:"next_cursor"`
	Liquidations  []lighterLiquidation `json:"liquidations"`
}

// lighterLiquidation represents a single liquidation event from
// GET /api/v1/liquidations. We capture the identification fields, the embedded
// trade (for the penalty fee), and the `info.risk_info_before` /
// `info.risk_info_after` pair (for the cross+isolated collateral delta). The
// remaining fields of `info` (positions, mark_prices, asset_index_prices,
// assets) are intentionally omitted — they are large and unused by accounting.
//
// The collateral delta is required because a Lighter liquidation migrates
// collateral OUT of the user's account in a way that /trades cannot fully
// reconstruct: the quote-leg notional of the embedded trade plus the taker_fee
// is materially smaller than the actual USDC outflow (the difference goes to
// the insurance fund / counterparty margin). Without the risk_info delta we
// systematically under-debit USDC by ~$50/event.
type lighterLiquidation struct {
	ID         int64                   `json:"id"`
	MarketID   int                     `json:"market_id"`
	Type       string                  `json:"type"`         // "partial", "full", etc.
	ExecutedAt int64                   `json:"executed_at"`  // Unix milliseconds
	Trade      lighterLiquidationTrade `json:"trade"`
	Info       lighterLiquidationInfo  `json:"info"`
}

// lighterLiquidationInfo carries the pre/post risk snapshots from
// /api/v1/liquidations. Both sides expose `cross_risk_parameters` (a single
// bucket for the cross-margin account) and `isolated_risk_parameters` (zero or
// more buckets, one per market with an isolated position). USDC collateral
// delta is computed as the sum of all buckets after minus the sum before.
type lighterLiquidationInfo struct {
	RiskInfoBefore lighterRiskInfo `json:"risk_info_before"`
	RiskInfoAfter  lighterRiskInfo `json:"risk_info_after"`
}

// lighterRiskInfo is a single side of the risk snapshot (before or after).
type lighterRiskInfo struct {
	CrossRiskParameters    lighterRiskBucket   `json:"cross_risk_parameters"`
	IsolatedRiskParameters []lighterRiskBucket `json:"isolated_risk_parameters"`
}

// lighterRiskBucket is a per-margin-context collateral bucket. Collateral is a
// string decimal denominated in USDC; market_id is 0 for the cross bucket and
// the relevant perp market for isolated buckets. We ignore market_id during
// summing — we only need the total USDC across all buckets.
type lighterRiskBucket struct {
	MarketID   int    `json:"market_id"`
	Collateral string `json:"collateral"`
}

// lighterLiquidationTrade is the embedded trade carrying the liquidation
// penalty fee. Field names and types match the live API (taker_fee/maker_fee
// are STRING decimals here, NOT micro-USDC int64s as in the regular /trades
// endpoint).
type lighterLiquidationTrade struct {
	Price           string `json:"price"`
	Size            string `json:"size"`
	TakerFee        string `json:"taker_fee"`
	MakerFee        string `json:"maker_fee"`
	TransactionTime int64  `json:"transaction_time"`
}

// lighterOrderBookDetail represents market metadata from the API.
// The API does not include base_asset/quote_asset fields; they are derived
// from the symbol (e.g. "ETH" -> base=ETH, quote=USDC for perps;
// "UNI/USDC" -> base=UNI, quote=USDC for spot).
type lighterOrderBookDetail struct {
	MarketID     int    `json:"market_id"`
	Symbol       string `json:"symbol"`
	MarketType   string `json:"market_type"` // "perp" or "spot"
	BaseAssetID  int    `json:"base_asset_id"`
	QuoteAssetID int    `json:"quote_asset_id"`
}

// lighterOrderBookDetailsResp is the response from GET /api/v1/orderBookDetails.
// Perp markets are in "order_book_details", spot markets in "spot_order_book_details".
type lighterOrderBookDetailsResp struct {
	Code              int                      `json:"code"`
	OrderBookDetails  []lighterOrderBookDetail `json:"order_book_details"`
	SpotOrderBooks    []lighterOrderBookDetail `json:"spot_order_book_details"`
}
