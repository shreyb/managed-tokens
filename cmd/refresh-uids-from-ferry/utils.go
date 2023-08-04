package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/user"
	"path"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"

	"github.com/shreyb/managed-tokens/internal/db"
	"github.com/shreyb/managed-tokens/internal/environment"
	"github.com/shreyb/managed-tokens/internal/kerberos"
	"github.com/shreyb/managed-tokens/internal/notifications"
	"github.com/shreyb/managed-tokens/internal/service"
	"github.com/shreyb/managed-tokens/internal/utils"
	"github.com/shreyb/managed-tokens/internal/worker"
)

// setupAdminNotifications prepares email and slack messages to be sent to admins in case of errors
func setupAdminNotifications(ctx context.Context, database *db.ManagedTokensDatabase) (adminNotifications []notifications.SendMessager, notificationsChan chan notifications.Notification) {
	// Send admin notifications at end of run
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
	slackMessage := notifications.NewSlackMessage(
		viper.GetString(prefix + "slack_alerts_url"),
	)
	adminNotifications = append(adminNotifications, email, slackMessage)

	// Functional options for AdminNotificationManager
	funcOpts := make([]notifications.AdminNotificationManagerOption, 0)
	dontTrackErrorCounts := func(a *notifications.AdminNotificationManager) error {
		a.TrackErrorCounts = false
		return nil
	}
	funcOpts = append(funcOpts, dontTrackErrorCounts)

	if database != nil {
		setDB := func(a *notifications.AdminNotificationManager) error {
			a.Database = database
			return nil
		}
		writeableDatabase := func(a *notifications.AdminNotificationManager) error {
			a.DatabaseReadOnly = false
			return nil
		}
		funcOpts = append(funcOpts, setDB, writeableDatabase)
	}

	notificationsChan = notifications.NewAdminNotificationManager(ctx, funcOpts...).ReceiveChan // Listen for messages from run
	return adminNotifications, notificationsChan
}

// getAllAccountsFromConfig reads the configuration file and gets a slice of accounts
func getAllAccountsFromConfig() []string {
	s := make([]string, 0)

	for experiment := range viper.GetStringMap("experiments") {
		roleConfigPath := "experiments." + experiment + ".roles"
		for role := range viper.GetStringMap(roleConfigPath) {
			accountConfigPath := roleConfigPath + "." + role + ".account"
			account := viper.GetString(accountConfigPath)
			exeLogger.WithField("account", account).Debug("Found account")
			s = append(s, account)
		}
	}
	return s
}

// getBearerTokenDefaultLocation returns the default location of the bearer token
// by looking first at the environment variable BEARER_TOKEN_FILE, and then
// using the current user's UID to find the default location for the bearer token
func getBearerTokenDefaultLocation() (string, error) {
	var location string
	if location = os.Getenv("BEARER_TOKEN_FILE"); location != "" {
		return location, nil
	}

	var tempDir string
	currentUser, err := user.Current()
	if err != nil {
		log.Error("Could not get current user")
		return location, err
	}
	currentUID := currentUser.Uid
	filename := fmt.Sprintf("bt_u%s", currentUID)

	if tempDir = os.Getenv("XDG_RUNTIME_DIR"); tempDir == "" {
		tempDir = os.TempDir()
	}

	return path.Join(tempDir, filename), nil
}

