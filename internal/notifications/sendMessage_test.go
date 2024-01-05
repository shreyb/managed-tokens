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
	"errors"
	"reflect"
	"testing"
)

type fakeSender struct {
	err error
}

func (f *fakeSender) sendMessage(ctx context.Context, msg string) error {
	return f.err
}

// TestSendMessage checks that SendMessage properly wraps a SendMessager's sendMessage method
func TestSendMessage(t *testing.T) {
	tests := []struct {
		description string
		s           SendMessager
		err         error
	}{
		{
			description: "Failure to send for some reason",
			s:           &fakeSender{errors.New("This failed for some reason")},
			err:         &SendMessageError{"Could not get a new grid proxy from gridProxyer"},
		},
		{
			description: "Successful send of message",
			s:           &fakeSender{err: nil},
			err:         nil,
		},
	}

	ctx := context.Background()

	for _, test := range tests {
		t.Run(test.description, func(t *testing.T) {
			err := SendMessage(ctx, test.s, "")
			if reflect.TypeOf(err) != reflect.TypeOf(test.err) {
				t.Errorf("SendMessage test should have returned %T; got %T instead", test.err, err)
			}
		},
		)
	}

}
