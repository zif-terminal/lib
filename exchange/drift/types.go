package drift

// Drift API response types
// Base URL: https://data.api.drift.trade

// Precision constants for Drift API
// Drift returns raw integer values that need precision adjustment
const (
	// BASE_PRECISION for position sizes (divide by 10^9)
	BasePrecision = 1_000_000_000
	// QUOTE_PRECISION for USD values, fees, funding (divide by 10^6)
	QuotePrecision = 1_000_000
)

// driftTradeRecord represents a single trade/fill from Drift API
// GET /user/{accountId}/trades
type driftTradeRecord struct {
	Ts                     int64  `json:"ts"`                     // Unix timestamp (seconds)
	TxSig                  string `json:"txSig"`                  // Transaction signature
	TxSigIndex             int    `json:"txSigIndex"`             // Index within tx
	FillRecordID           string `json:"fillRecordId"`           // Unique fill ID
	BaseAssetAmountFilled  string `json:"baseAssetAmountFilled"`  // Quantity (raw, divide by BasePrecision)
	QuoteAssetAmountFilled string `json:"quoteAssetAmountFilled"` // Value (raw, divide by QuotePrecision)
	TakerFee               string `json:"takerFee"`               // Fee if taker (raw, divide by QuotePrecision)
	MakerFee               string `json:"makerFee"`               // Fee if maker (raw, divide by QuotePrecision)
	TakerOrderDirection    string `json:"takerOrderDirection"`    // "long"/"short"
	MakerOrderDirection    string `json:"makerOrderDirection"`    // "long"/"short"
	TakerOrderID           string `json:"takerOrderId"`
	MakerOrderID           string `json:"makerOrderId"`
	Taker                  string `json:"taker"`       // Taker account
	Maker                  string `json:"maker"`       // Maker account
	User                   string `json:"user"`        // Account for this record
	Symbol                 string `json:"symbol"`      // e.g., "SOL-PERP"
	MarketIndex            int    `json:"marketIndex"` // Market index
	MarketType             string `json:"marketType"`  // "perp"/"spot"
	OraclePrice            string `json:"oraclePrice"` // Oracle price at fill time
}

// driftFundingPayment represents a single funding payment from Drift API
// GET /user/{accountId}/fundingPayments
type driftFundingPayment struct {
	Ts              int64  `json:"ts"`              // Unix timestamp (seconds)
	TxSig           string `json:"txSig"`           // Transaction signature
	TxSigIndex      int    `json:"txSigIndex"`      // Index within tx
	MarketIndex     int    `json:"marketIndex"`     // Market index
	FundingPayment  string `json:"fundingPayment"`  // Amount (signed, raw, divide by QuotePrecision)
	User            string `json:"user"`            // Account public key
	BaseAssetAmount string `json:"baseAssetAmount"` // Position size at payment time
}

// driftTradesResponse wraps the trades API response
type driftTradesResponse struct {
	Success bool               `json:"success"`
	Records []driftTradeRecord `json:"records"`
	Meta    driftMeta          `json:"meta"`
}

// driftFundingResponse wraps the funding payments API response
type driftFundingResponse struct {
	Success bool                  `json:"success"`
	Records []driftFundingPayment `json:"records"`
	Meta    driftMeta             `json:"meta"`
}

// driftMeta contains pagination metadata
type driftMeta struct {
	NextPage interface{} `json:"nextPage"` // string page number or nil when no more pages
}

// driftMarket represents market info from /stats/markets
type driftMarket struct {
	MarketIndex int    `json:"marketIndex"`
	Symbol      string `json:"symbol"`     // e.g., "SOL-PERP"
	BaseAsset   string `json:"baseAsset"`  // e.g., "SOL"
	MarketType  string `json:"marketType"` // "perp" or "spot"
}

// driftMarketsResponse wraps the markets API response
type driftMarketsResponse struct {
	Success bool          `json:"success"`
	Markets []driftMarket `json:"markets"`
}

// driftAuthorityAccount represents a subaccount from /authority/{wallet}/accounts
type driftAuthorityAccount struct {
	AccountID    string `json:"accountId"`    // Subaccount public key (used for API calls)
	SubAccountID int    `json:"subAccountId"` // 0 for main, 1+ for subaccounts
	Name         string `json:"name"`         // Account name
	Authority    string `json:"authority"`    // Wallet address that owns this account
}

