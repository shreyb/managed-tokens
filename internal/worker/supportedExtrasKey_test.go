package worker

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/shreyb/managed-tokens/internal/service"
)

func TestGetVaultTokenStoreHoldoff(t *testing.T) {
	config, _ := NewConfig(service.NewService("test_service"), SetSupportedExtrasKeyValue(VaultTokenStoreHoldoff, true))
	holdoff, ok := GetVaultTokenStoreHoldoff(config)
	if !ok {
		t.Errorf("Should have a holdoff value")
		return
	}
	if !holdoff {
		t.Errorf("Expected to have a holdoff value of true.  Got false")
	}
}

func TestSetVaultTokenStoreHoldoff(t *testing.T) {
	config, _ := NewConfig(service.NewService("test_service"), SetVaultTokenStoreHoldoff())
	val, ok := config.Extras[VaultTokenStoreHoldoff]
	if !ok {
		t.Error("VaultTokenStoreHoldoff assignment not made:  Key not present in Extras map")
	}
	if valBool, ok := val.(bool); !ok {
		t.Error("Stored value failed type check")
	} else if !valBool {
		t.Error("Stored value should be true.  Got false instead")
	}
}

func TestGetDefaultRoleFileDestinationTemplateValueFromExtras(t *testing.T) {
	config, _ := NewConfig(service.NewService("test_service"), SetSupportedExtrasKeyValue(DefaultRoleFileDestinationTemplate, "foobar"))
	val, ok := GetDefaultRoleFileDestinationTemplateValueFromExtras(config)
	assert.True(t, ok)
	assert.Equal(t, "foobar", val)
}

func TestGetFileCopierOptionsFromExtras(t *testing.T) {
	config, _ := NewConfig(service.NewService("test_service"), SetSupportedExtrasKeyValue(FileCopierOptions, "--testopts --moretestopts"))
	val, ok := GetFileCopierOptionsFromExtras(config)
	assert.True(t, ok)
	assert.Equal(t, "--testopts --moretestopts", val)
}