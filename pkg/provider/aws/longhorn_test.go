package aws

import "testing"

func TestAWSConfigLonghornDefaults(t *testing.T) {
	t.Run("nil Longhorn block defaults to enabled (AWS opt-in)", func(t *testing.T) {
		cfg := &Config{}
		if !cfg.LonghornEnabled() {
			t.Error("LonghornEnabled() = false on AWS config with nil Longhorn, want true")
		}
		if cfg.LonghornReplicaCount() == 0 {
			t.Error("LonghornReplicaCount() = 0, want non-zero default")
		}
	})

	t.Run("explicit disabled honours user", func(t *testing.T) {
		disabled := false
		cfg := &Config{Longhorn: newLonghornCfgWithEnabled(&disabled)}
		if cfg.LonghornEnabled() {
			t.Error("LonghornEnabled() = true with explicit Enabled=false")
		}
	})

	t.Run("explicit replica count overrides default", func(t *testing.T) {
		cfg := &Config{Longhorn: newLonghornCfgWithReplicas(5)}
		if got := cfg.LonghornReplicaCount(); got != 5 {
			t.Errorf("LonghornReplicaCount() = %d, want 5", got)
		}
	})
}
