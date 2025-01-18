package main

import (
	"errors"
	"testing"
	"time"

	"github.com/fermitools/managed-tokens/internal/worker"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
)

func TestGetAndCheckRetryInfoFromConfig(t *testing.T) {
	tests := []struct {
		name          string
		workerType    worker.WorkerType
		checkTimeout  time.Duration
		numRetries    uint
		retrySleep    time.Duration
		expectedError error
	}{
		{
			name:          "Valid configuration",
			workerType:    worker.GetKerberosTicketsWorkerType,
			checkTimeout:  10 * time.Second,
			numRetries:    3,
			retrySleep:    2 * time.Second,
			expectedError: nil,
		},
		{
			name:          "Invalid configuration - timeout less than retry duration",
			workerType:    worker.GetKerberosTicketsWorkerType,
			checkTimeout:  5 * time.Second,
			numRetries:    3,
			retrySleep:    2 * time.Second,
			expectedError: errors.New("timeout is less than the time it would take to retry all attempts.  Will stop now"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			viper.Reset()
			defer viper.Reset()
			viper.Set("workerType."+workerTypeToConfigString(tt.workerType)+".numRetries", tt.numRetries)
			viper.Set("workerType."+workerTypeToConfigString(tt.workerType)+".retrySleep", tt.retrySleep.String())

			numRetries, retrySleep, err := getAndCheckRetryInfoFromConfig(tt.workerType, tt.checkTimeout)
			if tt.expectedError != nil {
				assert.EqualError(t, err, tt.expectedError.Error())
				assert.Equal(t, uint(0), numRetries)
				assert.Equal(t, time.Duration(0), retrySleep)
				return
			}

			assert.NoError(t, err)
			assert.Equal(t, tt.numRetries, numRetries)
			assert.Equal(t, tt.retrySleep, retrySleep)
		})
	}
}
