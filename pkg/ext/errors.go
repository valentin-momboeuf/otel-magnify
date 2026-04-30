// Package ext exposes the contract types and sentinels that edition
// extensions (e.g. otel-magnify-enterprise) consume.
package ext

import "errors"

// ErrUserNotFound is returned (typically wrapped) by user-lookup methods
// on Store when no user matches the lookup. Consumers compare against
// this with errors.Is rather than substring matching on error strings.
var ErrUserNotFound = errors.New("user not found")
