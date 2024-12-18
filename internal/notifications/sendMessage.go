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

	"go.opentelemetry.io/otel"

	"github.com/fermitools/managed-tokens/internal/tracing"
)

// SendMessager wraps the SendMessage method
type SendMessager interface {
	sendMessage(ctx context.Context, message string) error
}

// SendMessageError indicates that an error occurred sending a message
type SendMessageError struct {
	underlying error
}

func (s *SendMessageError) Error() string {
	return "error sending message: " + s.underlying.Error()
}

func (s *SendMessageError) Unwrap() error {
	return s.underlying
}

// SendMessage sends a message (msg).  The kind of message and how that message is sent is determined
// by the SendMessager, which should be configured before passing into SendMessage
func SendMessage(ctx context.Context, s SendMessager, msg string) error {
	ctx, span := otel.GetTracerProvider().Tracer("managed-tokens").Start(ctx, "notifications.SendMessage")
	defer span.End()

	err := s.sendMessage(ctx, msg)
	if err != nil {
		err = &SendMessageError{err}
		tracing.LogErrorWithTrace(span, err)
		return err
	}
	tracing.LogSuccessWithTrace(span, "Message sent successfully")
	return nil
}
