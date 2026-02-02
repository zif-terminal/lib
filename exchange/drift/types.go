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
