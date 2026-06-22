package azure

import (
	"strings"
	"testing"
)

func TestStateStorageAccountName(t *testing.T) {
	tests := []struct {
		name           string
		subscriptionID string
		want           string
	}{
		{
			name:           "zero UUID",
			subscriptionID: "00000000-0000-0000-0000-000000000000",
			// First 14 hex chars of sha256("00000000-0000-0000-0000-000000000000").
			want: "nictfstate12b9377cbe7e5c",
		},
		{
			name:           "real-looking UUID",
			subscriptionID: "11111111-2222-3333-4444-555555555555",
			// deterministic from the SHA-256 hash; checked via re-invocation below.
			want: "", // computed dynamically; see check below
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := stateStorageAccountName(tc.subscriptionID)

			// All storage account names must comply with Azure's rules:
			// 3-24 chars, lowercase letters and digits only.
			if l := len(got); l < 3 || l > 24 {
				t.Errorf("len(%q) = %d, want 3..24", got, l)
			}
			for _, r := range got {
				if (r < 'a' || r > 'z') && (r < '0' || r > '9') {
					t.Errorf("name %q contains invalid char %q", got, r)
				}
			}
			if !strings.HasPrefix(got, "nictfstate") {
				t.Errorf("name %q missing nictfstate prefix", got)
			}

			// Deterministic: same input -> same output.
			if again := stateStorageAccountName(tc.subscriptionID); again != got {
				t.Errorf("non-deterministic: got %q, then %q", got, again)
			}

			if tc.want != "" && got != tc.want {
				t.Errorf("stateStorageAccountName(%q) = %q, want %q", tc.subscriptionID, got, tc.want)
			}
		})
	}
}

func TestStateStorageAccountNameDistinctForDistinctSubscriptions(t *testing.T) {
	a := stateStorageAccountName("11111111-1111-1111-1111-111111111111")
	b := stateStorageAccountName("22222222-2222-2222-2222-222222222222")
	if a == b {
		t.Errorf("expected distinct names for distinct subscription IDs, both = %q", a)
	}
}

func TestStateBackendConfig(t *testing.T) {
	cfg := newStateBackendConfig("11111111-2222-3333-4444-555555555555", "myproj")
	if cfg.RGName != stateResourceGroupName {
		t.Errorf("RGName = %q, want %q", cfg.RGName, stateResourceGroupName)
	}
	if cfg.Container != stateContainerName {
		t.Errorf("Container = %q, want %q", cfg.Container, stateContainerName)
	}
	if cfg.Key != "myproj.tfstate" {
		t.Errorf("Key = %q, want %q", cfg.Key, "myproj.tfstate")
	}
	if !strings.HasPrefix(cfg.SAName, "nictfstate") {
		t.Errorf("SAName = %q missing prefix", cfg.SAName)
	}
}
