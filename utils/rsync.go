package utils

import (
	"context"
	"errors"
	"fmt"

	"os/exec"
	"strings"
	"text/template"

	"github.com/shreyb/managed-tokens/service"
	log "github.com/sirupsen/logrus"
)

const (
	rsyncArgs = "-p -e \"{{.SSHExe}} {{.SSHOpts}}\" --chmod=u=rw,go= {{.SourcePath}} {{.Account}}@{{.Node}}.fnal.gov:{{.DestPath}}"
	sshOpts   = "-o ConnectTimeout=30 -o ServerAliveInterval=30 -o ServerAliveCountMax=1"
)

type FileCopier interface {
	copyToDestination(ctx context.Context) error
}

// Added this stuff to make it work
func NewSSHFileCopier(source, account, node, destination, sshOptions string, env service.EnvironmentMapper) FileCopier {
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

func CopyToDestination(ctx context.Context, f FileCopier) error {
	return f.copyToDestination(ctx)
}

var rsyncTemplate = template.Must(template.New("rsync").Parse(rsyncArgs))

// Type rsyncSetup contains the information needed to rsync a file to a certain destination
type rsyncSetup struct {
	source      string
	account     string
	node        string
	destination string
	sshOpts     string
	service.EnvironmentMapper
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
func rsyncFile(ctx context.Context, source, node, account, dest string, sshOptions string, environ service.EnvironmentMapper) error {
	rsyncExecutables := map[string]string{
		"rsync": "",
		"ssh":   "",
	}

	CheckForExecutables(rsyncExecutables)

	var b strings.Builder
	cArgs := struct{ SSHExe, SSHOpts, SourcePath, Account, Node, DestPath string }{
		SSHExe:     rsyncExecutables["ssh"],
		SSHOpts:    sshOptions,
		SourcePath: source,
		Account:    account,
		Node:       node,
		DestPath:   dest,
	}

	if err := rsyncTemplate.Execute(&b, cArgs); err != nil {
		err := fmt.Sprintf("Could not execute rsync template: %s", err.Error())
		log.WithField("source", source).Error(err)
		return errors.New(err)
	}

	args, err := GetArgsFromTemplate(b.String())
	if err != nil {
		err := fmt.Sprintf("Could not get rsync command arguments from template: %s", err.Error())
		log.WithField("source", source).Error(err)
		return errors.New(err)
	}

	cmd := exec.CommandContext(ctx, rsyncExecutables["rsync"], args...)
	cmd = KerberosEnvironmentWrappedCommand(cmd, environ)
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
