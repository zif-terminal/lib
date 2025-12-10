package hyperliquid

// hyperliquidFill represents a single fill from Hyperliquid API
// The API returns userFills as a direct array, not wrapped in an object
// Fields match the actual API response structure
type hyperliquidFill struct {
	Coin    string      `json:"coin"`     // Asset name (e.g., "BTC", "TNSR")
	Px      interface{} `json:"px"`      // Price (number or string)
	Sz      interface{} `json:"sz"`      // Size/Quantity (number or string)
	Side    string      `json:"side"`     // "B" (buy), "S" (sell), "A" (close)
	Time    interface{} `json:"time"`    // Unix timestamp in milliseconds
	Hash    string      `json:"hash"`     // Transaction hash
	Tid     interface{} `json:"tid"`      // Fill ID (unique per fill, used as trade_id)
	Oid     interface{} `json:"oid"`     // Order ID (number or string)
	Fee     interface{} `json:"fee"`     // Fee (number or string)
	// Additional fields that may be present but not used:
	// StartPosition, Dir, ClosedPnl, Crossed, FeeToken, TwapId
}
