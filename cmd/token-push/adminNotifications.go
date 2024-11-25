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

package main

import (
	"context"
	"time"

	"github.com/fermitools/managed-tokens/internal/db"
	"github.com/fermitools/managed-tokens/internal/notifications"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
)

// Prep admin notifications

// setupAdminNotifications prepares a notifications.AdminNotificationManager, and returns the following:
// 1. A pointer to the AdminNotificationsManager that was set up
// 2. A channel that the caller will send its notifications to for the AdminNotificationManager to process.
// 3. A slice of notifications.SendMessagers that will be populated by the errors the AdminNotificationManager collects
func setupAdminNotifications(ctx context.Context, database *db.ManagedTokensDatabase) (*notifications.AdminNotificationManager, chan<- notifications.SourceNotification, []notifications.SendMessager) {
	var adminNotifications []notifications.SendMessager

	ctx, span := otel.GetTracerProvider().Tracer("token-push").Start(ctx, "setupAdminNotifications")
	defer span.End()

	var prefix string
	if viper.GetBool("test") {
		prefix = "notifications_test."
	} else {
		prefix = "notifications."
	}

	now := time.Now().Format(time.RFC822)
	email := notifications.NewEmail(
		viper.GetString("email.from"),
		viper.GetStringSlice(prefix+"admin_email"),
		"Managed Tokens Errors "+now,
		viper.GetString("email.smtphost"),
		viper.GetInt("email.smtpport"),
	)
	slackMessage := notifications.NewSlackMessage(viper.GetString(prefix + "slack_alerts_url"))
	adminNotifications = append(adminNotifications, email, slackMessage)

	// Functional options for AdminNotificationManager
	funcOpts := make([]notifications.AdminNotificationManagerOption, 0)
	setNotificationMinimum := func(a *notifications.AdminNotificationManager) error {
		exeLogger.Debug("Setting AdminNotificationManager NotificationMinimum")
		a.NotificationMinimum = viper.GetInt("errorCountToSendMessage")
		return nil
	}
	funcOpts = append(funcOpts, setNotificationMinimum)

	if database != nil {
		setDB := func(a *notifications.AdminNotificationManager) error {
			exeLogger.Debug("Setting AdminNotificationManager Database")
			a.Database = database
			return nil
		}
		funcOpts = append(funcOpts, setDB)
	}

	a := notifications.NewAdminNotificationManager(ctx, funcOpts...)
	c := a.RegisterNotificationSource(ctx)
	return a, c, adminNotifications
}

func sendAdminNotifications(ctx context.Context, a *notifications.AdminNotificationManager, adminNotificationsPtr *[]notifications.SendMessager) error {
	ctx, span := otel.GetTracerProvider().Tracer("token-push").Start(ctx, "sendAdminNotifications")
	defer span.End()

	funcLogger := log.WithFields(log.Fields{
		"executable": currentExecutable,
		"func":       "sendAdminNotifications",
	})

	// Make sure that all of our Service Email Managers have finished sending their notifications
	handleNotificationsFinalization()
	a.RequestToCloseReceiveChan(ctx)

	if err := notifications.SendAdminNotifications(
		ctx,
		currentExecutable,
		viper.GetBool("test"),
		(*adminNotificationsPtr)...,
	); err != nil {
		msg := "Error sending admin notifications"
		span.RecordError(err)
		span.SetStatus(codes.Error, msg)
		funcLogger.Error(msg)
		return err
	}
	span.SetStatus(codes.Ok, "Admin notifications sent successfully")
	return nil
}
