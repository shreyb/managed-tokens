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

package utils

import (
	"errors"
	"testing"
	"text/template"

	"github.com/cornfeedhobo/pflag"
	"github.com/stretchr/testify/assert"
)

// TestCheckForExecutables checks that CheckForExecutables properly returns and populates the map for standard linux executables
func TestCheckForExecutables(t *testing.T) {
	exeMap := map[string]string{
		"true":  "",
		"false": "",
		"ls":    "",
		"cd":    "",
	}

	if err := CheckForExecutables(exeMap); err != nil {
		t.Error(err)
	}
}

// TestCheckRunningUserNotRoot ensures that CheckRunningUserNotRoot can run without issue
func TestCheckRunningUserNotRoot(t *testing.T) {
	if err := CheckRunningUserNotRoot(); err != nil {
		t.Error(err)
	}
}

// TestTemplateToCommand runs TemplateToCommand on a series of templates and arguments to that template, and
// ensures that either the correct args are returned, or the proper error is returned
func TestTemplateToCommand(t *testing.T) {
	type testCase struct {
		description  string
		template     *template.Template
		templateArgs any
		expectedArgs []string
		expectedErr  error
	}

	testCases := []testCase{
		{
			"Simple template, no args",
			template.Must(template.New("testGoodEmpty").Parse("")),
			struct{}{},
			[]string{},
			nil,
		},
		{
			"Simple template, args",
			template.Must(template.New("testGoodFilled").Parse("this is a template called {{.TemplateName}}")),
			struct{ TemplateName string }{TemplateName: "testGoodFilled"},
			[]string{"this", "is", "a", "template", "called", "testGoodFilled"},
			nil,
		},
		{
			"Simple template, but should fail execution of template",
			template.Must(template.New("testBadExecution").Parse("This template should not execute due to missing {{.Args}}")),
			struct{}{},
			[]string{},
			&TemplateExecuteError{"Could not execute template"},
		},
		{
			"Simple template, but should fail shlex",
			template.Must(template.New("testBadShlex").Parse("\"this is a bad template.  It shouldn't work with shlex rules")),
			struct{}{},
			[]string{},
			&TemplateArgsError{"Could not get arguments from template"},
		},
	}

	for _, test := range testCases {
		t.Run(
			test.description,
			func(t *testing.T) {
				args, err := TemplateToCommand(test.template, test.templateArgs)
				switch err == nil {
				case true:
					if test.expectedErr != nil {
						t.Errorf("Got wrong error.  Expected %v, but got nil instead.", test.expectedErr)
					}
				case false:
					if test.expectedErr == nil {
						t.Errorf("Got wrong error.  Expected nil, but got %v instead.", err)
					} else {
						if errors.Is(err, test.expectedErr) {
							t.Errorf("Got wrong error.  Expected %v, got %v", test.expectedErr, err)
						}
					}
				}

				if len(args) != len(test.expectedArgs) {
					t.Error("Args do not match (length of args and expectedArgs differ)")
					t.FailNow()
				}
				for index, arg := range args {
					if arg != test.expectedArgs[index] {
						t.Errorf("Args do not match at index %d.  Got %s, expected %s", index, arg, test.expectedArgs[index])
					}
				}
			},
		)
	}
}

func TestIsSliceSubslice(t *testing.T) {
	type testCase struct {
		description    string
		sliceOne       []any
		sliceTwo       []any
		expectedResult bool
	}

	testCases := []testCase{
		{
			"Slice one is subslice of slice two",
			[]any{1, 2, 3, 4, 5},
			[]any{1, 2, 3, 4, 5, 6, 7, 8},
			true,
		},
		{
			"Slice one is the same as slice two",
			[]any{1, 2, 3, 4, 5, 6, 7, 8},
			[]any{1, 2, 3, 4, 5, 6, 7, 8},
			true,
		},
		{
			"Slice one is not subslice of slice two",
			[]any{1, 2, 3},
			[]any{4, 5, 6},
			false,
		},
		{
			"Slice one is subslice of slice two, but different order",
			[]any{1, 2, 3},
			[]any{4, 5, 6, 2, 3, 1},
			true,
		},
		{
			"Slice one is subslice of slice two, string",
			[]any{"foo", "bar"},
			[]any{"foo", "bar", "baz"},
			true,
		},
		{
			"Slice one is not subslice of slice two, string",
			[]any{"foo", "bar"},
			[]any{"baz"},
			false,
		},
		{
			"Slice one is subslice of slice two, custom type",
			[]any{
				struct {
					a string
					b string
				}{
					"foo",
					"bar",
				},
				struct {
					a string
					b string
				}{
					"baz",
					"bar",
				},
			},
			[]any{
				struct {
					a string
					b string
				}{
					"foo",
					"bar",
				},
				struct {
					a string
					b string
				}{
					"baz",
					"bar",
				},
				struct {
					a string
					b string
				}{
					"foo",
					"baz",
				},
			},
			true,
		},
		{
			"Slice one is not subslice of slice two, custom type",
			[]any{
				struct {
					a string
					b string
				}{
					"foo",
					"bar",
				},
				struct {
					a string
					b string
				}{
					"baz",
					"bar",
				},
			},
			[]any{
				struct {
					a string
					b string
				}{
					"foo",
					"baz",
				},
			},
			false,
		},
		{
			"Mixed types",
			[]any{1, 2, "foo"},
			[]any{1, 2, "foo", "bar"},
			true,
		},
	}

	for _, test := range testCases {
		t.Run(
			test.description,
			func(t *testing.T) {
				if result := IsSliceSubSlice(test.sliceOne, test.sliceTwo); result != test.expectedResult {
					t.Errorf("Got wrong result.  Expected %t, got %t", test.expectedResult, result)
				}
			},
		)
	}
}

func TestMergeCmdArgs(t *testing.T) {
	newTestFlagSet := func() *pflag.FlagSet {
		fs := pflag.NewFlagSet("ping flags", pflag.ContinueOnError)

		// Load our default set.  Note that I'm using these names as a workaround as pflag doesn't provide support for shorthand flags only, which is a bummer
		fs.StringS("pingFlagW", "W", "5", "")
		fs.StringS("pingFlagc", "c", "1", "")

		return fs
	}
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
				sanitizedArgs, err := MergeCmdArgs(newTestFlagSet(), test.extraArgs)
				assert.Equal(t, test.expectedArgs, sanitizedArgs)
				if test.err == nil {
					assert.Nil(t, err)
				}
			},
		)
	}
}
