package utils

import (
	"context"
	"time"

	log "github.com/sirupsen/logrus"
)

// contextKey allows for an extensible set of context keys to be used by all packages to add strongly-typed values to contexts.  Each context key
// will have a declaration in the const clause below, and should provide a Get_ func and a func to return a new context with the key set to an
// appropriate value
// Thanks to https://go.dev/blog/context#package-userip and https://stackoverflow.com/a/40891417 for inspiration
type contextKey int

const (
	verbose contextKey = iota
	overrideTimeout
)

// GetVerboseFromContext returns the value of the verbose value from a context if it has been stored.  It also type-checks the value to ensure
// it was stored properly
func GetVerboseFromContext(ctx context.Context) (bool, bool) {
	verbose, ok := ctx.Value(verbose).(bool)
	return verbose, ok
}

// ContextWithVerbose wraps a context with a verbose=true value
func ContextWithVerbose(ctx context.Context) context.Context {
	return context.WithValue(ctx, verbose, true)
}

// GetOverrideTimeoutFromContext returns the override timeout value from a context if it's been stored and type-checks it
func GetOverrideTimeoutFromContext(ctx context.Context) (time.Duration, bool) {
	timeout, ok := ctx.Value(overrideTimeout).(time.Duration)
	return timeout, ok
}

// ContextWithOverrideTimeout takes a parent context and returns a new context with an override timeout set
func ContextWithOverrideTimeout(ctx context.Context, timeout time.Duration) context.Context {
	return context.WithValue(ctx, overrideTimeout, timeout)
}

// GetProperTimeoutFromContext takes a constant and a default duration, and tries to see if the context is holding an overrideTimeout key.
// If it finds an overrideTimeout key, it returns the corresponding time.Duration. Otherwise, it will parse the defaultDuration string and
// return a time.Duration from that, if possible.
func GetProperTimeoutFromContext(ctx context.Context, defaultDuration string) (time.Duration, error) {
	var useTimeout time.Duration
	var ok bool
	var err error

	if useTimeout, ok = GetOverrideTimeoutFromContext(ctx); !ok {
		log.Debug("No overrideTimeout set.  Will use default")
		useTimeout, err = time.ParseDuration(defaultDuration)
		if err != nil {
			log.Error("Could not parse default timeout duration")
			return useTimeout, err
		}
	}
	return useTimeout, nil
}
