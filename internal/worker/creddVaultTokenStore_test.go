package worker

import (
	"errors"
	"fmt"
	"os"
	"os/user"
	"path"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGetServiceTokenForCreddLocation(t *testing.T) {
	curUser, _ := user.Current()
	uid := curUser.Uid
	credd := "mycredd"
	service := "my_service"

	type testCase struct {
		tokenRootPath string
		expected      string
	}

	testCases := []testCase{
		{
			"/path/to/rootdir/",
			fmt.Sprintf("/path/to/rootdir/vt_u%s-%s-%s", uid, credd, service),
		},
		{
			"/path/to/rootdir2/",
			fmt.Sprintf("/path/to/rootdir2/vt_u%s-%s-%s", uid, credd, service),
		},
	}

	for idx, test := range testCases {
		t.Run(
			fmt.Sprintf("test%d", idx),
			func(t *testing.T) {
				assert.Equal(t, test.expected, getServiceTokenForCreddLocation(test.tokenRootPath, "my_service", "mycredd"))
			},
		)
	}
}

func TestGetCondorVaultTokenLocation(t *testing.T) {
	currentUser, _ := user.Current()
	uid := currentUser.Uid
	serviceName := "myService"
	expectedResult := fmt.Sprintf("/tmp/vt_u%s-%s", uid, serviceName)
	result := getCondorVaultTokenLocation(serviceName)
	assert.Equal(t, expectedResult, result)
}

func TestBackupCondorVaultToken(t *testing.T) {
	service := "my_service"
	condorVaultTokenLocation := getCondorVaultTokenLocation(service)

	if cleanupFunc := stashCondorVaultTokenFileIfExists(t, service); cleanupFunc != nil {
		t.Cleanup(cleanupFunc)
	} else {
		t.Cleanup(func() { os.Remove(condorVaultTokenLocation) })
	}

	type testCase struct {
		description                       string
		setupFunc                         func() (cleanupFunc func())
		expectedRestorePriorTokenFuncNoop bool
		expectedErrNil                    bool
	}

	testCases := []testCase{
		{
			"No condor vault token exists prior",
			func() func() { return nil },
			true,
			true,
		},
		{
			"condor vault token exists prior",
			func() func() {
				_, err := os.Create(condorVaultTokenLocation)
				if err != nil {
					t.FailNow()
				}
				return func() { os.Remove(condorVaultTokenLocation) }
			},
			false,
			true,
		},
		{
			"condor vault token exists prior, but can't be statted for some reason",
			func() func() {
				_, err := os.Create(condorVaultTokenLocation)
				if err != nil {
					t.FailNow()
				}
				os.Setenv("TMPDIR", "/dev/null")
				return func() {
					os.Remove(condorVaultTokenLocation)
					os.Unsetenv("TMPDIR")
				}
			},
			true,
			false,
		},
		// TODO Need to have real test where we can't make the tempfile
	}

	for _, test := range testCases {
		t.Run(
			test.description,
			func(t *testing.T) {
				if cleanupFunc := test.setupFunc(); cleanupFunc != nil {
					t.Cleanup(cleanupFunc)
				}
				restorePriorTokenFunc, err := backupCondorVaultToken(service)
				if test.expectedRestorePriorTokenFuncNoop {
					assert.Nil(t, restorePriorTokenFunc())
				} else {
					if assert.NotNil(t, restorePriorTokenFunc) {
						t.Cleanup(func() { restorePriorTokenFunc() })
					}
				}
				if test.expectedErrNil {
					assert.NoError(t, err)
					assert.NoFileExists(t, condorVaultTokenLocation)
				} else {
					assert.Error(t, err)
				}
			},
		)
	}

}

func TestStageStoredTokenFile(t *testing.T) {
	service := "my_service"
	credd := "mycredd"

	condorVaultTokenLocation := getCondorVaultTokenLocation(service)
	if cleanupFunc := stashCondorVaultTokenFileIfExists(t, service); cleanupFunc != nil {
		t.Cleanup(cleanupFunc)
	} else {
		t.Cleanup(func() { os.Remove(condorVaultTokenLocation) })
	}

	testTokenContents := []byte("thisisatesttoken")
	type testCase struct {
		description string
		setupFunc   func() (cleanupFunc func())
		expectedErr error
	}
	tempDir := t.TempDir()
	tokenRootPath := tempDir
	serviceCreddTokenStorePath := getServiceTokenForCreddLocation(tokenRootPath, service, credd)

	testCases := []testCase{
		{
			"no prior credd service token",
			func() func() { return nil },
			errNoServiceCreddToken,
		},
		{
			"prior credd service token exists",
			func() func() {
				err := os.WriteFile(serviceCreddTokenStorePath, testTokenContents, 0644)
				if err != nil {
					t.FailNow()
				}
				return func() { os.Remove(serviceCreddTokenStorePath) }
			},
			nil,
		},
	}

	for _, test := range testCases {
		t.Run(
			test.description,
			func(t *testing.T) {
				if cleanupFunc := test.setupFunc(); cleanupFunc != nil {
					t.Cleanup(cleanupFunc)
				}
				err := stageStoredTokenFile(tokenRootPath, service, credd)
				if test.expectedErr != nil {
					assert.ErrorIs(t, err, test.expectedErr)
				} else {
					assert.NoError(t, err)
				}
				if !errors.Is(test.expectedErr, errNoServiceCreddToken) {
					assert.FileExists(t, condorVaultTokenLocation)
					contents, err := os.ReadFile(condorVaultTokenLocation)
					if err != nil {
						t.FailNow()
					}
					assert.Equal(t, testTokenContents, contents)
				}
			},
		)
	}
}

func TestStoreServiceTokenForCreddFile(t *testing.T) {
	service := "my_service"
	credd := "mycredd"

	tempDir := t.TempDir()
	defaultTokenRootPath := tempDir

	condorVaultTokenLocation := getCondorVaultTokenLocation(service)
	serviceCreddTokenStorePath := getServiceTokenForCreddLocation(defaultTokenRootPath, service, credd)
	testTokenContents := []byte("thisisatesttoken")

	if cleanupFunc := stashCondorVaultTokenFileIfExists(t, service); cleanupFunc != nil {
		t.Cleanup(cleanupFunc)
	} else {
		t.Cleanup(func() { os.Remove(condorVaultTokenLocation) })
	}

	type testCase struct {
		description    string
		setupFunc      func() (cleanupFunc func())
		tokenRootPath  string
		expectedErrNil bool
	}

	testCases := []testCase{
		{
			"Normal case",
			func() (cleanupFunc func()) {
				err := os.WriteFile(condorVaultTokenLocation, testTokenContents, 0644)
				if err != nil {
					t.FailNow()
				}
				return func() { os.Remove(condorVaultTokenLocation) }
			},
			defaultTokenRootPath,
			true,
		},
		{
			"Error moving file",
			func() (cleanupFunc func()) {
				err := os.WriteFile(condorVaultTokenLocation, testTokenContents, 0644)
				if err != nil {
					t.FailNow()
				}
				return func() { os.Remove(condorVaultTokenLocation) }
			},
			"/this/dir/does/not/exist",
			false,
		},
	}

	for _, test := range testCases {
		t.Run(
			test.description,
			func(t *testing.T) {
				if cleanupFunc := test.setupFunc; cleanupFunc != nil {
					t.Cleanup(cleanupFunc())
				}
				err := storeServiceTokenForCreddFile(test.tokenRootPath, service, credd)
				if !test.expectedErrNil {
					assert.Error(t, err)
					return
				}
				assert.NoError(t, err)
				assert.FileExists(t, serviceCreddTokenStorePath)
				contents, err := os.ReadFile(serviceCreddTokenStorePath)
				if err != nil {
					t.FailNow()
				}
				assert.Equal(t, testTokenContents, contents)
			},
		)
	}
}

func TestMoveFileCrossDevice(t *testing.T) {
	// Base case
	curUser, _ := user.Current()
	srcFile, _ := os.CreateTemp(path.Join("/", "run", "user", curUser.Uid), "managed_tokens_test_src")
	srcFile.WriteString("This is a test string")
	defer srcFile.Close()
	t.Cleanup(func() { os.Remove(srcFile.Name()) })

	destDir := t.TempDir()
	destFile, _ := os.CreateTemp(destDir, "managed_tokens_test_dest")
	defer destFile.Close()

	// Setup for source file that we can't remove after successfully copying it
	newSrcDir := t.TempDir()
	cantRemoveSrcFile, _ := os.CreateTemp(newSrcDir, "managed_tokens_test")
	os.Chmod(newSrcDir, 0555)
	os.Chmod(cantRemoveSrcFile.Name(), 0644)
	defer os.Chmod(newSrcDir, 0755)

	type testCase struct {
		description string
		src         string
		dest        string
		expectedErr error
	}

	testCases := []testCase{
		{
			"Base case",
			srcFile.Name(),
			destFile.Name(),
			nil,
		},
		{
			"Nonexistent source error",
			"blahblah",
			destFile.Name(),
			&os.PathError{},
		},
		{
			"Can't open file to write",
			srcFile.Name(),
			path.Join(os.DevNull, "blah"),
			&os.PathError{},
		},
		{
			"Not being able to remove the source file after successful copy",
			cantRemoveSrcFile.Name(),
			destFile.Name(),
			errCannotRemoveFile,
		},
	}

	var err1 *os.PathError
	for _, test := range testCases {
		t.Run(
			test.description,
			func(t *testing.T) {
				err := moveFileCrossDevice(test.src, test.dest)
				if test.expectedErr == nil {
					assert.NoError(t, err)
				} else {
					// First check if our test expected error is a placeholder for an error type
					if errors.As(test.expectedErr, &err1) {
						assert.ErrorAs(t, err, &err1)
					} else {
						// Now we see if the error matches the test result error
						assert.ErrorIs(t, err, test.expectedErr)
					}
				}
			},
		)
	}

	// TODO Can we mock issue copying data?

}

// If we have a vault token file in the condor location, move it now
func stashCondorVaultTokenFileIfExists(t *testing.T, service string) (restoreTokenFunc func()) {
	condorVaultTokenLocation := getCondorVaultTokenLocation(service)
	if _, err := os.Stat(condorVaultTokenLocation); !errors.Is(err, os.ErrNotExist) {
		stageFile, err := os.CreateTemp(os.TempDir(), "managed_tokens_test_stage")
		if err != nil {
			t.FailNow()
		} else {
			if err = os.Rename(condorVaultTokenLocation, stageFile.Name()); err != nil {
				t.FailNow()
			}
			return func() { os.Rename(stageFile.Name(), condorVaultTokenLocation) }
		}
	}
	return
}
