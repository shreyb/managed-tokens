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
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	log "github.com/sirupsen/logrus"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"

	"github.com/fermitools/managed-tokens/internal/metrics"
	"github.com/fermitools/managed-tokens/internal/tracing"
)

// Metrics
var (
	serviceErrorNotificationAttemptTimestamp = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "managed_tokens",
		Name:      "service_error_email_last_sent_timestamp",
		Help:      "Last time managed tokens service attempted to send an service error notification",
	},
		[]string{
			"service",
			"success",
		},
	)
	serviceErrorNotificationSendDuration = prometheus.NewHistogram(prometheus.HistogramOpts{
		Namespace: "managed_tokens",
		Name:      "service_error_email_send_duration_seconds",
		Help:      "Time in seconds it took to successfully send an service error email",
	})
)

func init() {
	metrics.MetricsRegistry.MustRegister(serviceErrorNotificationAttemptTimestamp)
	metrics.MetricsRegistry.MustRegister(serviceErrorNotificationSendDuration)
}

// ServiceEmailManager contains all the information needed to receive Notifications for services and ensure they get sent in the
// correct email
type ServiceEmailManager struct {
	ReceiveChan chan Notification
	Service     string
	Email       SendMessager
	// AdminNotificationsManager is a pointer to an AdminNotificationManager that carries with it, among other things, the db.ManagedTokensDatabase
	// that the ServiceEmailManager should read from and write to
	*AdminNotificationManager
	// adminNotificationChannel  is a channel on which messages can be sent to the type's AdminNotificationManager
	adminNotificationChan chan<- SourceNotification
	NotificationMinimum   int
	wg                    *sync.WaitGroup
	trackErrorCounts      bool
	errorCounts           *serviceErrorCounts
}

// NewServiceEmailManager returns an EmailManager channel for callers to send Notifications on.  It will collect messages and sort them according
// to the underlying type of the Notification, and when EmailManager is closed, will send emails.  Set up the ManagedTokensDatabase and
// the NotificationMinimum via EmailManagerOptions passed in.  If a ManagedTokensDatabase is not passed in via an EmailManagerOption,
// then the EmailManager will send all notifications
func NewServiceEmailManager(ctx context.Context, wg *sync.WaitGroup, service string, e *email, opts ...ServiceEmailManagerOption) *ServiceEmailManager {
	ctx, span := otel.GetTracerProvider().Tracer("managed-tokens").Start(ctx, "NewServiceEmailManager")
	span.SetAttributes(attribute.KeyValue{Key: "service", Value: attribute.StringValue(service)})
	defer span.End()

	funcLogger := log.WithFields(log.Fields{
		"caller":  "notifications.NewServiceEmailManager",
		"service": service,
	})

	em := &ServiceEmailManager{
		Service:     service,
		Email:       e,
		ReceiveChan: make(chan Notification),
		wg:          wg,
	}
	for _, opt := range opts {
		emBackup := backupServiceEmailManager(em)
		if err := opt(em); err != nil {
			funcLogger.Errorf("Error running functional option")
			em = emBackup
		}
	}

	if em.AdminNotificationManager == nil {
		em.AdminNotificationManager = NewAdminNotificationManager(ctx)
	}

	var err error
	shouldTrackErrorCounts := true
	em.errorCounts, err = setErrorCountsByService(ctx, em.Service, em.AdminNotificationManager.Database) // Get our previous error information for this service
	if err != nil {
		funcLogger.Error("Error setting error counts.  Will not track errors.")
		shouldTrackErrorCounts = false
	}
	em.trackErrorCounts = shouldTrackErrorCounts
	if em.trackErrorCounts {
		funcLogger.Debug("Tracking Error Counts in ServiceEmailManager")
	} else {
		funcLogger.Debug("Not tracking Error counts in ServiceEmailManager")
	}

	em.adminNotificationChan = em.AdminNotificationManager.RegisterNotificationSource(ctx)
	em.runServiceNotificationHandler(ctx)

	return em
}

func backupServiceEmailManager(s1 *ServiceEmailManager) *ServiceEmailManager {
	s2 := new(ServiceEmailManager)

	s2.ReceiveChan = make(chan Notification)
	s2.adminNotificationChan = make(chan<- SourceNotification)

	s2.Service = s1.Service
	s2.Email = s1.Email
	s2.AdminNotificationManager = s1.AdminNotificationManager
	s2.NotificationMinimum = s1.NotificationMinimum
	s2.wg = s1.wg
	s2.trackErrorCounts = s1.trackErrorCounts
	s2.errorCounts = s1.errorCounts

	return s2
}

