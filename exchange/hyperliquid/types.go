package hyperliquid

// Hyperliquid API request/response types
// Base URL: https://api.hyperliquid.xyz/info (POST with JSON body)

// hlFill represents a single trade fill from Hyperliquid API
// Request: {"type": "userFillsByTime", "user": "0x...", "startTime": 1234567890000}
// Returns fills sorted by timestamp ascending (oldest first).
type hlFill struct {
	Time      int64  `json:"time"`      // Unix milliseconds
	Coin      string `json:"coin"`      // e.g., "ETH" for perp, "ETH-SPOT" for spot
	Side      string `json:"side"`      // "B" (buy) or "A" (sell/ask)
	Px        string `json:"px"`        // Price
	Sz        string `json:"sz"`        // Size/quantity
	Fee       string `json:"fee"`       // Fee amount (denominated in FeeToken)
	Tid       int64  `json:"tid"`       // Trade ID (unique)
	ClosedPnl string `json:"closedPnl"` // Realized PnL on this fill
	Hash      string `json:"hash"`      // Transaction hash
	StartPosition string `json:"startPosition"` // Position before fill
	Dir       string `json:"dir"`       // Direction description (e.g., "Open Long", "Close Short", "Sell", "Spot Dust Conversion")
	Oid       int64  `json:"oid"`       // Order ID
	FeeToken  string `json:"feeToken"`  // Fee denomination token
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
	Type        string `json:"type"`        // "deposit", "withdraw", "internalTransfer", "spotTransfer", etc.
	Usdc        string `json:"usdc"`        // USDC amount (string)
	Amount      string `json:"amount"`      // Amount (sometimes used instead of usdc)
	Token       string `json:"token"`       // Token name (for non-USDC)
	Fee         string `json:"fee"`         // Fee (if applicable)
	Nonce       int64  `json:"nonce"`       // Nonce
	Destination string `json:"destination"` // Destination address (for withdrawals/spotTransfer)
	User        string `json:"user"`        // Source user address (for spotTransfer outbound)
	ToPerp          bool   `json:"toPerp"`          // Direction for accountClassTransfer (true = spot→perp)
	UsdcValue       string `json:"usdcValue"`       // USDC value of spot transfer (for price derivation)
	NetWithdrawnUsd string `json:"netWithdrawnUsd"` // Actual USDC returned to user on vaultWithdraw
	IsDeposit       bool   `json:"isDeposit"`       // Direction for cStakingTransfer (true = stake/leave balance)
	Vault           string `json:"vault"`           // Vault address (for vault-related entries)
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
		TotalRawUsd  string `json:"totalRawUsd"`
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

// hlSubAccount represents a subaccount returned by the subAccounts API
// Request: {"type": "subAccounts", "user": "0x..."}
// Returns null (no subaccounts) or a list of hlSubAccount.
type hlSubAccount struct {
	SubAccountUser string `json:"subAccountUser"`
	Name           string `json:"name"`
	Master         string `json:"master"`
}

// hlBorrowLendInterest represents a single borrow/lend interest entry from Hyperliquid API
// Request: {"type": "userBorrowLendInterest", "user": "0x...", "startTime": 0}
// Returns hourly interest entries per token, sorted ascending by time.
type hlBorrowLendInterest struct {
	Time   int64  `json:"time"`   // Unix milliseconds
	Token  string `json:"token"`  // e.g., "USDC", "HYPE"
	Borrow string `json:"borrow"` // Interest paid (cost)
	Supply string `json:"supply"` // Interest earned (yield)
}

// hlVaultEquity represents a vault the user participates in
// Request: {"type": "userVaultEquities", "user": "0x..."}
type hlVaultEquity struct {
	VaultAddress string `json:"vaultAddress"`
	Equity       string `json:"equity"`
}

// hlVaultDetails represents vault details from the API
// Request: {"type": "vaultDetails", "vaultAddress": "0x..."}
type hlVaultDetails struct {
	Name       string `json:"name"`
	VaultAddress string `json:"vaultAddress"`
	Leader     string `json:"leader"`
}
