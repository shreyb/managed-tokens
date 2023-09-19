package worker

import (
	"testing"

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
	val, ok := config.Extras[DefaultRoleFileDestinationTemplate]
	if !ok {
		t.Error("DefaultRoleFileDestinationTemplate assignment not made:  Key not present in Extras map")
	}
	if valString, ok := val.(string); !ok {
		t.Error("Stored value failed type check")
	} else if valString != "foobar" {
		t.Errorf("Stored value should be foobar.  Got %s instead", valString)
	}
}
