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
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

const (
	goodhost string = "localhost"
	badhost  string = "thisisafakehostandwillneverbeusedever.example.com"
)

var ctx context.Context = context.Background()

func TestPingNode(t *testing.T) {
	timeoutCtx, timeoutCancel := context.WithTimeout(ctx, time.Duration(1*time.Nanosecond))
	t.Cleanup(timeoutCancel)

	type testCase struct {
		description string
		c           context.Context
		Node
		extraArgs      []string
		expectedErrNil bool
	}

	testCases := []testCase{
		{
			"Good node",
			ctx,
			NewNode(goodhost),
			[]string{},
			true,
		},
		{
			"Bad node",
			ctx,
			NewNode(badhost),
			[]string{},
			false,
		},
		{
			"Good node, timeout",
			timeoutCtx,
			NewNode(goodhost),
			[]string{},
			false,
		},
		{
			"Good node, good extra args",
			ctx,
			NewNode(goodhost),
			[]string{"-4"},
			true,
		},
		{
			"bad extra args",
			ctx,
			NewNode(goodhost),
			[]string{"--invalidextraarg"},
			false,
		},
	}

	for _, test := range testCases {
		t.Run(
			test.description,
			func(t *testing.T) {
				err := test.Node.Ping(test.c, test.extraArgs)
				errNil := (err == nil)
				assert.Equal(t, test.expectedErrNil, errNil)
			},
		)
	}
}

func TestParseAndExecutePingTemplate(t *testing.T) {
	node := "mynode"

	type testCase struct {
		description string
		extraArgs   []string
		expected    []string
	}

	testCases := []testCase{
		{
			"No extra args (default)",
			[]string{},
			[]string{"-W", "5", "-c", "1", node},
		},
		{
			"Extra args",
			[]string{"--foo", "--bar", "--baz"},
			[]string{"-W", "5", "-c", "1", "--foo", "--bar", "--baz", node},
		},
		{
			"Extra args, invalid arg form (gets sanitized to default)",
			[]string{"foo", "bar", "baz", "{{for}}"},
			[]string{"-W", "5", "-c", "1", node},
		},
	}

	for _, test := range testCases {
		t.Run(
			test.description,
			func(t *testing.T) {
				result, err := parseAndExecutePingTemplate(node, test.extraArgs)
				assert.Equal(t, test.expected, result)
				assert.Nil(t, err)
			},
		)
	}
}

func TestMergePingOpts(t *testing.T) {
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
				sanitizedArgs, err := mergePingOpts(test.extraArgs)
				assert.Equal(t, test.expectedArgs, sanitizedArgs)
				if test.err == nil {
					assert.Nil(t, err)
				}
			},
		)
	}
}
