package worker

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/shreyb/managed-tokens/internal/service"
)

func TestGetVaultTokenStoreHoldoff(t *testing.T) {
	testService := service.NewService("test_service")

	type testCase struct {
		description     string
		setKeyValFunc   func(*Config) error
		expectedHoldoff bool
		expectedOk      bool
	}

	testCases := []testCase{
		{
			"Nothing set",
			func(c *Config) error { return nil },
			false,
			false,
		},
		{
			"Valid setting",
			SetSupportedExtrasKeyValue(VaultTokenStoreHoldoff, true),
			true,
			true,
		},
		{
			"Invalid setting",
			SetSupportedExtrasKeyValue(VaultTokenStoreHoldoff, 12345),
			false,
			false,
		},
	}

	for _, test := range testCases {
		t.Run(
			test.description,
			func(t *testing.T) {
				config, _ := NewConfig(testService, test.setKeyValFunc)
				holdoff, ok := GetVaultTokenStoreHoldoff(config)
				assert.Equal(t, test.expectedHoldoff, holdoff)
				assert.Equal(t, test.expectedOk, ok)
			},
		)
	}
}

func TestSetVaultTokenStoreHoldoff(t *testing.T) {
	config, _ := NewConfig(service.NewService("test_service"), SetVaultTokenStoreHoldoff())
	val, ok := config.Extras[VaultTokenStoreHoldoff]
	assert.True(t, ok, "VaultTokenStoreHoldoff assignment not made:  Key not present in Extras map")
	valBool, ok := val.(bool)
	if !ok {
		t.Error("Stored value failed type check")
	}
	assert.True(t, valBool, "Stored value should be true.  Got false instead")
}

func TestGetDefaultRoleFileDestinationTemplateValueFromExtras(t *testing.T) {
	testService := service.NewService("test_service")

	type testCase struct {
		description      string
		setKeyValFunc    func(*Config) error
		expectedTemplate string
		expectedOk       bool
	}

	testCases := []testCase{
		{
			"Nothing set",
			func(c *Config) error { return nil },
			"",
			false,
		},
		{
			"Valid setting",
			SetSupportedExtrasKeyValue(DefaultRoleFileDestinationTemplate, "foobar"),
			"foobar",
			true,
		},
		{
			"Invalid setting",
			SetSupportedExtrasKeyValue(DefaultRoleFileDestinationTemplate, 12345),
			"",
			false,
		},
	}

	for _, test := range testCases {
		t.Run(
			test.description,
			func(t *testing.T) {
				// config, _ := NewConfig(service.NewService("test_service"), SetSupportedExtrasKeyValue(DefaultRoleFileDestinationTemplate, "foobar"))
				config, _ := NewConfig(testService, test.setKeyValFunc)
				val, ok := GetDefaultRoleFileDestinationTemplateValueFromExtras(config)
				assert.Equal(t, test.expectedTemplate, val)
				assert.Equal(t, test.expectedOk, ok)
			},
		)
	}
}

func TestGetFileCopierOptionsFromExtras(t *testing.T) {
	testService := service.NewService("test_service")

	type testCase struct {
		description   string
		setKeyValFunc func(*Config) error
		expectedOpts  string
		expectedOk    bool
	}

	testCases := []testCase{
		{
			"Default case",
			func(c *Config) error { return nil },
			defaultFileCopierOpts,
			true,
		},
		{
			"Valid opts stored",
			SetSupportedExtrasKeyValue(FileCopierOptions, "thisisvalid --opts"),
			"thisisvalid --opts",
			true,
		},
		{
			"Invalid opts stored - wrong type",
			SetSupportedExtrasKeyValue(FileCopierOptions, 12345),
			"",
			false,
		},
	}

	for _, test := range testCases {
		t.Run(
			test.description,
			func(t *testing.T) {
				config, _ := NewConfig(testService, test.setKeyValFunc)
				val, ok := GetFileCopierOptionsFromExtras(config)
				assert.Equal(t, test.expectedOpts, val)
				assert.Equal(t, test.expectedOk, ok)
			},
		)
	}
}
