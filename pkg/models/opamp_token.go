package models

import "time"

type OpAMPTokenStatus string

const (
	OpAMPTokenActive  OpAMPTokenStatus = "active"
	OpAMPTokenExpired OpAMPTokenStatus = "expired"
	OpAMPTokenRevoked OpAMPTokenStatus = "revoked"
)

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

type OpAMPTokenCredential struct {
	Token      OpAMPToken
	SecretHash [32]byte `json:"-"`
}

type OpAMPTokenPrincipal struct {
	ID        string
	ExpiresAt *time.Time
}
