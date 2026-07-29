// Package ext exposes the contract types and sentinels that edition
// extensions (e.g. otel-magnify-enterprise) consume.
package ext

import "errors"

// ErrUserNotFound is returned (typically wrapped) by user-lookup methods
// on Store when no user matches the lookup. Consumers compare against
// this with errors.Is rather than substring matching on error strings.
var ErrUserNotFound = errors.New("user not found")

// ErrInvalidOpAMPToken is returned for unknown, mismatched, expired, or revoked managed OpAMP tokens.
var ErrInvalidOpAMPToken = errors.New("invalid OpAMP token")

// ErrOpAMPTokenNotFound is returned when an administrative token lookup has no match.
var ErrOpAMPTokenNotFound = errors.New("OpAMP token not found")

// ErrCommitOutcomeUnknown indicates that a transaction commit failed after its
// durable outcome could no longer be established by the caller.
var ErrCommitOutcomeUnknown = errors.New("transaction commit outcome unknown")
