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
	TakerFee     int64  `json:"taker_fee"`  // micro-USDC (divide by 1e6)
	MakerFee     int64  `json:"maker_fee"`  // micro-USDC (divide by 1e6)
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

// lighterAccount represents a single account within the response
type lighterAccount struct {
	AccountIndex    int                 `json:"account_index"`
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
