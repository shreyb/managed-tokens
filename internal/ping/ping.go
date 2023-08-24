// Package ping provides utilities to ping a remote host
package ping

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"sync"
	"text/template"

	log "github.com/sirupsen/logrus"

	"github.com/shreyb/managed-tokens/internal/utils"
)

var (
	pingExecutables = map[string]string{
		"ping": "",
	}
)

// PingNoder is an interface that wraps the PingNode method. It is meant to be used where pinging a node is necessary.  PingNoders also implement the Stringer interface.
type PingNoder interface {
	PingNode(context.Context) error
	String() string
}

// Node is an interactive node
type Node string

// NewNode returns a Node object when given a string
func NewNode(s string) Node { return Node(s) }

// PingNode pings a node (described by a Node object) with a 5-second timeout.  It returns an error
func (n Node) PingNode(ctx context.Context) error {

	funcLogger := log.WithField("node", string(n))
	args, err := parseAndExecutePingTemplate(string(n))
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

// PingAllNodes will launch goroutines, which each ping a PingNoder from the nodes variadic.  It returns a channel,
// on which it reports the pingNodeStatuses signifying success or error
func PingAllNodes(ctx context.Context, nodes ...PingNoder) <-chan PingNodeStatus {
	// Buffered Channel to report on
	c := make(chan PingNodeStatus, len(nodes))
	var wg sync.WaitGroup
	wg.Add(len(nodes))
	for _, n := range nodes {
		go func(n PingNoder) {
			defer wg.Done()
			p := PingNodeStatus{n, n.PingNode(ctx)}
			c <- p
		}(n)
	}

	// Wait for all goroutines to finish, then close channel so that the caller knows all objects have been sent
	go func() {
		defer close(c)
		wg.Wait()
	}()

	return c
}

func init() {
	if err := utils.CheckForExecutables(pingExecutables); err != nil {
		log.WithField("executableGroup", "ping").Error("One or more required executables were not found in $PATH.  Will still attempt to run, but this will probably fail")
	}
}

func parseAndExecutePingTemplate(node string) ([]string, error) {
	pingTemplate, err := template.New("ping").Parse("-W 5 -c 1 {{.Node}}")
	if err != nil {
		log.Error("could not parse kinit template")
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
