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

package fileCopier

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/fermitools/managed-tokens/internal/environment"
	"github.com/stretchr/testify/assert"
)

// TestNewSSHFileCopier asserts that the type of the returned object from NewSSHFileCopier
// is an *rsyncSetup
func TestNewSSHFileCopier(t *testing.T) {
	environ := environment.CommandEnvironment{}
	testCopier := NewSSHFileCopier("", "", "", "", []string{}, []string{}, environ)

	if _, ok := testCopier.(*rsyncSetup); !ok {
		t.Errorf(
			"Got wrong type for NewSSHFileCopier.  Expected %T",
			&rsyncSetup{},
		)
	}

}

type fakeCopierProtocolSetup struct {
	err error
}

func (f *fakeCopierProtocolSetup) copyToDestination(ctx context.Context) error {
	return f.err
}

// TestCopyToDestination checks that CopyToDestination properly wraps a FileCopier's
// copyToDestination method and handles the error returned properly
func TestCopyToDestination(t *testing.T) {
	testCases := []struct {
		description string
		fakeCopierProtocolSetup
		err error
	}{
		{
			"Mock FileCopier that returns nil error",
			fakeCopierProtocolSetup{nil},
			nil,
		},
		{
			"Mock FileCopier that returns an error",
			fakeCopierProtocolSetup{errors.New("This is an error")},
			errors.New("This is an error"),
		},
	}

	ctx := context.Background()

	for _, test := range testCases {
		t.Run(
			test.description,
			func(t *testing.T) {
				err := CopyToDestination(ctx, &test.fakeCopierProtocolSetup)
				switch err {
				case nil:
					if test.err != nil {
						t.Errorf(
							"Expected non-nil error %s from CopyToDestination.  Got nil",
							test.err,
						)
					}
				default:
					if test.err == nil {
						t.Errorf(
							"Expected nil error from CopyToDestination.  Got %s",
							err.Error(),
						)
					}
				}
			},
		)
	}
}

func TestMergeSshArgs(t *testing.T) {
	defaultArgs := []string{"-o", "ConnectTimeout=30", "-o", "ServerAliveInterval=30", "-o", "ServerAliveCountMax=1"}

	type testCase struct {
		description  string
		extraArgs    []string
		expectedArgs []string
		expectedErr  error
	}

	testCases := []testCase{
		{
			"default",
			[]string{},
			defaultArgs,
			nil,
		},
		{
			"override default",
			[]string{"ConnectTimeout=40"},
			[]string{"-o", "ConnectTimeout=40", "-o", "ServerAliveInterval=30", "-o", "ServerAliveCountMax=1"},
			nil,
		},
		{
			"default + extras",
			[]string{"MyArg=value"},
			append(defaultArgs, "-o", "MyArg=value"),
			nil,
		},
		{
			"override default + extras",
			[]string{"ConnectTimeout=40", "MyArg=value"},
			[]string{"-o", "ConnectTimeout=40", "-o", "ServerAliveInterval=30", "-o", "ServerAliveCountMax=1", "-o", "MyArg=value"},
			nil,
		},
		{
			"Test user passing in '-o'",
			[]string{"ConnectTimeout=40", "-o", "MyArg=value"},
			[]string{"-o", "ConnectTimeout=40", "-o", "ServerAliveInterval=30", "-o", "ServerAliveCountMax=1", "-o", "MyArg=value"},
			nil,
		},
	}

	for _, test := range testCases {
		t.Run(
			test.description,
			func(t *testing.T) {
				args := mergeSshOpts(test.extraArgs)
				fmt.Println(args)
				assert.ElementsMatch(t, test.expectedArgs, args)
				assert.Condition(t, func() (success bool) {
					for i := 0; i < len(args); i += 2 {
						assert.Equal(t, args[i], "-o")
					}
					return true
				})
			},
		)
	}

}