// driftAuthorityAccountsResponse wraps the authority accounts API response
type driftAuthorityAccountsResponse struct {
	Success  bool                    `json:"success"`
	Accounts []driftAuthorityAccount `json:"accounts"`
}

// driftDepositRecord represents a single deposit/withdrawal from Drift API
// GET /user/{accountId}/deposits
type driftDepositRecord struct {
	Ts              int64  `json:"ts"`              // Unix timestamp (seconds)
	TxSig           string `json:"txSig"`           // Transaction signature
	Slot            int64  `json:"slot"`            // Solana slot
	Amount          string `json:"amount"`          // Amount (raw, divide by token precision)
	MarketIndex     int    `json:"marketIndex"`     // Spot market index
	DepositRecordID string `json:"depositRecordId"` // Unique record ID
	Direction       string `json:"direction"`       // "deposit" or "withdraw"
	OraclePrice     string `json:"oraclePrice"`     // Oracle price at time of deposit/withdrawal
	User            string `json:"user"`            // Account public key
}

// driftDepositsResponse wraps the deposits API response
type driftDepositsResponse struct {
	Success bool                 `json:"success"`
	Records []driftDepositRecord `json:"records"`
	Meta    driftMeta            `json:"meta"`
}

// driftSwapRecord represents a single swap from Drift API
// GET /user/{accountId}/swaps
type driftSwapRecord struct {
	Ts             int64  `json:"ts"`             // Unix timestamp (seconds)
	TxSig          string `json:"txSig"`          // Transaction signature
	TxSigIndex     int    `json:"txSigIndex"`     // Index within tx
	Slot           int64  `json:"slot"`           // Solana slot
	User           string `json:"user"`           // Account public key
	OutMarketIndex int    `json:"outMarketIndex"` // Market index of asset sold
	InMarketIndex  int    `json:"inMarketIndex"`  // Market index of asset bought
	AmountOut      string `json:"amountOut"`      // Amount sold (quote)
	AmountIn       string `json:"amountIn"`       // Amount bought (base)
	OutOraclePrice string `json:"outOraclePrice"` // Oracle price of asset sold
	InOraclePrice  string `json:"inOraclePrice"`  // Oracle price of asset bought
	Fee            string `json:"fee"`            // Swap fee
	OutSymbol      string `json:"outSymbol"`      // Symbol of asset sold (e.g., "USDC")
	InSymbol       string `json:"inSymbol"`       // Symbol of asset bought (e.g., "SOL")
}

// driftSwapsResponse wraps the swaps API response
type driftSwapsResponse struct {
	Success bool              `json:"success"`
	Records []driftSwapRecord `json:"records"`
	Meta    driftMeta         `json:"meta"`
}

// driftSettlePnlRecord represents a single PnL settlement from Drift API
// GET /user/{accountId}/settlePnl
type driftSettlePnlRecord struct {
	Ts                    int64  `json:"ts"`                    // Unix timestamp (seconds)
	TxSig                 string `json:"txSig"`                 // Transaction signature
	TxSigIndex            int    `json:"txSigIndex"`            // Index within tx
	Slot                  int64  `json:"slot"`                  // Solana slot
	Pnl                   string `json:"pnl"`                   // Settled PnL amount (positive = profit, negative = loss)
	User                  string `json:"user"`                  // Account public key
	BaseAssetAmount       string `json:"baseAssetAmount"`       // Position size at settlement time
	QuoteAssetAmountAfter string `json:"quoteAssetAmountAfter"` // Quote amount after settlement
	QuoteEntryAmount      string `json:"quoteEntryAmount"`      // Entry quote amount
	SettlePrice           string `json:"settlePrice"`           // Settlement price
	MarketIndex           int    `json:"marketIndex"`           // Perp market index
	Explanation           string `json:"explanation"`           // Settlement explanation
}

// driftSettlePnlResponse wraps the settlePnl API response
type driftSettlePnlResponse struct {
	Success bool                   `json:"success"`
	Records []driftSettlePnlRecord `json:"records"`
	Meta    driftMeta              `json:"meta"`
}
