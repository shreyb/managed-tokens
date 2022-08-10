package notifications

import (
	"context"

	log "github.com/sirupsen/logrus"
)

// SendMessager wraps the SendMessage method
type SendMessager interface {
	sendMessage(ctx context.Context, message string) error
}

// SendMessageError indicates that an error occurred sending a message
type SendMessageError struct{ message string }

func (s *SendMessageError) Error() string { return s.message }

// SendMessage sends a message (msg).  The kind of message and how that message is sent is determined
// by the SendMessager, and the ConfigInfo gives supplemental information to send the message.
func SendMessage(ctx context.Context, s SendMessager, msg string) error {
	err := s.sendMessage(ctx, msg)
	if err != nil {
		err := &SendMessageError{"Error sending message"}
		log.Error(err)
		return err
	}
	return nil
}