func TestPreProcessSshOpts(t *testing.T) {
	type testCase struct {
		description  string
		args         []string
		expectedArgs []string
	}

	testCases := []testCase{
		{
			"No values passed in",
			[]string{},
			[]string{},
		},
		{
			"values passed without '--'",
			[]string{"foo=bar", "baz=go"},
			[]string{"--foo=bar", "--baz=go"},
		},
		{
			"with -o specified",
			[]string{"-o", "foo=bar", "baz=go"},
			[]string{"--foo=bar", "--baz=go"},
		},
	}

	for _, test := range testCases {
		t.Run(
			test.description,
			func(t *testing.T) {
				args := preProcessSshOpts(test.args)
				assert.Equal(t, test.expectedArgs, args)
			},
		)
	}
}

func TestCorrectMergedSshOpts(t *testing.T) {
	// We have to do a little extra processing here to convert something that looks like
	// []string{--Arg1, val1, --Arg2, val2, Arg3=val3}
	// to become:
	// []string{-o Arg1=val1 -o Arg2=val2 -o Arg3=val3}

	type testCase struct {
		description  string
		args         []string
		expectedArgs []string
	}

	testCases := []testCase{
		{
			"No args",
			[]string{},
			[]string{},
		},
		{
			"dashed args w values",
			[]string{"--Arg1", "val1", "--Arg2", "val2"},
			[]string{"-o", "Arg1=val1", "-o", "Arg2=val2"},
		},
		{
			"equal-sign args",
			[]string{"--Arg1=val1", "--Arg2=val2"},
			[]string{"-o", "Arg1=val1", "-o", "Arg2=val2"},
		},
		{
			"mixed args",
			[]string{"--Arg1=val1", "--Arg2=val2", "--Arg3", "val3"},
			[]string{"-o", "Arg1=val1", "-o", "Arg2=val2", "-o", "Arg3=val3"},
		},
		{
			"mixed args with single-flag",
			[]string{"--Arg1=val1", "--Arg2=val2", "--Arg3", "val3", "--Arg4"},
			[]string{"-o", "Arg1=val1", "-o", "Arg2=val2", "-o", "Arg3=val3", "-o", "Arg4"},
		},
	}

	for _, test := range testCases {
		t.Run(
			test.description,
			func(t *testing.T) {
				args := correctMergedSshOpts(test.args)
				assert.Equal(t, test.expectedArgs, args)
			},
		)
	}
}

// TODO This test doesn't work because of rsync issues with kerberos
// // Then we can get rid of the hostnameRegex stuff, and just use localhost
// func TestRsyncCopyToDestination(t *testing.T) {
// 	sourceFile, err := os.CreateTemp(os.TempDir(), "testManagedTokens")
// 	if err != nil {
// 		t.Error("Could not create source tempfile")
// 		t.Fail()
// 	}
// 	// defer os.Remove(sourceFile.Name())
// 	destFile := sourceFile.Name() + "_copy"

// 	curUser, err := user.Current()
// 	if err != nil {
// 		t.Error("Could not get current user")
// 		t.Fail()
// 	}
// 	// currentHost, err := os.Hostname()
// 	// if err != nil {
// 	// 	t.Error("Could not get current hostname")
// 	// 	t.Fail()
// 	// }
// 	// hostnameRegex := regexp.MustCompile(`(.+)\.fnal\.gov`)
// 	// matches := hostnameRegex.FindStringSubmatch(currentHost)
// 	// if len(matches) == 0 {
// 	// 	t.Error("Could not parse hostname")
// 	// 	t.Fail()
// 	// }
// 	// hostName := matches[1]

// 	environmentMapper := &service.CommandEnvironment{Krb5ccname: "KRB5CCNAME=" + os.Getenv("KRB5CCNAME")}
// 	fakeRsyncSetup := rsyncSetup{
// 		source:  sourceFile.Name(),
// 		account: curUser.Username,
// 		// node:              hostName,
// 		node:              "localhost",
// 		destination:       destFile,
// 		sshOpts:           "",
// 		EnvironmentMapper: environmentMapper,
// 	}

// 	if err := fakeRsyncSetup.copyToDestination(context.Background()); err != nil {
// 		t.Errorf("Error copying file, %s", err)
// 	}

// 	if _, err := os.Stat(destFile); errors.Is(err, os.ErrNotExist) {
// 		t.Errorf("Test destination file %s does not exist", destFile)
// 	}
// }