// runServiceNotificationHandler concurrently handles the routing and counting of errors that result from a Notification being sent
// on the ServiceEmailManager's ReceiveChan.
func (em *ServiceEmailManager) runServiceNotificationHandler(ctx context.Context) {
	ctx, span := otel.GetTracerProvider().Tracer("managed-tokens").Start(ctx, "notifications.ServiceEmailManager.runServiceNotificationHandler")
	span.SetAttributes(attribute.String("service", em.Service))
	defer span.End()

	funcLogger := log.WithFields(log.Fields{
		"caller":  "notifications.runServiceNotificationHandler",
		"service": em.Service,
	})

	// Start listening for new notifications
	go func() {
		defer em.wg.Done()
		defer close(em.adminNotificationChan)

		ctx, span := otel.GetTracerProvider().Tracer("managed-tokens").Start(ctx, "notifications.ServiceEmailManager.runServiceNotificationHandler_anonFunc")
		defer span.End()

		serviceErrorsTable := make(map[string]string, 0)
		for {
			select {
			case <-ctx.Done():
				err := fmt.Errorf("serviceNotificationHandler error: %w", ctx.Err())
				tracing.LogErrorWithTrace(span, err)
				return
			case n, chanOpen := <-em.ReceiveChan:
				// Channel is closed --> save errors to database and send notifications
				if !chanOpen {
					if em.trackErrorCounts {
						if err := saveErrorCountsInDatabase(ctx, em.Service, em.Database, em.errorCounts); err != nil {
							funcLogger.Error("Error saving new error counts in database.  Please investigate")
						}
					}
					sendServiceEmailIfErrors(ctx, serviceErrorsTable, em)
					return
				}

				// Channel is open: direct the message as needed
				funcLogger.WithField("message", n.GetMessage()).Debug("Received notification message")
				shouldSend := true
				if em.trackErrorCounts {
					shouldSend = adjustErrorCountsByServiceAndDirectNotification(n, em.errorCounts, em.NotificationMinimum)
					if !shouldSend {
						log.WithField("service", n.GetService()).Debug("Error count less than error limit.  Not sending notification")
						continue
					}
				}
				if shouldSend {
					addPushErrorNotificationToServiceErrorsTable(n, serviceErrorsTable)
					em.adminNotificationChan <- SourceNotification{n}
				}
			}
		}
	}()
}

func addPushErrorNotificationToServiceErrorsTable(n Notification, serviceErrorsTable map[string]string) {
	// Note that we ONLY send push errors to the stakeholders.  Only admins will get all Notifications.
	funcLogger := log.WithFields(log.Fields{
		"caller":  "notifications.addPushErrorNotificationToServiceErrorsTable",
		"service": n.GetService(),
	})

	msg := "Error counts either not tracked or exceeded error limit.  Sending notification"
	if nValue, ok := n.(*pushError); ok {
		serviceErrorsTable[nValue.node] = n.GetMessage()
		funcLogger.WithField("node", nValue.node).Debug(msg)
		return
	}
	funcLogger.Debug(msg)
}

// TODO - if this fails, we need to know somehow
func sendServiceEmailIfErrors(ctx context.Context, serviceErrorsTable map[string]string, em *ServiceEmailManager) {
	ctx, span := otel.GetTracerProvider().Tracer("managed-tokens").Start(ctx, "notifications.sendServiceEmailIfErrors")
	span.SetAttributes(attribute.String("service", em.Service))
	defer span.End()

	funcLogger := log.WithFields(log.Fields{
		"caller":  "notifications.sendServiceEmailIfErrors",
		"service": em.Service,
	})

	if len(serviceErrorsTable) == 0 {
		funcLogger.Debug("No errors to send for service")
		return
	}

	tableString := PrepareTableStringFromMap(
		serviceErrorsTable,
		"The following is a list of nodes on which all vault tokens were not refreshed, and the corresponding roles for those failed token refreshes:",
		[]string{"Node", "Error"},
	)
	msg, err := prepareServiceEmail(tableString)
	if err != nil {
		err = fmt.Errorf("Error preparing service email for sending: %w", err)
		tracing.LogErrorWithTrace(span, err)
	}

	start := time.Now()
	var success bool
	err = SendMessage(ctx, em.Email, msg)
	dur := time.Since(start).Seconds()
	if err != nil {
		err = fmt.Errorf("Error sending service email: %w", err)
		tracing.LogErrorWithTrace(span, err)
		return
	}
	serviceErrorNotificationSendDuration.Observe(dur)
	success = true
	span.SetStatus(codes.Ok, "Email sent successfully")
	serviceErrorNotificationAttemptTimestamp.WithLabelValues(em.Service, strconv.FormatBool(success)).SetToCurrentTime()
}

// prepareServiceEmail returns a string that contains email text according to the passed in errorTable
func prepareServiceEmail(errorTable string) (string, error) {
	timestamp := time.Now().Format(time.RFC822)
	templateStruct := struct {
		Timestamp  string
		ErrorTable string
	}{
		Timestamp:  timestamp,
		ErrorTable: errorTable,
	}
	return prepareMessageFromTemplate(strings.NewReader(serviceErrorsTemplate), templateStruct)
}
