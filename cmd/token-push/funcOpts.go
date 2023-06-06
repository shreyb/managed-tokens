package main

import (
	"context"
	"errors"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"

	"github.com/shreyb/managed-tokens/internal/db"
)

// Functional options for worker.Config initialization

// getDesiredUIByOverrideOrLookup gets the DesiredUID for a service by checking the configuration in the "account"
// field.  It then checks the configuration to see if there is a configured override for the UID.  If it not overridden,
// the default behavior is to query the managed tokens database that should be populated by the refresh-uids-from-ferry executable.
//
// If the default behavior is not possible, the configuration should have a desiredUIDOverride field to allow token-push to run properly
func getDesiredUIByOverrideOrLookup(ctx context.Context, serviceConfigPath string, database *db.ManagedTokensDatabase) (uint32, error) {
	if viper.IsSet(serviceConfigPath + ".desiredUIDOverride") {
		return viper.GetUint32(serviceConfigPath + ".desiredUIDOverride"), nil
	}
	// Get UID from SQLite DB that should be kept up to date by refresh-uids-from-ferry
	if database == nil {
		msg := "no valid database to read UID from"
		log.Error(msg)
		return 0, errors.New(msg)
	}
	username := viper.GetString(serviceConfigPath + ".account")
	uid, err := database.GetUIDByUsername(ctx, username)
	if err != nil {
		log.Error("Could not get UID by username")
		return 0, err
	}
	log.WithFields(log.Fields{
		"username": username,
		"uid":      uid,
	}).Debug("Got UID")
	return uint32(uid), nil
}

// getDefaultRoleFileDestinationTemplate gets the template that the pushTokenWorker should use when
// deriving the default role file path on the destination node.
func getDefaultRoleFileDestinationTemplate(serviceConfigPath string) string {
	defaultRoleFileDestOverridePath := serviceConfigPath + ".defaultRoleFileDestinationTemplateOverride"
	if viper.IsSet(defaultRoleFileDestOverridePath) {
		return viper.GetString(defaultRoleFileDestOverridePath)
	}
	notSetBackup := "/tmp/default_role_{{.Experiment}}_{{.DesiredUID}}" // Default role file destination template
	if defaultRoleFileDestinationTplString := viper.GetString("defaultRoleFileDestinationTemplate"); defaultRoleFileDestinationTplString != "" {
		return defaultRoleFileDestinationTplString
	}
	return notSetBackup
}
