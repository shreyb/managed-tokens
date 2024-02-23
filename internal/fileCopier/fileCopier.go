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

// Package fileCopier contains interfaces and functions to assist in copying files via ssh.
package fileCopier

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"text/template"

	"github.com/cornfeedhobo/pflag"
	log "github.com/sirupsen/logrus"

	"github.com/fermitools/managed-tokens/internal/environment"
	"github.com/fermitools/managed-tokens/internal/utils"
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

// mergeSshOpts expects args to be passed in the ssh option format, without the -o specification.
// For example, if a user wants to pass "-o ConnectTimeout=30", they should pass []string{"ConnectTimeout=30"}
// All options passed here will be returned with the "-o" prepended, for use in ssh commands, so the only options that
// should be passed are those supported by the ssh utility
func mergeSshOpts(extraArgs []string) ([]string, error) {
	defaultArgs := []string{"-o", "ConnectTimeout=30", "-o", "ServerAliveInterval=30", "-o", "ServerAliveCountMax=1"}
	fs := pflag.NewFlagSet("ssh args", pflag.ContinueOnError)
	fs.ParseErrorsWhitelist = pflag.ParseErrorsWhitelist{UnknownFlags: true}

	// Defaults for ssh options
	fs.String("ConnectTimeout", "30", "")
	fs.String("ServerAliveInterval", "30", "")
	fs.String("ServerAliveCountMax", "1", "")

	_preprocessedArgs := preProcessSshOpts(extraArgs)

	_mergedArgs, err := utils.MergeCmdArgs(fs, _preprocessedArgs)
	if err != nil {
		log.WithField("args", extraArgs).Error("Could not merge ssh args. Using default")
		return defaultArgs, nil
	}

	mergedArgs := correctMergedSshOpts(_mergedArgs)

	return mergedArgs, nil

}

func preProcessSshOpts(args []string) []string {
	// Preprocessing - add "--" to the extra args
	_preprocessedArgs := make([]string, 0)
	for _, arg := range args {
		if arg == "-o" {
			continue
		}
		var _arg string
		if strings.Contains(arg, "=") {
			_arg = "--" + arg // ArgName=argValue --> --ArgName=argValue
			_preprocessedArgs = append(_preprocessedArgs, _arg)
		}
	}
	return _preprocessedArgs
}

func correctMergedSshOpts(args []string) []string {
	// We have to do a little extra processing here to convert something that looks like
	// []string{--Arg1, val1, --Arg2, val2, Arg3=val3}
	// to become:
	// []string{-o Arg1=val1 -o Arg2=val2 -o Arg3=val3}
	correctedArgs := make([]string, 0)

	for i := 0; i < len(args); i++ {
		_arg := args[i]
		if strings.HasPrefix(_arg, "--") {
			_arg = strings.TrimPrefix(_arg, "--")
			var equalArg string
			if strings.Contains(_arg, "=") {
				// Argument=value --> keep as is
				equalArg = _arg
			} else {
				// Argument value --> Argument=value
				equalArg = _arg + "=" + args[i+1]
			}
			correctedArgs = append(correctedArgs, "-o", equalArg)
		}
	}
	return correctedArgs
}
