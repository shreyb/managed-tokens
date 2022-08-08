package utils

import (
	"context"
	"time"
)

// contextKey.go allows for an extensible set of context keys to be used by all packages to add strongly-typed values to contexts.  Each context key
// will have a declaration in the const clause below, and should provide a Get_ func and a func to return a new context with the key set to an
// appropriate value

type contextKey int

const (
	overrideTimeout contextKey = iota
)

// GetOverrideTimeoutFromContext returns the override timeout value from a context if it's been stored and type-checks it
func GetOverrideTimeoutFromContext(ctx context.Context) (time.Duration, bool) {
	timeout, ok := ctx.Value(overrideTimeout).(time.Duration)
	return timeout, ok
}

// ContextWithOverrideTimeout takes a parent context and returns a new context with an override timeout set
func ContextWithOverrideTimeout(ctx context.Context, timeout time.Duration) context.Context {
	return context.WithValue(ctx, overrideTimeout, timeout)
}
