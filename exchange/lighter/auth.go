package lighter

import (
	"fmt"
)

// buildAuthToken returns the auth token to use for API requests.
// Lighter requires a user-issued API key stored in account_type_metadata.
// Randomly generated tokens are not accepted by the Lighter API.
func buildAuthToken(apiKey string, accountIndex int) (string, error) {
	if apiKey != "" {
		return apiKey, nil
	}

	return "", fmt.Errorf("lighter: no API key configured for account_index %d; a user-issued read-only API key is required", accountIndex)
}
