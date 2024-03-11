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

package notifications

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestNewEmail(t *testing.T) {
	e := &email{
		from:     "from@address",
		to:       []string{"toemail@address1", "toemail@address2"},
		subject:  "test subject",
		smtpHost: "smtp.host",
		smtpPort: 42,
	}
	assert.Equal(t, *e, *NewEmail("from@address", []string{"toemail@address1", "toemail@address2"}, "test subject", "smtp.host", 42))
}

func TestSendMessageEmailContextErrors(t *testing.T) {
	type testCase struct {
		description      string
		contextSetupFunc func() (context.Context, context.CancelFunc)
		expectedErr      error
	}

	testCases := []testCase{
		{
			"Canceled context",
			func() (context.Context, context.CancelFunc) {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				cancel()
				return ctx, cancel
			},
			context.Canceled,
		},
		{
			"Context deadline exceeded",
			func() (context.Context, context.CancelFunc) {
				ctx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
				time.Sleep(1 * time.Nanosecond)
				return ctx, cancel
			},
			context.DeadlineExceeded,
		},
	}

	e := &email{
		from:     "from@address",
		to:       []string{"toemail@address1", "toemail@address2"},
		subject:  "test subject",
		smtpHost: "smtp.host",
		smtpPort: 42,
	}

	for _, test := range testCases {
		t.Run(
			test.description,
			func(t *testing.T) {
				ctx, cancel := test.contextSetupFunc()
				t.Cleanup(func() { cancel() })
				assert.ErrorIs(t, test.expectedErr, e.sendMessage(ctx, "This is a message"))
			},
		)
	}
}

func TestSendMessageEmailError(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(func() {
		cancel()
	})

	e := &email{
		from:     "from@address",
		to:       []string{"toemail@address1", "toemail@address2"},
		subject:  "test subject",
		smtpHost: "badhost",
		smtpPort: 42,
	}

	// Assert that we got an error
	assert.NotNil(t, e.sendMessage(ctx, "This is a message"))
}
