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

package ping

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

const (
	goodhost string = "localhost"
	badhost  string = "thisisafakehostandwillneverbeusedever.example.com"
)

var ctx context.Context = context.Background()

type goodNode string

func (g goodNode) PingNode(ctx context.Context, extraArgs []string) error {
	time.Sleep(1 * time.Microsecond)
	if e := ctx.Err(); e != nil {
		return e
	}
	fmt.Println("Running fake PingNode")
	return nil
}

func (g goodNode) String() string { return string(g) }

type badNode string

func (b badNode) PingNode(ctx context.Context, extraArgs []string) error {
	for _, arg := range extraArgs {
		if arg == "--invalidextraarg" {
			return fmt.Errorf("invalid argument")
		}
	}
	time.Sleep(1 * time.Microsecond)
	if e := ctx.Err(); e != nil {
		return e
	}
	return fmt.Errorf("exit status 2 ping: unknown host %s", b)
}

func (b badNode) String() string { return string(b) }

// Tests
// TestPingNodeGood pings a PingNoder and makes sure we get no error
func TestPingNode(t *testing.T) {
	timeoutCtx, timeoutCancel := context.WithTimeout(ctx, time.Duration(1*time.Nanosecond))
	t.Cleanup(timeoutCancel)

	type testCase struct {
		description string
		c           context.Context
		PingNoder
		extraArgs      []string
		expectedErrNil bool
	}

	testCases := []testCase{
		{
			"Good node",
			ctx,
			goodNode(goodhost),
			[]string{},
			true,
		},
		{
			"Bad node",
			ctx,
			badNode(badhost),
			[]string{},
			false,
		},
		{
			"Bad node, timeout",
			timeoutCtx,
			badNode(badhost),
			[]string{},
			false,
		},
		{
			"Good node, good extra args",
			ctx,
			goodNode(goodhost),
			[]string{"-4"},
			true,
		},
		{
			"bad extra args",
			ctx,
			badNode(goodhost),
			[]string{"--invalidextraarg"},
			false,
		},
	}

	for _, test := range testCases {
		t.Run(
			test.description,
			func(t *testing.T) {
				err := test.PingNoder.PingNode(test.c, test.extraArgs)
				errNil := (err == nil)
				assert.Equal(t, test.expectedErrNil, errNil)
			},
		)
	}
}

func TestParseAndExecutePingTemplate(t *testing.T) {
	node := "mynode"

	type testCase struct {
		description    string
		extraArgs      []string
		expected       []string
		expectedErrNil bool
	}

	testCases := []testCase{
		{
			"No extra args (default)",
			[]string{},
			[]string{"-W", "5", "-c", "1", node},
			true,
		},
		{
			"Extra args",
			[]string{"foo", "bar", "baz"},
			[]string{"foo", "bar", "baz", "-W", "5", "-c", "1", node},
			true,
		},
		{
			"Extra args, invalid",
			[]string{"foo", "bar", "baz", "{{for}}"},
			nil,
			false,
		},
	}

	for _, test := range testCases {
		t.Run(
			test.description,
			func(t *testing.T) {
				result, err := parseAndExecutePingTemplate(node, test.extraArgs)
				assert.Equal(t, test.expected, result)
				errNil := (err == nil)
				assert.Equal(t, test.expectedErrNil, errNil)
			},
		)
	}
}

func TestMergePingArgs(t *testing.T) {
	defaultArgs := []string{"-W", "5", "-c", "1"}

	type testCase struct {
		description  string
		extraArgs    []string
		expectedArgs []string
		err          error
	}

	testCases := []testCase{
		{
			"Default case",
			[]string{},
			defaultArgs,
			nil,
		},
		{
			"Override a default case",
			[]string{"-W", "6"},
			[]string{"-W", "6", "-c", "1"},
			nil,
		},
		{
			"Provide new flags",
			[]string{"-4"},
			[]string{"-W", "5", "-c", "1", "-4"},
			nil,
		},
		{
			"Provide new flags and overwrite defaults",
			[]string{"-4", "-W", "6"},
			[]string{"-W", "6", "-c", "1", "-4"},
			nil,
		},
	}

	for _, test := range testCases {
		t.Run(
			test.description,
			func(t *testing.T) {
				sanitizedArgs, err := mergePingArgs(test.extraArgs)
				assert.Equal(t, test.expectedArgs, sanitizedArgs)
				if test.err == nil {
					assert.Nil(t, err)
				}
			},
		)
	}

}
