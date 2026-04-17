package lighter

// Lighter API response types
// Base URL: https://mainnet.zklighter.elliot.ai/api/v1

// apiResponse is the generic envelope for paginated Lighter API responses.
type apiResponse[T any] struct {
	Code       int    `json:"code"`
	Message    string `json:"message"`
	NextCursor string `json:"next_cursor"`
	Data       []T    `json:"data"`
}

// lighterTrade represents a single trade from GET /api/v1/trades
type lighterTrade struct {
	TradeID         string `json:"trade_id"`
	MarketID        int    `json:"market_id"`
	AskAccountID    int    `json:"ask_account_id"`
	BidAccountID    int    `json:"bid_account_id"`
	Price           string `json:"price"`
	Quantity        string `json:"quantity"`
	TakerFee        string `json:"taker_fee"`
	MakerFee        string `json:"maker_fee"`
	IsBuyerTaker    bool   `json:"is_buyer_taker"`
	Timestamp       int64  `json:"timestamp"` // Unix milliseconds
	AskOrderID      string `json:"ask_order_id"`
	BidOrderID      string `json:"bid_order_id"`
}

// lighterFunding represents a funding payment from GET /api/v1/positionFunding
type lighterFunding struct {
	FundingID   string `json:"funding_id"`
	AccountID   int    `json:"account_id"`
	MarketID    int    `json:"market_id"`
	Amount      string `json:"amount"` // Signed: negative = paid, positive = received
	Timestamp   int64  `json:"timestamp"`
}

// lighterDeposit represents a deposit/withdrawal from GET /api/v1/deposit/history
type lighterDeposit struct {
	DepositID   string `json:"deposit_id"`
	AccountID   int    `json:"account_id"`
	L1Address   string `json:"l1_address"`
	AssetID     int    `json:"asset_id"`
	Amount      string `json:"amount"` // Positive = deposit, negative = withdrawal
	Status      string `json:"status"` // "completed", "pending", etc.
	Timestamp   int64  `json:"timestamp"`
	TxHash      string `json:"tx_hash"`
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
	Status          string              `json:"status"`
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

// lighterOrderBookDetail represents market metadata from the API
type lighterOrderBookDetail struct {
	MarketID   int    `json:"market_id"`
	Symbol     string `json:"symbol"`
	BaseAsset  string `json:"base_asset"`
	QuoteAsset string `json:"quote_asset"`
	MarketType string `json:"market_type"` // "perp" or "spot"
}

// lighterOrderBookDetailsResp is the response from GET /api/v1/orderBookDetails
type lighterOrderBookDetailsResp struct {
	OrderBooks []lighterOrderBookDetail `json:"order_books"`
}
