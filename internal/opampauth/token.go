// Package opampauth provides generation and verification helpers for managed OpAMP tokens.
package opampauth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strings"

	"github.com/google/uuid"
)

const (
	tokenPrefix       = "ompt_"
	secretLength      = 32
	encodedSecretSize = 43
)

var errInvalidToken = errors.New("invalid OpAMP token")

// GeneratedToken contains a new token's public identifier, bearer value, and storage digest.
type GeneratedToken struct {
	ID         string
	Value      string
	SecretHash [32]byte
}

// Generate creates a cryptographically random managed OpAMP token.
func Generate() (GeneratedToken, error) {
	secret := make([]byte, secretLength)
	if _, err := rand.Read(secret); err != nil {
		return GeneratedToken{}, err
	}

	id, err := uuid.NewRandom()
	if err != nil {
		return GeneratedToken{}, err
	}

	value := tokenPrefix + id.String() + "." + base64.RawURLEncoding.EncodeToString(secret)
	return GeneratedToken{
		ID:         id.String(),
		Value:      value,
		SecretHash: sha256.Sum256([]byte(value)),
	}, nil
}

// ParseAndHash validates a managed OpAMP token and returns its identifier and storage digest.
func ParseAndHash(value string) (id string, secretHash [32]byte, err error) {
	if !strings.HasPrefix(value, tokenPrefix) {
		return "", [32]byte{}, errInvalidToken
	}

	parts := strings.Split(strings.TrimPrefix(value, tokenPrefix), ".")
	if len(parts) != 2 {
		return "", [32]byte{}, errInvalidToken
	}

	parsedID, err := uuid.Parse(parts[0])
	if err != nil || parts[0] != parsedID.String() {
		return "", [32]byte{}, errInvalidToken
	}

	if len(parts[1]) != encodedSecretSize {
		return "", [32]byte{}, errInvalidToken
	}
	secret, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || len(secret) != secretLength || base64.RawURLEncoding.EncodeToString(secret) != parts[1] {
		return "", [32]byte{}, errInvalidToken
	}

	return parsedID.String(), sha256.Sum256([]byte(value)), nil
}
