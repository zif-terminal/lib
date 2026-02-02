package lighter

// lighterAccountsResponse wraps the response from /api/v1/accountsByL1Address
type lighterAccountsResponse struct {
	Code        int              `json:"code"`
	L1Address   string           `json:"l1_address"`
	SubAccounts []lighterAccount `json:"sub_accounts"`
}

// lighterAccount represents account data from the API
type lighterAccount struct {
	Index       int    `json:"index"`
	L1Address   string `json:"l1_address"`
	AccountType int    `json:"account_type"` // 0 = main, 1 = sub
	Collateral  string `json:"collateral"`
}

// lighterOrdersResponse wraps the response from /api/v1/accountInactiveOrders
type lighterOrdersResponse struct {
	Code       int            `json:"code"`
	Orders     []lighterOrder `json:"orders"`
	NextCursor string         `json:"next_cursor"`
}

// lighterOrder represents an order/trade from accountInactiveOrders
// Filled orders are treated as trades
type lighterOrder struct {
	OrderID           string `json:"order_id"`
	OwnerAccountIndex int    `json:"owner_account_index"`
	MarketIndex       int    `json:"market_index"`
	IsAsk             bool   `json:"is_ask"`
	Price             string `json:"price"`
	FilledBaseAmount  string `json:"filled_base_amount"`
	FilledQuoteAmount string `json:"filled_quote_amount"`
	Timestamp         int64  `json:"timestamp"` // Unix seconds
	Status            string `json:"status"`
	Type              string `json:"type"`
	Side              string `json:"side"`
}

// lighterTradesResponse wraps the response from /api/v1/trades
type lighterTradesResponse struct {
	Code       int            `json:"code"`
	Trades     []lighterTrade `json:"trades"`
	NextCursor string         `json:"next_cursor"`
}

// lighterTrade represents an individual trade/fill from /api/v1/trades
type lighterTrade struct {
	TradeID      int64  `json:"trade_id"`
	TxHash       string `json:"tx_hash"`
	Type         string `json:"type"` // "trade" or "liquidation"
	MarketID     int    `json:"market_id"`
	Size         string `json:"size"`
	Price        string `json:"price"`
	USDAmount    string `json:"usd_amount"`
	AskID        int64  `json:"ask_id"`
	BidID        int64  `json:"bid_id"`
	AskAccountID int64  `json:"ask_account_id"`
	BidAccountID int64  `json:"bid_account_id"`
	IsMakerAsk   bool   `json:"is_maker_ask"`
	BlockHeight  int64  `json:"block_height"`
	Timestamp    int64  `json:"timestamp"` // Unix milliseconds
	TakerFee     int64  `json:"taker_fee"`
}

// lighterFundingResponse wraps the response from /api/v1/positionFunding
type lighterFundingResponse struct {
	Code             int                      `json:"code"`
	PositionFundings []lighterPositionFunding `json:"position_fundings"`
	NextCursor       string                   `json:"next_cursor"`
}

// lighterPositionFunding represents a funding payment
type lighterPositionFunding struct {
	FundingID    int    `json:"funding_id"`
	MarketID     int    `json:"market_id"`
	Change       string `json:"change"`
	Rate         string `json:"rate"`
	PositionSize string `json:"position_size"`
	PositionSide string `json:"position_side"`
	Timestamp    int64  `json:"timestamp"` // Unix seconds
}

// lighterMarketsResponse wraps the response from /api/v1/orderBookDetails
type lighterMarketsResponse struct {
	Code                 int             `json:"code"`
	OrderBookDetails     []lighterMarket `json:"order_book_details"`
	SpotOrderBookDetails []lighterMarket `json:"spot_order_book_details"`
}

// lighterMarket represents market info for symbol resolution
type lighterMarket struct {
	MarketID   int    `json:"market_id"`
	Symbol     string `json:"symbol"`
	MarketType string `json:"market_type"` // "perp" or "spot"
	Status     string `json:"status"`
}
