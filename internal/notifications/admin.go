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
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	log "github.com/sirupsen/logrus"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"golang.org/x/sync/errgroup"

	"github.com/fermitools/managed-tokens/internal/metrics"
	"github.com/fermitools/managed-tokens/internal/tracing"
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

// packageErrors is a concurrent-safe struct that holds information about all the errors encountered while running package funcs and methods
// It also includes a sync.Mutex and a sync.WaitGroup to coordinate data access
type packageErrors struct {
	errorsMap   *sync.Map      // Holds all the errors accumulated for the current invocation.  Roughly a map[service string]*adminData
	writerCount sync.WaitGroup // WaitGroup to be incremented anytime a function wants to write to a packageErrors
	mu          sync.Mutex
}

// SendAdminNotifications sends admin messages via email and Slack that have been collected in adminErrors. It expects a valid template file
// configured at adminTemplatePath
func SendAdminNotifications(ctx context.Context, operation string, isTest bool, sendMessagers ...SendMessager) error {
	ctx, span := tracer.Start(ctx, "SendAdminNotifications")
	span.SetAttributes(attribute.String("operation", operation))
	defer span.End()

	funcLogger := log.WithField("caller", "notifications.SendAdminNotifications")

	// Let all adminErrors writers finish updating adminErrors
	adminErrors.writerCount.Wait()
	funcLogger.Debug("adminErrors is finalized")

	// No errors - only send slack message saying we tested.  If there are no errors, we don't send emails
	if adminErrorsIsEmpty() {
		if isTest {
			if err := sendSlackNoErrorTestMessages(ctx, sendMessagers); err != nil {
				err = fmt.Errorf("error sending admin notifications saying there were no errors in test mode: %w", err)
				tracing.LogErrorWithTrace(span, err)
				return err
			}
		}
		span.SetStatus(codes.Ok, "No errors to send")
		funcLogger.Debug("No errors to send")
		return nil
	}

	fullMessage, abridgedMessage, err := prepareFullAndAbridgedMessages(operation)
	if err != nil {
		err = fmt.Errorf("error preparing full and abridged messages: %w", err)
		tracing.LogErrorWithTrace(span, err)
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
		err = errors.New("sending admin notifications failed.  Please see logs")
		tracing.LogErrorWithTrace(span, err)
		return err
	}

	span.SetStatus(codes.Ok, "Admin notifications sent")
	return nil
}

func sendSlackNoErrorTestMessages(ctx context.Context, sendMessagers []SendMessager) error {
	ctx, span := tracer.Start(ctx, "sendSlackNoErrorTestMessages")
	defer span.End()

	funcLogger := log.WithField("caller", "sendSlackNoErrorTestMessages")

	slackMessages := make([]*slackMessage, 0)
	slackMsgText := "Test run completed successfully"
	for _, sm := range sendMessagers {
		if messager, ok := sm.(*slackMessage); ok {
			slackMessages = append(slackMessages, messager)
		}
	}
	for _, slackMessage := range slackMessages {
		if err := SendMessage(ctx, slackMessage, slackMsgText); err != nil {
			err = fmt.Errorf("error sending slack message: %w", err)
			funcLogger.Error(err.Error())
			tracing.LogErrorWithTrace(span, err)
			return err
		}
	}
	span.SetStatus(codes.Ok, "Slack messages sent")
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
