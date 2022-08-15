package utils

import (
	"context"
	"errors"
	"testing"

	"github.com/shreyb/managed-tokens/service"
)

func TestNewSSHFileCopier(t *testing.T) {
	environ := &service.CommandEnvironment{}
	testCopier := NewSSHFileCopier("", "", "", "", "", environ)

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
				// TODO FIX THIS
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
