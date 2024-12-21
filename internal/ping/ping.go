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
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"

	"github.com/fermitools/managed-tokens/internal/tracing"
	"github.com/fermitools/managed-tokens/internal/utils"
)

var (
	pingExecutables = map[string]string{
		"ping": "",
	}
	tracer = otel.Tracer("ping")
)

// Node is an interactive node
type Node string

// NewNode returns a Node object when given a string
func NewNode(s string) Node { return Node(s) }

// PingNode pings a node (described by a Node object) with a 5-second timeout.  It returns an error
func (n Node) Ping(ctx context.Context, extraPingOpts []string) error {
	ctx, span := tracer.Start(ctx, "Node.Ping")
	span.SetAttributes(attribute.String("node", string(n)))
	defer span.End()

	args, err := parseAndExecutePingTemplate(string(n), extraPingOpts)
	if err != nil {
		err = fmt.Errorf("could not ping node: %w", err)
		tracing.LogErrorWithTrace(span, err)
		return err
	}

	cmd := exec.CommandContext(ctx, pingExecutables["ping"], args...)
	span.AddEvent("Running command to ping node: " + cmd.String()) // TODO This should only be done if we're in verbose mode
	if stdoutStderr, err := cmd.CombinedOutput(); err != nil {
		if err2 := ctx.Err(); err2 != nil {
			err2 = fmt.Errorf("could not ping node: %w", err2)
			tracing.LogErrorWithTrace(span, err2,
				tracing.KeyValueForLog{Key: "command", Value: cmd.String()},
			)
			return err2
		}
		tracing.LogErrorWithTrace(span, fmt.Errorf("could not ping node: %w", err),
			tracing.KeyValueForLog{Key: "command", Value: cmd.String()},
		)
		return fmt.Errorf("%s %s", stdoutStderr, err)

	}
	span.SetStatus(codes.Ok, "Ping successful")
	return nil
}

// String converts a Node object into a string
func (n Node) String() string { return string(n) }

func init() {
	if err := utils.CheckForExecutables(pingExecutables); err != nil {
		fmt.Println("One or more required executables were not found in $PATH.  Will still attempt to run, but this will probably fail")
	}
}

func parseAndExecutePingTemplate(node string, extraPingOpts []string) ([]string, error) {
	mergedPingOpts, err := mergePingOpts(extraPingOpts)
	if err != nil {
		return nil, fmt.Errorf("could not parse and execute ping template: %w", err)
	}
	finalPingOpts := strings.Join(mergedPingOpts, " ")
	pingTemplate, err := template.New("ping").Parse(fmt.Sprintf("%s {{.Node}}", finalPingOpts))
	if err != nil {
		return nil, fmt.Errorf("could not parse and execute ping template: %w", err)
	}

	var pArgs = map[string]string{
		"Node": node,
	}
	args, err := utils.TemplateToCommand(pingTemplate, pArgs)
	if err != nil {
		var t1 *utils.TemplateExecuteError
		var t2 *utils.TemplateArgsError
		if errors.As(err, &t1) {
			return nil, fmt.Errorf("could not execute ping template: %w", err)
		}
		if errors.As(err, &t2) {
			return nil, fmt.Errorf("could not get ping command arguments from template: %w", err)
		}
	}
	return args, nil
}

// mergePingOpts will evaluate the extra args and return a slice of args containing the merged arguments
func mergePingOpts(extraArgs []string) ([]string, error) {
	fs := pflag.NewFlagSet("ping flags", pflag.ContinueOnError)

	// Load our default set.  Note that I'm using these names as a workaround as pflag doesn't provide support for shorthand flags only, which is a bummer
	fs.StringS("pingFlagW", "W", "5", "")
	fs.StringS("pingFlagc", "c", "1", "")

	return utils.MergeCmdArgs(fs, extraArgs)
}
