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
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	log "github.com/sirupsen/logrus"
	"golang.org/x/sync/errgroup"

	"github.com/fermitools/managed-tokens/internal/metrics"
)

// Metrics
var (
	adminErrorNotificationAttemptTimestamp = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "managed_tokens",
		Name:      "admin_error_email_last_sent_timestamp",
		Help:      "Last time managed tokens service attempted to send an admin error notification",
	},
		[]string{
			"notification_type",
			"success",
		},
	)
	adminErrorNotificationSendDuration = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "managed_tokens",
		Name:      "admin_error_email_send_duration_seconds",
		Help:      "Time in seconds it took to successfully send an admin error email",
	},
		[]string{
			"notification_type",
		},
	)
)

func init() {
	metrics.MetricsRegistry.MustRegister(adminErrorNotificationAttemptTimestamp)
	metrics.MetricsRegistry.MustRegister(adminErrorNotificationSendDuration)
}

// For Admin notifications, we expect caller to instantiate admin email object, admin slackMessage object, and pass them to SendAdminNotifications.

// runAdminNotificationHandler handles the routing and counting of errors that result from a
// Notification being sent on the AdminNotificationManager's ReceiveChan
func runAdminNotificationHandler(ctx context.Context, a *AdminNotificationManager, allServiceCounts map[string]*serviceErrorCounts) {
	funcLogger := log.WithField("caller", "runAdminNotificationHandler")

	go func() {
		defer close(a.adminErrorChan)
		for {
			select {
			case <-ctx.Done():
				if err := ctx.Err(); err == context.DeadlineExceeded {
					funcLogger.Error("Timeout exceeded in notification Manager")
				} else {
					funcLogger.Error(err)
				}
				return

			case n, chanOpen := <-a.ReceiveChan:
				// Channel is closed --> send notifications
				if !chanOpen {
					// Only save error counts if we expect no other NotificationsManagers (like ServiceEmailManager) to write to the database
					if a.TrackErrorCounts && !a.DatabaseReadOnly {
						for service, ec := range allServiceCounts {
							if err := saveErrorCountsInDatabase(ctx, service, a.Database, ec); err != nil {
								funcLogger.WithField("service", n.GetService()).Error("Error saving new error counts in database.  Please investigate")
							}
						}
					}
					return
				} else {
					// Send notification to admin message aggregator
					shouldSend := true
					if a.TrackErrorCounts {
						shouldSend = adjustErrorCountsByServiceAndDirectNotification(n, allServiceCounts[n.GetService()], a.NotificationMinimum)
						if !shouldSend {
							funcLogger.Debug("Error count less than error limit.  Not sending notification.")
							continue
						}
					}
					if shouldSend {
						a.adminErrorChan <- n
					}
				}
			}
		}
	}()
}

// SendAdminNotifications sends admin messages via email and Slack that have been collected in adminErrors. It expects a valid template file
// configured at adminTemplatePath
func SendAdminNotifications(ctx context.Context, operation string, isTest bool, sendMessagers ...SendMessager) error {
	funcLogger := log.WithField("caller", "notifications.SendAdminNotifications")

	// Let all adminErrors writers finish updating adminErrors
	adminErrors.writerCount.Wait()
	funcLogger.Debug("adminErrors is finalized")

	// No errors - only send slack message saying we tested.  If there are no errors, we don't send emails
	if adminErrorsIsEmpty() {
		if isTest {
			if err := sendSlackNoErrorTestMessages(ctx, sendMessagers); err != nil {
				funcLogger.Error("Error sending admin notifications saying there were no errors in test mode")
				return err
			}
		}
		funcLogger.Debug("No errors to send")
		return nil
	}

	fullMessage, abridgedMessage, err := prepareFullAndAbridgedMessages(operation)
	if err != nil {
		funcLogger.Errorf("Could not prepare full or abridged admin message from template: %s", err)
		return err
	}

	// Run SendMessage on all configured sendMessagers
	g := new(errgroup.Group)
	for _, sm := range sendMessagers {
		sm := sm
		g.Go(func() error {
			var messageToSend, logEnding, metricLabel string
			var successForMetric bool
			switch sm.(type) {
			case *email:
				messageToSend = fullMessage
				logEnding = "admin email"
				metricLabel = "email"
			case *slackMessage:
				messageToSend = abridgedMessage
				logEnding = "slack message"
				metricLabel = "slack_message"
			default:
				err := errors.New("unsupported SendMessager")
				funcLogger.Error(err)
				return err
			}
			start := time.Now()
			err := SendMessage(ctx, sm, messageToSend)
			dur := time.Since(start).Seconds()
			if err != nil {
				funcLogger.Errorf("Failed to send %s", logEnding)
			} else {
				funcLogger.Debugf("Sent %s", logEnding)
				successForMetric = true
				adminErrorNotificationSendDuration.WithLabelValues(metricLabel).Set(dur)
			}
			adminErrorNotificationAttemptTimestamp.WithLabelValues(metricLabel, strconv.FormatBool(successForMetric)).SetToCurrentTime()
			return err
		})
	}

	if err := g.Wait(); err != nil {
		return errors.New("sending admin notifications failed.  Please see logs")
	}

	return nil
}

