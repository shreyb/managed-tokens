package main

import (
	"os"
	"testing"
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
)

func TestMain(m *testing.M) {
	exeLogger = log.NewEntry(log.New())
	os.Exit(m.Run())
}

func TestInitTimeouts(t *testing.T) {
	type testCase struct {
		description      string
		cmdLineTimeout   string
		expectedTimeouts map[string]time.Duration
	}

	testCases := []testCase{
		{
			"Timeout less than global",
			"10s",
			map[string]time.Duration{
				"global":      300 * time.Second,
				"kerberos":    20 * time.Second,
				"vaultstorer": 10 * time.Second,
			},
		},
		{
			"Timeout greater than global",
			"400s",
			map[string]time.Duration{
				"global":      400 * time.Second,
				"kerberos":    20 * time.Second,
				"vaultstorer": 400 * time.Second,
			},
		},
		{
			"No timeout given - default used",
			"60s", // The flag parser would set this value if no flag is given
			map[string]time.Duration{
				"global":      300 * time.Second,
				"kerberos":    20 * time.Second,
				"vaultstorer": 60 * time.Second,
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			timeoutsReset()
			defer timeoutsReset()

			viper.Set("timeout", tc.cmdLineTimeout)

			err := initTimeouts()

			if err != nil {
				t.Errorf("Expected nil error, but got %s", err)
				return
			}

			assert.Equal(t, tc.expectedTimeouts, timeouts)
		})
	}
}

func timeoutsReset() {
	viper.Reset()
	timeouts = map[string]time.Duration{
		"global":   time.Duration(300 * time.Second),
		"kerberos": time.Duration(20 * time.Second),
	}
}
func TestInitServices(t *testing.T) {
	type testCase struct {
		description      string
		setupFunc        func()
		expectedErrNil   bool
		expectedErr      string
		expecteedService string
	}

	testCases := []testCase{
		{
			"No arguments provided, service not set",
			func() {},
			false,
			"invalid service",
			"",
		},
		{
			"Argument provided, service not set",
			func() {
				pflag.CommandLine.Set("service", "")
			},
			false,
			"invalid service",
			"",
		},
		{
			"Argument provided, service set",
			func() {
				os.Args = []string{"example_service"}
				pflag.CommandLine.Parse(os.Args)
				viper.BindPFlags(pflag.CommandLine)
			},
			true,
			"",
			"example_service",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			viper.Reset()
			pflag.CommandLine = pflag.NewFlagSet(os.Args[0], pflag.ExitOnError)
			tc.setupFunc()
			err := initServices()
			if tc.expectedErrNil {
				assert.NoError(t, err)
			} else {
				assert.EqualError(t, err, tc.expectedErr)
			}
			assert.Equal(t, tc.expecteedService, viper.GetString("service"))
		},
		)
	}
}
