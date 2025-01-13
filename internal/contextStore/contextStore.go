// COPYRIGHT 2024 FERMI NATIONAL ACCELERATOR LABORATORY
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
//
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// package contextStore provides a set of functions and types that allow for strongly-typed values to be stored in contexts, and to
// be safely retrieved later

package contextStore

import (
	"context"
	"errors"
	"fmt"
	"time"
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
	// ErrorContextKeyFailedTypeCheck is returned when a value retrieved from a context fails a type-check
	ErrContextKeyFailedTypeCheck error = errors.New("returned value failed type-check")
	// ErrorContextKeyNotStored is returned when a requested key is not stored in a context
	ErrContextKeyNotStored = errors.New("no value in Context for contextKey")
)

// GetVerbose returns the type-checked value of the verbose contextKey from a context if it has been stored. If there was an issue
// retrieving the value from the verbose contextKey in the context, an error is returned
func GetVerbose(ctx context.Context) (bool, error) {
	var verboseReturnVal, ok bool
	verboseVal := ctx.Value(verbose)
	if verboseVal == nil {
		return false, ErrContextKeyNotStored
	}
	if verboseReturnVal, ok = verboseVal.(bool); !ok {
		return false, ErrContextKeyFailedTypeCheck
	}
	return verboseReturnVal, nil
}

// WithVerbose wraps a context with a verbose=true value
func WithVerbose(ctx context.Context) context.Context {
	return context.WithValue(ctx, verbose, true)
}

// GetOverrideTimeout returns the override timeout value from a context if it's been stored and type-checks it.  If either of these
// operations fail, it returns an error
func GetOverrideTimeout(ctx context.Context) (time.Duration, error) {
	var timeout time.Duration
	var ok bool
	timeoutVal := ctx.Value(overrideTimeout)
	if timeoutVal == nil {
		return timeout, ErrContextKeyNotStored
	}
	if timeout, ok = timeoutVal.(time.Duration); !ok {
		return timeout, ErrContextKeyFailedTypeCheck
	}
	return timeout, nil
}

// WithOverrideTimeout takes a parent context and returns a new context with an override timeout set
func WithOverrideTimeout(ctx context.Context, timeout time.Duration) context.Context {
	return context.WithValue(ctx, overrideTimeout, timeout)
}

// GetProperTimeoutFromContext takes a context and a default duration, and tries to see if the context is holding an overrideTimeout key.
// If it finds an overrideTimeout key, it returns the corresponding time.Duration. Otherwise, it will parse the defaultDuration string and
// return a time.Duration from that, if possible.  If the latter happens, the returned bool will be set to true, indicating that a the
// passed in default was used.
func GetProperTimeout(ctx context.Context, defaultDuration string) (timeout time.Duration, defaultUsed bool, err error) {
	getDefaultTimeout := func() (time.Duration, error) {
		_timeout, err := time.ParseDuration(defaultDuration)
		if err != nil {
			return _timeout, fmt.Errorf("could not parse default timeout duration: %w", err)
		}
		return _timeout, nil
	}

	if timeout, err = GetOverrideTimeout(ctx); err != nil {
		defaultTimeout, err2 := getDefaultTimeout()
		if err2 != nil {
			return 0, false, err2
		}
		return defaultTimeout, true, nil
	}
	return timeout, false, nil
}
