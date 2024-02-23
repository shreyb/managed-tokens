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

// Package ping provides utilities to ping a remote host
package ping

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"text/template"

	"github.com/cornfeedhobo/pflag"
	log "github.com/sirupsen/logrus"

	"github.com/fermitools/managed-tokens/internal/utils"
)

var (
	pingExecutables = map[string]string{
		"ping": "",
	}
)

// PingNoder is an interface that wraps the PingNode method. It is meant to be used where pinging a node is necessary.  PingNoders also implement the Stringer interface.
type PingNoder interface {
	PingNode(context.Context, []string) error
	String() string
}

// Node is an interactive node
type Node string

// NewNode returns a Node object when given a string
func NewNode(s string) Node { return Node(s) }

// PingNode pings a node (described by a Node object) with a 5-second timeout.  It returns an error
func (n Node) PingNode(ctx context.Context, extraPingArgs []string) error {
	funcLogger := log.WithField("node", string(n))

	args, err := parseAndExecutePingTemplate(string(n), extraPingArgs)
	if err != nil {
		funcLogger.Error("Could not parse and execute ping template")
		return err
	}

	cmd := exec.CommandContext(ctx, pingExecutables["ping"], args...)
	funcLogger.WithField("command", cmd.String()).Debug("Running command to ping node")
	if cmdOut, cmdErr := cmd.CombinedOutput(); cmdErr != nil {
		if e := ctx.Err(); e != nil {
			funcLogger.WithField("command", cmd.String()).Error(fmt.Sprintf("Context error: %s", e.Error()))
			return e
		}

		funcLogger.WithField("command", cmd.String()).Error(fmt.Sprintf("Error running ping command: %s %s", string(cmdOut), cmdErr.Error()))
		return fmt.Errorf("%s %s", cmdOut, cmdErr)

	}
	return nil
}

// String converts a Node object into a string
func (n Node) String() string { return string(n) }

// PingNodeStatus stores information about an attempt to ping a Node.  If there was an error, it's stored in Err.
type PingNodeStatus struct {
	PingNoder
	Err error
}

func init() {
	if err := utils.CheckForExecutables(pingExecutables); err != nil {
		log.WithField("executableGroup", "ping").Error("One or more required executables were not found in $PATH.  Will still attempt to run, but this will probably fail")
	}
}

func parseAndExecutePingTemplate(node string, extraPingArgs []string) ([]string, error) {
	mergedPingArgs, err := mergePingArgs(extraPingArgs)
	if err != nil {
		msg := "could not merge ping args"
		log.WithFields(log.Fields{
			"extraPingArgs": extraPingArgs,
			"node":          node,
		}).Errorf(msg)
		return nil, errors.New("msg")
	}
	finalPingArgs := strings.Join(mergedPingArgs, " ")
	pingTemplate, err := template.New("ping").Parse(fmt.Sprintf("%s {{.Node}}", finalPingArgs))
	if err != nil {
		log.Error("could not parse ping template")
		return nil, err
	}

	var pArgs = map[string]string{
		"Node": node,
	}
	args, err := utils.TemplateToCommand(pingTemplate, pArgs)
	if err != nil {
		var t1 *utils.TemplateExecuteError
		var t2 *utils.TemplateArgsError
		var retErr error
		if errors.As(err, &t1) {
			retErr = fmt.Errorf("could not execute ping template: %w", err)
		}
		if errors.As(err, &t2) {
			retErr = fmt.Errorf("could not get ping command arguments from template: %w", err)
		}
		log.Error(retErr.Error())
		return nil, retErr
	}
	return args, nil
}

// mergePingArgs will evaluate the extra args and return a slice of args containing the merged arguments
func mergePingArgs(extraArgs []string) ([]string, error) {
	fs := pflag.NewFlagSet("ping flags", pflag.ContinueOnError)
	fs.ParseErrorsWhitelist = pflag.ParseErrorsWhitelist{UnknownFlags: true}

	// Load our default set.  Note that I'm using these names as a workaround as pflag doesn't provide support for shorthand flags only, which is a bummer
	fs.StringP("pingFlagW", "W", "5", "")
	fs.StringP("pingFlagc", "c", "1", "")

	// See if we override any of our args by parsing the extraArgs
	fs.Parse(extraArgs)

	flags := make([]string, 0)
	fs.VisitAll(func(f *pflag.Flag) {
		var flagName string
		if f.Shorthand == "" {
			flagName = "--" + f.Name
		} else {
			flagName = "-" + f.Shorthand
		}

		flags = append(flags, flagName)
		flags = append(flags, f.Value.String())
	})
	fmt.Println(fs.GetUnknownFlags())
	flags = append(flags, fs.GetUnknownFlags()...)

	return flags, nil

}
