package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"strings"

	"lukechampine.com/blake3"
)

// ClientIDKey is the context key for storing the authenticated client ID
type ClientIDKey struct{}

// GenerateKey creates a new API key in the format: sk-{client_id}-{pepper}-{keyed_blake3_hash}
func GenerateKey(clientID string, masterKey string) (string, error) {
	// Generate a random 8-byte pepper
	pepperBytes := make([]byte, 8)
	if _, err := rand.Read(pepperBytes); err != nil {
		return "", fmt.Errorf("failed to generate pepper: %v", err)
	}
	pepper := hex.EncodeToString(pepperBytes)

	// Calculate MAC
	mac := calculateMAC(clientID, pepper, masterKey)

	// Construct final key
	return fmt.Sprintf("sk-%s-%s-%s", clientID, pepper, mac), nil
}

// ValidateKey checks if the provided API key is valid using the master key.
// It returns the extracted clientID if valid.
func ValidateKey(apiKey string, masterKey string) (string, error) {
	parts := strings.Split(apiKey, "-")
	if len(parts) != 4 || parts[0] != "sk" {
		return "", fmt.Errorf("invalid api key format")
	}

	clientID := parts[1]
	pepper := parts[2]
	providedMAC := parts[3]

	expectedMAC := calculateMAC(clientID, pepper, masterKey)

	// Use constant time comparison to prevent timing attacks
	if subtle.ConstantTimeCompare([]byte(providedMAC), []byte(expectedMAC)) != 1 {
		return "", fmt.Errorf("invalid api key signature")
	}

	return clientID, nil
}

// calculateMAC generates a BLAKE3 Keyed MAC over the clientID and pepper.
func calculateMAC(clientID, pepper, masterKey string) string {
	// Ensure the master key is exactly 32 bytes for BLAKE3 keyed mode.
	// We'll hash the user-provided master key with regular BLAKE3 to derive a 32-byte key.
	derivedKey := blake3.Sum256([]byte(masterKey))

	// Create a new keyed hasher
	hasher := blake3.New(32, derivedKey[:])
	hasher.Write([]byte(clientID))
	hasher.Write([]byte(pepper))

	return hex.EncodeToString(hasher.Sum(nil))
}
