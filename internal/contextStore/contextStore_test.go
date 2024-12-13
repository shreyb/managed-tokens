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

package contextStore

import (
	"context"
	"testing"
	"time"
)

// TestGetOverrideTimeoutFromContext checks that GetOverrideTimeoutFromContext properly obtains an override timeout from a context
func TestGetOverrideTimeoutFromContext(t *testing.T) {
	ctx := context.Background()
	testTimeout := time.Duration(1 * time.Second)
	useCtx := context.WithValue(ctx, overrideTimeout, testTimeout)

	t.Run(
		"Get a timeout value from passed in context",
		func(t *testing.T) {
			timeout, _ := GetOverrideTimeout(useCtx)
			if timeout != time.Duration(1*time.Second) {
				t.Errorf("Did not get expected timeout.  Expected %s, got %s",
					time.Duration(1*time.Second),
					timeout,
				)
			}
		},
	)
}

// TestContextWithOverrideTimeout checks that ContextWithOverrideTimeout properly returns a context with an overrideTimeout stored
func TestContextWithOverrideTimeout(t *testing.T) {
	ctx := context.Background()
	t.Run(
		"Make sure we get a context with overrideTimeout set",
		func(t *testing.T) {
			newCtx := WithOverrideTimeout(ctx, time.Duration(1*time.Second))
			timeout, ok := newCtx.Value(overrideTimeout).(time.Duration)
			if !ok {
				t.Error("Wrong type saved in context.  Expected time.Duration")
			}
			if timeout != time.Duration(1*time.Second) {
				t.Errorf("Wrong time.Duration stored in context.  Expected %s, got %s",
					time.Duration(1*time.Second),
					timeout,
				)
			}
		},
	)
}

// TestGetProperTimeoutFromContext checks that GetProperTimeoutFromContext properly retrieves a time.Duration from a context with an
// overrideTimeout set, and returns the proper default or error if overrideTimeout is not set in the context
func TestGetProperTimeoutFromContext(t *testing.T) {
	var nilTimeDuration time.Duration
	ctx := context.Background()

	const badOverrideKey contextKey = 12345
	type testCase struct {
		description          string
		ctx                  context.Context
		defaultDurationStr   string
		expectedTimeDuration time.Duration
		expectedDefaultUsed  bool
		isNilError           bool
	}

	testCases := []testCase{
		{
			"Context has value set, defaultDuration proper",
			context.WithValue(ctx, overrideTimeout, time.Duration(1*time.Second)),
			"20s",
			time.Duration(1 * time.Second),
			false,
			true,
		},
		{
			"Context has no value set, defaultDuration proper",
			ctx,
			"20s",
			time.Duration(20 * time.Second),
			true,
			true,
		},
		{
			"Context has value set, defaultDuration not proper",
			context.WithValue(ctx, overrideTimeout, time.Duration(1*time.Second)),
			"12345qwerty",
			time.Duration(1 * time.Second),
			false,
			true,
		},
		{
			"Context has no value set, defaultDuration not proper",
			ctx,
			"12345qwerty",
			nilTimeDuration,
			false,
			false,
		},
		{
			"Context has value set of wrong type, defaultDuration proper",
			context.WithValue(ctx, badOverrideKey, time.Duration(1*time.Second)),
			"20s",
			time.Duration(20 * time.Second),
			true,
			true,
		},
		{
			"Context has value set of wrong type, defaultDuration not proper",
			context.WithValue(ctx, badOverrideKey, time.Duration(1*time.Second)),
			"12345qwerty",
			nilTimeDuration,
			false,
			false,
		},
	}

	for _, test := range testCases {
		t.Run(
			test.description,
			func(t *testing.T) {

				timeout, defaultUsed, err := GetProperTimeout(test.ctx, test.defaultDurationStr)
				if timeout != test.expectedTimeDuration {
					t.Errorf(
						"Got the wrong timeout from context.  Expected %s, got %s",
						test.expectedTimeDuration,
						timeout,
					)
				}
				if defaultUsed != test.expectedDefaultUsed {
					t.Errorf(
						"Got the wrong defaultUsed value.  Expected %t, got %t",
						test.expectedDefaultUsed,
						defaultUsed,
					)
				}
				if (err != nil) && test.isNilError {
					t.Errorf("Expected nil error from test.  Got %s", err)
				}
			},
		)
	}
}

// TestGetVerbose checks that GetVerbose properly retrieves the verbose value from a context
func TestGetVerbose(t *testing.T) {
	ctx := context.Background()
	useCtx := context.WithValue(ctx, verbose, true)

	t.Run(
		"Get a verbose value from passed in context",
		func(t *testing.T) {
			verboseVal, err := GetVerbose(useCtx)
			if err != nil {
				t.Errorf("Unexpected error: %s", err)
			}
			if !verboseVal {
				t.Errorf("Did not get expected verbose value. Expected true, got %t", verboseVal)
			}
		},
	)

	t.Run(
		"Get a verbose value from context with no verbose key",
		func(t *testing.T) {
			verboseVal, err := GetVerbose(ctx)
			if err != ErrContextKeyNotStored {
				t.Errorf("Expected error: %s, got: %s", ErrContextKeyNotStored, err)
			}
			if verboseVal {
				t.Errorf("Expected verbose value to be false, got %t", verboseVal)
			}
		},
	)

	t.Run(
		"Get a verbose value from context with wrong type",
		func(t *testing.T) {
			wrongCtx := context.WithValue(ctx, verbose, "not a bool")
			verboseVal, err := GetVerbose(wrongCtx)
			if err != ErrContextKeyFailedTypeCheck {
				t.Errorf("Expected error: %s, got: %s", ErrContextKeyFailedTypeCheck, err)
			}
			if verboseVal {
				t.Errorf("Expected verbose value to be false, got %t", verboseVal)
			}
		},
	)
}

// TestWithVerbose checks that WithVerbose properly sets a verbose value in a context
func TestWithVerbose(t *testing.T) {
	ctx := context.Background()
	newCtx := WithVerbose(ctx)

	t.Run(
		"Make sure we get a context with verbose set",
		func(t *testing.T) {
			verboseVal, ok := newCtx.Value(verbose).(bool)
			if !ok {
				t.Error("Wrong type saved in context. Expected bool")
			}
			if !verboseVal {
				t.Errorf("Expected verbose value to be true, got %t", verboseVal)
			}
		},
	)
}
