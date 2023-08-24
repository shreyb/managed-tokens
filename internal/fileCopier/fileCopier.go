// Package fileCopier contains interfaces and functions to assist in copying files via ssh.
package fileCopier

import (
	"context"
	"errors"
	"fmt"

	"text/template"

	log "github.com/sirupsen/logrus"

	"github.com/shreyb/managed-tokens/internal/environment"
	"github.com/shreyb/managed-tokens/internal/utils"
)

var fileCopierExecutables map[string]string = map[string]string{
	"rsync": "",
	"ssh":   "",
}

// FileCopier is an exported interface for objects that manage the copying of a file
type FileCopier interface {
	copyToDestination(ctx context.Context) error
}

// NewSSHFileCopier returns a FileCopier object that copies a file via ssh
func NewSSHFileCopier(source, account, node, destination, fileCopierOptions, sshOptions string, env environment.CommandEnvironment) FileCopier {
	// Default ssh options
	sshOpts := "-o ConnectTimeout=30 -o ServerAliveInterval=30 -o ServerAliveCountMax=1"
	if sshOptions == "" {
		sshOptions = sshOpts
	}

	return &rsyncSetup{
		source:             source,
		account:            account,
		node:               node,
		destination:        destination,
		sshOpts:            sshOptions,
		rsyncOpts:          fileCopierOptions,
		CommandEnvironment: env,
	}
}

// CopyToDestination wraps a FileCopier's copyToDestination method
func CopyToDestination(ctx context.Context, f FileCopier) error {
	return f.copyToDestination(ctx)
}

// Type rsyncSetup contains the information needed copy a file to a specified destination via rsync
type rsyncSetup struct {
	source      string
	account     string
	node        string
	destination string
	sshOpts     string
	rsyncOpts   string
	environment.CommandEnvironment
}

// copyToDestination copies a file from the path at source to a destination according to the rsyncSetup struct
func (r *rsyncSetup) copyToDestination(ctx context.Context) error {
	err := rsyncFile(ctx, r.source, r.node, r.account, r.destination, r.sshOpts, r.rsyncOpts, r.CommandEnvironment)
	if err != nil {
		log.WithFields(log.Fields{
			"sourcePath": r.source,
			"destPath":   r.destination,
			"node":       r.node,
			"account":    r.account,
			"rsyncOpts":  r.rsyncOpts,
		}).Error("Could not copy source file to destination node")
	}
	return err
}

// rsyncFile runs rsync on a file at source, and syncs it with the destination account@node:dest
func rsyncFile(ctx context.Context, source, node, account, dest, sshOptions, rsyncOptions string, environ environment.CommandEnvironment) error {
	utils.CheckForExecutables(fileCopierExecutables)

	funcLogger := log.WithFields(log.Fields{
		"source":       source,
		"node":         node,
		"destination":  dest,
		"account":      account,
		"sshOptions":   sshOptions,
		"rsyncOptions": rsyncOptions,
	})

	rsyncArgs := "-e \"{{.SSHExe}} {{.SSHOpts}}\" {{.RsyncOpts}} {{.SourcePath}} {{.Account}}@{{.Node}}:{{.DestPath}}"
	rsyncTemplate, err := template.New("rsync").Parse(rsyncArgs)
	if err != nil {
		funcLogger.Error("could not parse rsync template")
		return err
	}

	cArgs := struct{ SSHExe, SSHOpts, RsyncOpts, SourcePath, Account, Node, DestPath string }{
		SSHExe:     fileCopierExecutables["ssh"],
		SSHOpts:    sshOptions,
		RsyncOpts:  rsyncOptions,
		SourcePath: source,
		Account:    account,
		Node:       node,
		DestPath:   dest,
	}

	args, err := utils.TemplateToCommand(rsyncTemplate, cArgs)
	if err != nil {
		var t1 *utils.TemplateExecuteError
		var t2 *utils.TemplateArgsError
		var retErr error
		if errors.As(err, &t1) {
			retErr = fmt.Errorf("could not execute rsync template: %w", err)
		}
		if errors.As(err, &t2) {
			retErr = fmt.Errorf("could not get rsync command arguments from template: %w", err)
		}
		funcLogger.Error(retErr.Error())
		return retErr
	}

	cmd := environment.KerberosEnvironmentWrappedCommand(ctx, &environ, fileCopierExecutables["rsync"], args...)
	funcLogger.WithFields(log.Fields{
		"command":     cmd.String(),
		"environment": environ.String(),
	}).Debug("Running commmand to rsync file")

	if err := cmd.Run(); err != nil {
		err := fmt.Sprintf("rsync command failed: %s", err.Error())
		funcLogger.WithField("command", cmd.String()).Error(err)

		return errors.New(err)
	}

	log.WithFields(log.Fields{
		"account":  account,
		"node":     node,
		"destPath": dest,
	}).Debug("rsync successful")
	return nil
}
