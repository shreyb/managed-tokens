package utils

import (
	"errors"
	"testing"
	"text/template"
)

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

func TestCheckRunningUserNotRoot(t *testing.T) {
	if err := CheckRunningUserNotRoot(); err != nil {
		t.Error(err)
	}
}

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