// newFERRYServiceConfigWithKerberosAuth uses the configuration file to return a *worker.Config
// with kerberos credentials initialized
func newFERRYServiceConfigWithKerberosAuth(ctx context.Context) (*worker.Config, error) {
	var serviceName string

	if viper.GetString("ferry.serviceRole") != "" {
		serviceName = viper.GetString("ferry.serviceExperiment") + "_" + viper.GetString("ferry.serviceRole")
	} else {
		serviceName = viper.GetString("ferry.serviceExperiment")
	}
	s := service.NewService(serviceName)

	// Get krb5ccname directory
	krb5ccname, err := os.MkdirTemp("", "managed-tokens")
	if err != nil {
		log.Fatal("Cannot create temporary dir for kerberos cache.  This will cause a fatal race condition.  Exiting")
	}

	userPrincipal, htgettokenopts := getUserPrincipalAndHtgettokenopts()
	serviceConfig, err := worker.NewConfig(
		s,
		worker.SetCommandEnvironment(
			func(e *environment.CommandEnvironment) { e.SetKrb5ccname(krb5ccname, environment.DIR) },
			func(e *environment.CommandEnvironment) { e.SetHtgettokenOpts(htgettokenopts) },
		),
		worker.SetKeytabPath(viper.GetString("ferry.serviceKeytabPath")),
		worker.SetUserPrincipal(userPrincipal),
	)
	if err != nil {
		log.Error("Could not create new service configuration")
		return &worker.Config{}, err
	}

	// Get kerberos ticket and check it.  If we already have kerberos ticket, use it
	if err := kerberos.SwitchCache(ctx, serviceConfig.UserPrincipal, serviceConfig.CommandEnvironment); err != nil {
		log.Warn("No kerberos ticket in cache.  Attempting to get a new one")
		if err := kerberos.GetTicket(ctx, serviceConfig.KeytabPath, serviceConfig.UserPrincipal, serviceConfig.CommandEnvironment); err != nil {
			log.Error("Could not get kerberos ticket to generate JWT")
			return &worker.Config{}, err
		}
		if err := kerberos.CheckPrincipal(ctx, serviceConfig.UserPrincipal, serviceConfig.CommandEnvironment); err != nil {
			log.Error("Verification of kerberos ticket failed")
			return &worker.Config{}, err
		}
	}
	return serviceConfig, nil
}

// checkFerryDataInDB compares two slices of db.FERRYUIDDatum, to ensure that the dbData
// slice contains all of the data in the ferryData slice
func checkFerryDataInDB(ferryData, dbData []db.FerryUIDDatum) bool {
	type datum struct {
		username string
		uid      int
	}

	ferrySlice := make([]datum, 0, len(ferryData))
	for _, d := range ferryData {
		ferrySlice = append(
			ferrySlice,
			datum{
				username: d.Username(),
				uid:      d.Uid(),
			},
		)
	}
	dbSlice := make([]datum, 0, len(dbData))
	for _, d := range dbData {
		dbSlice = append(
			dbSlice,
			datum{
				username: d.Username(),
				uid:      d.Uid(),
			},
		)
	}

	if ok, err := utils.IsSliceSubSlice(ferrySlice, dbSlice); !ok {
		log.Errorf("Verification of INSERT failed: %s", err)
		return false
	}
	return true
}

// getAndAggregateFERRYData takes a username and a function that sets up authentication,
// authFunc.  It spins up a worker to get data from FERRY, and then puts that data into
// a channel for aggregation.
func getAndAggregateFERRYData(ctx context.Context, username string, authFunc func() func(context.Context, string, string) (*http.Response, error),
	ferryDataChan chan<- db.FerryUIDDatum, notificationsChan chan notifications.Notification) {
	var ferryRequestContext context.Context
	if timeout, ok := timeouts["ferryrequesttimeout"]; ok {
		ferryRequestContext = utils.ContextWithOverrideTimeout(ctx, timeout)
	} else {
		ferryRequestContext = ctx
	}
	entry, err := worker.GetFERRYUIDData(
		ferryRequestContext,
		username,
		viper.GetString("ferry.host"),
		viper.GetInt("ferry.port"),
		authFunc(),
		ferryDataChan,
	)
	if err != nil {
		msg := "Could not get FERRY UID data"
		log.WithField("username", username).Error(msg)
		notificationsChan <- notifications.NewSetupError(msg+" for user "+username, currentExecutable)
	} else {
		ferryDataChan <- entry
	}
}

// This space is for other auxiliary functions

// setUserPrincipalAndHtgettokenopts sets a worker.Config's kerberos principal and with it, the HTGETTOKENOPTS environment variable.
func getUserPrincipalAndHtgettokenopts() (string, string) {
	var htgettokenOpts string
	userPrincipal := viper.GetString("ferry.serviceKerberosPrincipal")
	credKey := strings.ReplaceAll(userPrincipal, "@FNAL.GOV", "")

	if viper.IsSet("htgettokenopts") {
		htgettokenOpts = viper.GetString("htgettokenopts")
	} else {
		htgettokenOpts = "--credkey=" + credKey
	}
	return userPrincipal, htgettokenOpts
}
