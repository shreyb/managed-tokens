package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTimeoutKeyString(t *testing.T) {
	tests := []struct {
		key      timeoutKey
		expected string
	}{
		{timeoutGlobal, "global"},
		{timeoutKerberos, "kerberos"},
		{timeoutVaultStorer, "vaultstorer"},
		{timeoutPing, "ping"},
		{timeoutPush, "push"},
		{invalidTimeoutKey, ""},
		{invalidTimeoutKey + 42, ""},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if got := tt.key.String(); got != tt.expected {
				t.Errorf("timeoutKey.String() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestGettimeoutKeyFromString(t *testing.T) {
	tests := []struct {
		input    string
		expected timeoutKey
		ok       bool
	}{
		{"global", timeoutGlobal, true},
		{"kerberos", timeoutKerberos, true},
		{"vaultstorer", timeoutVaultStorer, true},
		{"ping", timeoutPing, true},
		{"push", timeoutPush, true},
		{"invalid", invalidTimeoutKey, false},
		{"", invalidTimeoutKey, false},
		{"randomKey", invalidTimeoutKey, false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, ok := getTimeoutKeyFromString(tt.input)
			assert.Equal(t, tt.expected, got)
			assert.Equal(t, tt.ok, ok)
		})
	}
}
