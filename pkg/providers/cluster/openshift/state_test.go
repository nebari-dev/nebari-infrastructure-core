package openshift

import (
	"strings"
	"testing"
)

func TestGenerateBucketNameDeterministicAndPrefixed(t *testing.T) {
	a, err := generateBucketName("123456789012", "us-east-1", "proj")
	if err != nil {
		t.Fatalf("generateBucketName: %v", err)
	}
	b, err := generateBucketName("123456789012", "us-east-1", "proj")
	if err != nil {
		t.Fatalf("generateBucketName: %v", err)
	}
	if a != b {
		t.Errorf("not deterministic: %q != %q", a, b)
	}
	if !strings.HasPrefix(a, "nic-ocp-tfstate-") {
		t.Errorf("bucket %q missing nic-ocp-tfstate- prefix (must not collide with aws provider buckets)", a)
	}
	if len(a) > maxBucketNameLength {
		t.Errorf("bucket %q exceeds %d chars", a, maxBucketNameLength)
	}

	// A different account must hash to a different suffix.
	c, _ := generateBucketName("999999999999", "us-east-1", "proj")
	if a == c {
		t.Errorf("different account IDs produced identical bucket name %q", a)
	}
}

func TestGenerateBucketNameTooLong(t *testing.T) {
	long := strings.Repeat("x", 60)
	if _, err := generateBucketName("123456789012", "us-east-1", long); err == nil {
		t.Error("expected error for over-long bucket name, got nil")
	}
}

func TestStateKey(t *testing.T) {
	if got := stateKey("proj"); got != "proj/terraform.tfstate" {
		t.Errorf("stateKey = %q, want proj/terraform.tfstate", got)
	}
}
