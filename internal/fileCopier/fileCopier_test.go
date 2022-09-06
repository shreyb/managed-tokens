package fileCopier

import (
	"context"
	"errors"
	"testing"

	"github.com/shreyb/managed-tokens/internal/environment"
)

// TestNewSSHFileCopier asserts that the type of the returned object from NewSSHFileCopier
// is an *rsyncSetup
func TestNewSSHFileCopier(t *testing.T) {
	environ := &environment.CommandEnvironment{}
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

// TODO This test doesn't work because of rsync issues with kerberos
// // TODO Change the hostname code so that we don't assume fnal.gov, or at least fill it in in the executable and config file
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
