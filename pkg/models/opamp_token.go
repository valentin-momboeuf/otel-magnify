package models

import "time"

// OpAMPTokenStatus describes whether a managed OpAMP token can authenticate.
type OpAMPTokenStatus string

// OpAMPTokenActive and the other statuses describe the managed token lifecycle.
const (
	OpAMPTokenActive  OpAMPTokenStatus = "active"
	OpAMPTokenExpired OpAMPTokenStatus = "expired"
	OpAMPTokenRevoked OpAMPTokenStatus = "revoked"
)

// OpAMPToken contains public metadata and lifecycle state for a managed token.
type OpAMPToken struct {
	ID          string           `json:"id"`
	Name        string           `json:"name"`
	Description string           `json:"description"`
	Team        string           `json:"team"`
	Environment string           `json:"environment"`
	CreatedAt   time.Time        `json:"created_at"`
	CreatedBy   string           `json:"created_by"`
	ExpiresAt   *time.Time       `json:"expires_at,omitempty"`
	LastUsedAt  *time.Time       `json:"last_used_at,omitempty"`
	RevokedAt   *time.Time       `json:"revoked_at,omitempty"`
	RevokedBy   string           `json:"revoked_by,omitempty"`
	Status      OpAMPTokenStatus `json:"status"`
}

// OpAMPTokenCredential combines public token metadata with its non-serializable digest.
type OpAMPTokenCredential struct {
	Token      OpAMPToken
	SecretHash [32]byte `json:"-"`
}

// OpAMPTokenPrincipal identifies the managed token authenticated on an OpAMP connection.
type OpAMPTokenPrincipal struct {
	ID        string
	ExpiresAt *time.Time
}
