package hyperliquid

// Hyperliquid API request/response types
// Base URL: https://api.hyperliquid.xyz/info (POST with JSON body)

// hlFill represents a single trade fill from Hyperliquid API
// Request: {"type": "userFills", "user": "0x..."}
// or with pagination: {"type": "userFills", "user": "0x...", "startTime": 1234567890000}
type hlFill struct {
	Time      int64  `json:"time"`      // Unix milliseconds
	Coin      string `json:"coin"`      // e.g., "ETH" for perp, "ETH-SPOT" for spot
	Side      string `json:"side"`      // "B" (buy) or "A" (sell/ask)
	Px        string `json:"px"`        // Price
	Sz        string `json:"sz"`        // Size/quantity
	Fee       string `json:"fee"`       // Fee in USDC
	Tid       int64  `json:"tid"`       // Trade ID (unique)
	ClosedPnl string `json:"closedPnl"` // Realized PnL on this fill
	Hash      string `json:"hash"`      // Transaction hash
	StartPosition string `json:"startPosition"` // Position before fill
	Dir       string `json:"dir"`       // Direction description
	Oid       int64  `json:"oid"`       // Order ID
}

// hlFundingEntry represents a single funding payment from Hyperliquid API
// Request: {"type": "userFunding", "user": "0x...", "startTime": 1234567890000, "endTime": ...}
// Response wraps funding fields inside a "delta" object.
type hlFundingEntry struct {
	Time  int64          `json:"time"` // Unix milliseconds
	Hash  string         `json:"hash"` // Transaction hash
	Delta hlFundingDelta `json:"delta"`
}

// hlFundingDelta holds the funding payment details inside the delta wrapper
type hlFundingDelta struct {
	Coin        string `json:"coin"`        // e.g., "ETH"
	FundingRate string `json:"fundingRate"` // Funding rate
	NSamples    int    `json:"nSamples"`    // Number of samples (1 = hourly, 24 = daily)
	Usdc        string `json:"usdc"`        // USDC amount (signed)
	Szi         string `json:"szi"`         // Position size at time of funding
	Type        string `json:"type"`        // Always "funding"
}

// hlLedgerEntry represents a non-funding ledger update from Hyperliquid API
// Request: {"type": "userNonFundingLedgerUpdates", "user": "0x...", "startTime": ...}
type hlLedgerEntry struct {
	Time  int64         `json:"time"` // Unix milliseconds
	Hash  string        `json:"hash"` // Transaction hash
	Delta hlLedgerDelta `json:"delta"`
}

// hlLedgerDelta holds the details of a ledger update
type hlLedgerDelta struct {
	Type    string `json:"type"`    // "deposit", "withdraw", "internalTransfer", etc.
	Usdc    string `json:"usdc"`    // USDC amount (string)
	Amount  string `json:"amount"`  // Amount (sometimes used instead of usdc)
	Token   string `json:"token"`   // Token name (for non-USDC)
	Fee     string `json:"fee"`     // Fee (if applicable)
	Nonce   int64  `json:"nonce"`   // Nonce
	Destination string `json:"destination"` // Destination address (for withdrawals)
}

// hlClearinghouseState represents the clearinghouse state for a user
// Request: {"type": "clearinghouseState", "user": "0x..."}
type hlClearinghouseState struct {
	AssetPositions []struct {
		Position struct {
			Coin          string `json:"coin"`
			Szi           string `json:"szi"`
			EntryPx       string `json:"entryPx"`
			PositionValue string `json:"positionValue"`
			UnrealizedPnl string `json:"unrealizedPnl"`
		} `json:"position"`
	} `json:"assetPositions"`
	MarginSummary struct {
		AccountValue string `json:"accountValue"`
	} `json:"marginSummary"`
}

// hlSpotClearinghouseState represents the spot clearinghouse state for a user
// Request: {"type": "spotClearinghouseState", "user": "0x..."}
type hlSpotClearinghouseState struct {
	Balances []struct {
		Coin  string `json:"coin"`
		Total string `json:"total"`
		Hold  string `json:"hold"`
	} `json:"balances"`
}
