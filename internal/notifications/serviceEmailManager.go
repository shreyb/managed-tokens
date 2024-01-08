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
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	log "github.com/sirupsen/logrus"

	"github.com/fermitools/managed-tokens/internal/db"
	"github.com/fermitools/managed-tokens/internal/metrics"
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
	ReceiveChan         chan Notification
	Service             string
	Email               *email
	Database            *db.ManagedTokensDatabase
	NotificationMinimum int
	wg                  *sync.WaitGroup
}

type ServiceEmailManagerOption func(*ServiceEmailManager) error

// NewServiceEmailManager returns an EmailManager channel for callers to send Notifications on.  It will collect messages and sort them according
// to the underlying type of the Notification, and when EmailManager is closed, will send emails.  Set up the ManagedTokensDatabase and
// the NotificationMinimum via EmailManagerOptions passed in.  If a ManagedTokensDatabase is not passed in via an EmailManagerOption,
// then the EmailManager will send all notifications
func NewServiceEmailManager(ctx context.Context, wg *sync.WaitGroup, service string, e *email, opts ...ServiceEmailManagerOption) *ServiceEmailManager {
	funcLogger := log.WithFields(log.Fields{
		"caller":  "notifications.NewServiceEmailManager",
		"service": service,
	})

	em := &ServiceEmailManager{
		Service:     service,
		ReceiveChan: make(chan Notification),
		wg:          wg,
	}
	for _, opt := range opts {
		if err := opt(em); err != nil {
			funcLogger.Errorf("Error running functional option")
		}
	}

	shouldTrackErrorCounts := true
	ec, err := setErrorCountsByService(ctx, em.Service, em.Database) // Get our previous error information for this service
	if err != nil {
		funcLogger.Error("Error setting error counts.  Will not track errors.")
		shouldTrackErrorCounts = false
	}

	adminChan := make(chan Notification)
	startAdminErrorAdder(adminChan)
	runServiceNotificationHandler(ctx, em, adminChan, ec, shouldTrackErrorCounts)

	return em
}

// runServiceNotificationHandler concurrently handles the routing and counting of errors that result from a Notification being sent
// on the ServiceEmailManager's ReceiveChan.
func runServiceNotificationHandler(ctx context.Context, em *ServiceEmailManager, adminChan chan<- Notification, ec *serviceErrorCounts, shouldTrackErrorCounts bool) {
	funcLogger := log.WithFields(log.Fields{
		"caller":  "notifications.runServiceNotificationHandler",
		"service": em.Service,
	})

	// Start listening for new notifications
	go func() {
		serviceErrorsTable := make(map[string]string, 0)
		defer em.wg.Done()
		defer close(adminChan)
		for {
			select {
			case <-ctx.Done():
				if err := ctx.Err(); err == context.DeadlineExceeded {
					funcLogger.Error("Timeout exceeded in notification Manager")
				} else {
					funcLogger.Error(err)
				}
				return

			case n, chanOpen := <-em.ReceiveChan:
				// Channel is closed --> save errors to database and send notifications
				if !chanOpen {
					if shouldTrackErrorCounts {
						if err := saveErrorCountsInDatabase(ctx, em.Service, em.Database, ec); err != nil {
							funcLogger.Error("Error saving new error counts in database.  Please investigate")
						}
					}
					sendServiceEmailIfErrors(ctx, serviceErrorsTable, em)
					return
				}

				// Channel is open: direct the message as needed
				funcLogger.WithField("message", n.GetMessage()).Debug("Received notification message")
				shouldSend := true
				if shouldTrackErrorCounts {
					shouldSend = adjustErrorCountsByServiceAndDirectNotification(n, ec, em.NotificationMinimum)
					if !shouldSend {
						log.WithField("service", n.GetService()).Debug("Error count less than error limit.  Not sending notification")
						continue
					}
				}
				if shouldSend {
					addPushErrorNotificationToServiceErrorsTable(n, serviceErrorsTable)
					adminChan <- n
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

func sendServiceEmailIfErrors(ctx context.Context, serviceErrorsTable map[string]string, em *ServiceEmailManager) {
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
	msg, err := prepareServiceEmail(ctx, tableString, em.Email)
	if err != nil {
		funcLogger.Error("Error preparing service email for sending")
	}

	start := time.Now()
	var success bool
	err = SendMessage(ctx, em.Email, msg)
	dur := time.Since(start).Seconds()
	if err != nil {
		funcLogger.Error("Error sending email")
	} else {
		serviceErrorNotificationSendDuration.Observe(dur)
		success = true
	}
	serviceErrorNotificationAttemptTimestamp.WithLabelValues(em.Service, strconv.FormatBool(success)).SetToCurrentTime()
}

// prepareServiceEmail sets a passed-in email object's templateStruct field to the passed in errorTable, and returns a string that contains
// email text according to the passed in errorTable and the email object's templatePath
func prepareServiceEmail(ctx context.Context, errorTable string, e *email) (string, error) {
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
