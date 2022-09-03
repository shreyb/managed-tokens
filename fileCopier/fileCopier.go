package fileCopier

import (
	"context"
	"errors"
	"fmt"

	"strings"
	"text/template"

	log "github.com/sirupsen/logrus"

	"github.com/shreyb/managed-tokens/environment"
	"github.com/shreyb/managed-tokens/utils"
)

// FileCopier is an exported interface for objects that manage the copying of a file
type FileCopier interface {
	copyToDestination(ctx context.Context) error
}

// Added this stuff to make it work
// func NewSSHFileCopier(source, account, node, destination, sshOptions string, env service.EnvironmentMapper) FileCopier {

// NewSSHFileCopier returns a FileCopier object that copies a file via ssh
func NewSSHFileCopier(source, account, node, destination, sshOptions string, env environment.EnvironmentMapper) FileCopier {
	if sshOptions == "" {
		sshOptions = sshOpts
	}
	return &rsyncSetup{
		source:            source,
		account:           account,
		node:              node,
		destination:       destination,
		sshOpts:           sshOptions,
		EnvironmentMapper: env,
	}
}

// CopyToDestination wraps a FileCopier's copyToDestination method
func CopyToDestination(ctx context.Context, f FileCopier) error {
	return f.copyToDestination(ctx)
}

var rsyncTemplate = template.Must(template.New("rsync").Parse(rsyncArgs))

const (
	rsyncArgs = "-e \"{{.SSHExe}} {{.SSHOpts}}\" --chmod=u=rw,go= {{.SourcePath}} {{.Account}}@{{.Node}}:{{.DestPath}}"
	sshOpts   = "-o ConnectTimeout=30 -o ServerAliveInterval=30 -o ServerAliveCountMax=1"
)

// Type rsyncSetup contains the information needed copy a file to a specified destination via rsync
type rsyncSetup struct {
	source      string
	account     string
	node        string
	destination string
	sshOpts     string
	environment.EnvironmentMapper
}

// copyToDestination copies a file from the path at source to a destination according to the rsyncSetup struct
func (r *rsyncSetup) copyToDestination(ctx context.Context) error {
	err := rsyncFile(ctx, r.source, r.node, r.account, r.destination, r.sshOpts, r.EnvironmentMapper)
	if err != nil {
		log.WithFields(log.Fields{
			"sourcePath": r.source,
			"destPath":   r.destination,
			"node":       r.node,
			"account":    r.account,
		}).Error("Could not copy source file to destination node")
	}
	return err
}

// rsyncFile runs rsync on a file at source, and syncs it with the destination account@node:dest
func rsyncFile(ctx context.Context, source, node, account, dest string, sshOptions string, environ environment.EnvironmentMapper) error {
	rsyncExecutables := map[string]string{
		"rsync": "",
		"ssh":   "",
	}

	utils.CheckForExecutables(rsyncExecutables)

	cArgs := struct{ SSHExe, SSHOpts, SourcePath, Account, Node, DestPath string }{
		SSHExe:     rsyncExecutables["ssh"],
		SSHOpts:    sshOptions,
		SourcePath: source,
		Account:    account,
		Node:       node,
		DestPath:   dest,
	}

	args, err := utils.TemplateToCommand(rsyncTemplate, cArgs)
	var t1 *utils.TemplateExecuteError
	if errors.As(err, &t1) {
		retErr := fmt.Errorf("could not execute rsync template: %w", err)
		log.WithFields(log.Fields{
			"source":      source,
			"node":        node,
			"destination": dest,
			"account":     account,
			"sshOptions":  sshOptions,
		}).Error(retErr.Error())
		return retErr
	}
	var t2 *utils.TemplateArgsError
	if errors.As(err, &t2) {
		retErr := fmt.Errorf("could not get rsync command arguments from template: %w", err)
		log.WithFields(log.Fields{
			"source":      source,
			"node":        node,
			"destination": dest,
			"account":     account,
			"sshOptions":  sshOptions,
		}).Error(retErr.Error())
		return retErr
	}

	cmd := environment.KerberosEnvironmentWrappedCommand(ctx, environ, rsyncExecutables["rsync"], args...)
	if err := cmd.Run(); err != nil {
		err := fmt.Sprintf("rsync command failed: %s", err.Error())
		log.WithFields(log.Fields{
			"sshOpts":    sshOptions,
			"sourcePath": source,
			"account":    account,
			"node":       node,
			"destPath":   dest,
			"command":    strings.Join(cmd.Args, " "),
		}).Error(err)

		return errors.New(err)
	}

	log.WithFields(log.Fields{
		"account":  account,
		"node":     node,
		"destPath": dest,
	}).Debug("rsync successful")
	return nil

}
