package utils

import (
	"context"
	"errors"
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

var (
	ErrContextKeyFailedTypeCheck error = errors.New("returned value failed type-check")
	ErrContextKeyNotStored             = errors.New("no value in Context for contextKey")
)

// GetVerboseFromContext returns the type-checked value of the verbose contextKey from a context if it has been stored. If there was an issue
// retrieving the value from the verbose contextKey in the context, an error is returned
func GetVerboseFromContext(ctx context.Context) (bool, error) {
	var verboseReturnVal, ok bool
	verboseVal := ctx.Value(verbose)
	funcLogger := log.WithField("contextKey", "verbose")
	if verboseVal == nil {
		funcLogger.Debug("No value stored for contextKey.")
		return false, ErrContextKeyNotStored
	}
	if verboseReturnVal, ok = verboseVal.(bool); !ok {
		funcLogger.Debug("contextKey value failed type check")
		return false, ErrContextKeyFailedTypeCheck
	}
	return verboseReturnVal, nil
}

// ContextWithVerbose wraps a context with a verbose=true value
func ContextWithVerbose(ctx context.Context) context.Context {
	return context.WithValue(ctx, verbose, true)
}

// GetOverrideTimeoutFromContext returns the override timeout value from a context if it's been stored and type-checks it.  If either of these
// operations fail, it returns an error
func GetOverrideTimeoutFromContext(ctx context.Context) (time.Duration, error) {
	var timeout time.Duration
	var ok bool
	funcLogger := log.WithField("contextKey", "overrideTimeout")
	timeoutVal := ctx.Value(overrideTimeout)
	if timeoutVal == nil {
		funcLogger.Debug("No value stored for contextKey")
		return timeout, ErrContextKeyNotStored
	}
	if timeout, ok = timeoutVal.(time.Duration); !ok {
		funcLogger.Debug("contextKey value failed type check")
		return timeout, ErrContextKeyFailedTypeCheck
	}
	return timeout, nil
}

// ContextWithOverrideTimeout takes a parent context and returns a new context with an override timeout set
func ContextWithOverrideTimeout(ctx context.Context, timeout time.Duration) context.Context {
	return context.WithValue(ctx, overrideTimeout, timeout)
}

// GetProperTimeoutFromContext takes a context and a default duration, and tries to see if the context is holding an overrideTimeout key.
// If it finds an overrideTimeout key, it returns the corresponding time.Duration. Otherwise, it will parse the defaultDuration string and
// return a time.Duration from that, if possible.
func GetProperTimeoutFromContext(ctx context.Context, defaultDuration string) (time.Duration, error) {
	getDefaultTimeout := func() (time.Duration, error) {
		timeout, err := time.ParseDuration(defaultDuration)
		if err != nil {
			log.Error("Could not parse default timeout duration")
		}
		return timeout, err
	}

	var useTimeout time.Duration
	var err error

	useTimeout, err = GetOverrideTimeoutFromContext(ctx)
	if err != nil {
		switch {
		case errors.Is(err, ErrContextKeyNotStored):
			log.Debug("No overrideTimeout set.  Will use default")
		case errors.Is(err, ErrContextKeyFailedTypeCheck):
			log.Debug("Stored overrideTimeout failed type check.  Will use default")
		default:
			log.Error("Unspecified error getting overrideTimeout from context.  Will use default")
		}
		return getDefaultTimeout()
	}
	return useTimeout, nil
}
