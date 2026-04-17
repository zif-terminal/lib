package lighter

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"
)

// defaultTokenDuration is how long a read-only token is valid for.
const defaultTokenDuration = 24 * time.Hour

// generateReadOnlyToken creates a read-only API token for Lighter.
// Format: ro:{account_index}:{scope}:{expiry_unix}:{random_hex}
// scope is "single" for one account or "all" for all accounts.
func generateReadOnlyToken(accountIndex int, scope string, expiry time.Time) (string, error) {
	randomBytes := make([]byte, 16)
	if _, err := rand.Read(randomBytes); err != nil {
		return "", fmt.Errorf("failed to generate random bytes: %w", err)
	}

	token := fmt.Sprintf("ro:%d:%s:%d:%s",
		accountIndex,
		scope,
		expiry.Unix(),
		hex.EncodeToString(randomBytes),
	)

	return token, nil
}

// buildAuthToken returns the auth token to use for API requests.
// If an API key is provided (from account metadata), it is used directly.
// Otherwise, a read-only token is generated.
func buildAuthToken(apiKey string, accountIndex int) (string, error) {
	if apiKey != "" {
		return apiKey, nil
	}

	// Generate a read-only token
	expiry := time.Now().Add(defaultTokenDuration)
	return generateReadOnlyToken(accountIndex, "single", expiry)
}