func sendSlackNoErrorTestMessages(ctx context.Context, sendMessagers []SendMessager) error {
	funcLogger := log.WithField("caller", "sendSlackNoErrorTestMessages")
	slackMessages := make([]*slackMessage, 0)
	slackMsgText := "Test run completed successfully"
	for _, sm := range sendMessagers {
		if messager, ok := sm.(*slackMessage); ok {
			slackMessages = append(slackMessages, messager)
		}
	}
	for _, slackMessage := range slackMessages {
		if slackErr := SendMessage(ctx, slackMessage, slackMsgText); slackErr != nil {
			funcLogger.Error("Failed to send slack message")
			return slackErr
		}
	}
	return nil
}

// AdminDataFinal stores the same information as adminData, but with the PushErrors converted to string form, as a table.  The PushErrorsTable field
// is meant to be read as is and used directly in an admin message
type AdminDataFinal struct {
	SetupErrors     []string
	PushErrorsTable string
}

func prepareFullAndAbridgedMessages(operation string) (fullMessage string, abridgedMessage string, err error) {
	funcLogger := log.WithFields(log.Fields{
		"caller":    "notifications.prepareFullAndAbridgedMessages",
		"operation": operation,
	})
	timestamp := time.Now().Format(time.RFC822)

	// If there are errors, prepare the long-form and abridged messages
	// Prepare the full and abridged messages
	adminErrorsMapFinal := prepareAdminErrorsForFullMessage()
	fullMessageStruct := struct {
		Timestamp   string
		Operation   string
		AdminErrors map[string]AdminDataFinal
		Abridged    bool
	}{
		Timestamp:   timestamp,
		Operation:   operation,
		AdminErrors: adminErrorsMapFinal,
		Abridged:    false,
	}
	fullMessage, err = prepareMessageFromTemplate(strings.NewReader(adminErrorsTemplate), fullMessageStruct)
	if err != nil {
		funcLogger.Errorf("Could not prepare full admin message from template: %s", err)
		return "", "", err
	}

	setupErrorsCombined, pushErrorsCombined := prepareAbridgedAdminSlices()
	abridgedMessageStruct := struct {
		Timestamp           string
		Operation           string
		SetupErrorsCombined []string
		PushErrorsCombined  []string
		Abridged            bool
	}{
		Timestamp:           timestamp,
		Operation:           operation,
		SetupErrorsCombined: setupErrorsCombined,
		PushErrorsCombined:  pushErrorsCombined,
		Abridged:            true,
	}
	abridgedMessage, err = prepareMessageFromTemplate(strings.NewReader(adminErrorsTemplate), abridgedMessageStruct)
	if err != nil {
		funcLogger.Errorf("Could not prepare abridged admin message from template: %s", err)
		return "", "", err
	}
	return fullMessage, abridgedMessage, nil
}

// prepareAdminErrorsForMessage transforms the accumulated adminErrors variable from type *adminData to AdminDataFinal
func prepareAdminErrorsForFullMessage() map[string]AdminDataFinal {
	// Get our adminErrors from type map[string]*adminData to a map[string]AdminDataUnsync so it's easier to work with
	adminErrorsIntermediateMap := adminErrorsToAdminDataUnsync()
	adminErrorsMapFinal := make(map[string]AdminDataFinal)

	// Convert pushErrors map to string, save as map adminErrorsMapFinal so we get our final form.
	for service, aData := range adminErrorsIntermediateMap {
		a := AdminDataFinal{
			SetupErrors: aData.SetupErrors,
			PushErrorsTable: PrepareTableStringFromMap(
				aData.PushErrors,
				"The following is a list of nodes on which all vault tokens were not refreshed, and the corresponding roles for those failed token refreshes:",
				[]string{"Node", "Error"},
			),
		}
		adminErrorsMapFinal[service] = a
	}
	return adminErrorsMapFinal
}

// prepareAbridgedAdminSlices takes the stored adminErrors and returns two []string objects containing the various setup and push errors, not broken
// up by service.  This is for abridged messages like slack messages.
func prepareAbridgedAdminSlices() (setupErrorsCombined []string, pushErrorsCombined []string) {
	adminErrorsUnsync := adminErrorsToAdminDataUnsync()
	for service, data := range adminErrorsUnsync {
		for _, setupError := range data.SetupErrors {
			setupErrorsCombined = append(setupErrorsCombined, fmt.Sprintf("%s: %s", service, setupError))
		}
		for node, pushError := range data.PushErrors {
			pushErrorsCombined = append(pushErrorsCombined, fmt.Sprintf("%s@%s: %s", service, node, pushError))
		}
	}
	return setupErrorsCombined, pushErrorsCombined
}
