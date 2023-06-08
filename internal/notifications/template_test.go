package notifications

import (
	"errors"
	"io"
	"strings"
	"testing"
)

type invalidReader struct{}

func (i invalidReader) Read(p []byte) (n int, err error) {
	return 0, errors.New("There was an error reading the invalidReader object")
}

// TestPrepareMessageFromTemplate checks that prepareMessageFromTemplate properly populates
// a template given by an io.Reader, given a generic struct, and checks that we return
// the proper error in case either of the arguments are invalid.
func TestPrepareMessageFromTemplate(t *testing.T) {

	type expectedResult struct {
		resultString string
		err          error
	}

	type testCase struct {
		description string
		reader      io.Reader
		tmplStruct  any
		expectedResult
	}

	testCases := []testCase{
		{
			"Valid case: Good template, proper tmplStruct",
			strings.NewReader("This is a good template with a valid field: {{.Foo}}"),
			struct{ Foo string }{Foo: "valid value"},
			expectedResult{
				"This is a good template with a valid field: valid value",
				nil,
			},
		},
		{
			"Valid case: No-op template",
			strings.NewReader("Foo"),
			struct{}{},
			expectedResult{
				"Foo",
				nil,
			},
		},
		{
			"Invalid case: Invalid reader - should get errCopyReaderToBuilder",
			invalidReader{},
			struct{}{},
			expectedResult{
				"",
				errCopyReaderToBuilder,
			},
		},
		{
			"Invalid case: Invalid template - should get errParseTemplate",
			strings.NewReader("This is an invalid template with a syntax error HERE {{"),
			struct{}{},
			expectedResult{
				"",
				errParseTemplate,
			},
		},
		{
			"Invalid case: Valid template, invalid tmplStruct args - should get errExecuteTemplate",
			strings.NewReader("This is a good template with a valid field: {{.Foo}}"),
			struct{ Bar string }{Bar: "wrong field here"},
			expectedResult{
				"",
				errExecuteTemplate,
			},
		},
	}

	for _, test := range testCases {
		t.Run(test.description, func(t *testing.T) {
			resultString, err := prepareMessageFromTemplate(test.reader, test.tmplStruct)
			if test.expectedResult.resultString != resultString {
				t.Errorf("Got unexpected return string.  Expected %s, got %s", test.expectedResult.resultString, resultString)
			}
			if test.expectedResult.err == nil && err != nil {
				t.Errorf("Got unexpected return error.  Expected nil, got %s", err)
				return
			}
			if !errors.Is(test.expectedResult.err, err) {
				t.Errorf("Wrong error returned from test.  Expected %v, got %v", test.expectedResult.err, err)
			}
		})
	}
}
