package notifications

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	log "github.com/sirupsen/logrus"
)

// slackMessage is a Slack message configuration that consists of the url endpoint to which the message data should be POSTed via HTTP.
// Using a slackMessage assumes that a slack webhook or other HTTP POST API has been enabled on an existing slack channel
type slackMessage struct {
	url string
}

func (s *slackMessage) From() string   { return "" }
func (s *slackMessage) To() []string   { return []string{s.url} }
func (s *slackMessage) SetFrom() error { return nil }
func (s *slackMessage) SetTo(recipient []string) error {
	if len(recipient) > 1 {
		return errors.New("slackMessage does not support more than one recipient URL")
	}
	s.url = recipient[0]
	return nil
}

// NewSlackMessage returns a configured *slackMessage that can be used to send a message using SendMessage()
func NewSlackMessage(url string) *slackMessage {
	return &slackMessage{
		url: url,
	}
}

// sendMessage sends message as a Slack message by sending an HTTP POST request to the value of the url field of the
// slackMessage.
func (s *slackMessage) sendMessage(ctx context.Context, message string) error {
	if e := ctx.Err(); e != nil {
		log.Errorf("Error sending slack message: %s", e)
		return e
	}

	if message == "" {
		log.Warn("Slack message is empty.  Will not attempt to send it")
		return nil
	}

	msg := []byte(fmt.Sprintf(`{"text": "%s"}`, strings.Replace(message, "\"", "\\\"", -1)))
	req, err := http.NewRequest("POST", s.url, bytes.NewBuffer(msg))
	if err != nil {
		log.Errorf("Error sending slack message: %s", err)
		return err
	}

	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(ctx)

	client := http.DefaultClient
	resp, err := client.Do(req)
	if err != nil {
		log.Errorf("Error sending slack message: %s", err)
		return err
	}

	// This should be redundant, but just in case the timeout before didn't trigger.
	if e := ctx.Err(); e != nil {
		log.Errorf("Error sending slack message: %s", e)
		return e
	}

	defer resp.Body.Close()

	// Parse the response to make sure we're good
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		err := errors.New("could not send slack message")
		log.WithFields(log.Fields{
			"url":              s.url,
			"response status":  resp.Status,
			"response headers": resp.Header,
			"response body":    string(body),
		}).Error(err)
		return err
	}
	log.Debug("Slack message sent")
	return nil
}
