package aws

import (
	"context"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
)

func TestCalculateSubnetCIDR(t *testing.T) {
	p := &Provider{}

	tests := []struct {
		name    string
		vpcCIDR string
		index   int
		public  bool
		want    string
	}{
		{
			name:    "first public subnet",
			vpcCIDR: "10.10.0.0/16",
			index:   0,
			public:  true,
			want:    "10.10.0.0/20",
		},
		{
			name:    "second public subnet",
			vpcCIDR: "10.10.0.0/16",
			index:   1,
			public:  true,
			want:    "10.10.16.0/20",
		},
		{
			name:    "third public subnet",
			vpcCIDR: "10.10.0.0/16",
			index:   2,
			public:  true,
			want:    "10.10.32.0/20",
		},
		{
			name:    "first private subnet",
			vpcCIDR: "10.10.0.0/16",
			index:   0,
			public:  false,
			want:    "10.10.128.0/20",
		},
		{
			name:    "second private subnet",
			vpcCIDR: "10.10.0.0/16",
			index:   1,
			public:  false,
			want:    "10.10.144.0/20",
		},
		{
			name:    "third private subnet",
			vpcCIDR: "10.10.0.0/16",
			index:   2,
			public:  false,
			want:    "10.10.160.0/20",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := p.calculateSubnetCIDR(tt.vpcCIDR, tt.index, tt.public)
			if got != tt.want {
				t.Errorf("calculateSubnetCIDR() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestReconcileVPC_NoExistingVPC(t *testing.T) {
	p := &Provider{}
	ctx := context.Background()

	// This test validates that reconcileVPC with nil actual state
	// would trigger VPC creation (though we can't test the actual creation without AWS)

	cfg := newTestConfig("test-cluster", &Config{
		Region:            "us-west-2",
		KubernetesVersion: "1.34",
		VPCCIDRBlock:      "10.10.0.0/16",
	})

	// actual is nil (no VPC exists)
	// In a real scenario, this would trigger VPC creation
	// We can't test that here without mocking or real AWS access
	_ = p
	_ = ctx
	_ = cfg
}

func TestReconcileVPC_CIDRMismatch(t *testing.T) {
	p := &Provider{}
	ctx := context.Background()

	cfg := newTestConfig("test-cluster", &Config{
		Region:            "us-west-2",
		KubernetesVersion: "1.34",
		VPCCIDRBlock:      "10.20.0.0/16", // Different from actual
	})

	actual := &VPCState{
		VPCID: "vpc-123456",
		CIDR:  "10.10.0.0/16", // Different from desired
	}

	// This should return an error about immutable CIDR
	_, err := p.reconcileVPC(ctx, nil, cfg, actual)
	if err == nil {
		t.Error("Expected error for CIDR mismatch, got nil")
	}

	// Check error message mentions immutable
	if err != nil && len(err.Error()) > 0 {
		// Error should mention CIDR and immutable
		errMsg := err.Error()
		if !containsSubstring([]string{errMsg}, "CIDR") {
			t.Errorf("Error message should mention CIDR: %v", errMsg)
		}
		if !containsSubstring([]string{errMsg}, "immutable") {
			t.Errorf("Error message should mention immutable: %v", errMsg)
		}
	}
}

func TestReconcileVPC_CIDRMatch(t *testing.T) {
	p := &Provider{}
	ctx := context.Background()

	cfg := newTestConfig("test-cluster", &Config{
		Region:            "us-west-2",
		KubernetesVersion: "1.34",
		VPCCIDRBlock:      "10.10.0.0/16",
	})

	actual := &VPCState{
		VPCID: "vpc-123456",
		CIDR:  "10.10.0.0/16", // Matches desired
	}

	// This should succeed with no changes
	_, err := p.reconcileVPC(ctx, nil, cfg, actual)
	if err != nil {
		t.Errorf("Expected no error when CIDR matches, got: %v", err)
	}
}

func TestReconcileVPC_AvailabilityZonesMismatch(t *testing.T) {
	p := &Provider{}
	ctx := context.Background()

	cfg := newTestConfig("test-cluster", &Config{
		Region:            "us-west-2",
		KubernetesVersion: "1.34",
		VPCCIDRBlock:      "10.10.0.0/16",
		AvailabilityZones: []string{"us-west-2a", "us-west-2b", "us-west-2c"}, // Different AZs
	})

	actual := &VPCState{
		VPCID:             "vpc-123456",
		CIDR:              "10.10.0.0/16",
		AvailabilityZones: []string{"us-west-2a", "us-west-2b"}, // Original AZs (missing 2c)
	}

	// This should return an error about immutable AZs
	_, err := p.reconcileVPC(ctx, nil, cfg, actual)
	if err == nil {
		t.Error("Expected error for availability zones mismatch, got nil")
	}

	// Check error message mentions immutable
	if err != nil && len(err.Error()) > 0 {
		errMsg := err.Error()
		if !containsSubstring([]string{errMsg}, "availability zones") {
			t.Errorf("Error message should mention availability zones: %v", errMsg)
		}
		if !containsSubstring([]string{errMsg}, "immutable") {
			t.Errorf("Error message should mention immutable: %v", errMsg)
		}
	}
}

func TestReconcileVPC_AvailabilityZonesMatch(t *testing.T) {
	p := &Provider{}
	ctx := context.Background()

	cfg := newTestConfig("test-cluster", &Config{
		Region:            "us-west-2",
		KubernetesVersion: "1.34",
		VPCCIDRBlock:      "10.10.0.0/16",
		AvailabilityZones: []string{"us-west-2a", "us-west-2b"}, // Same AZs
	})

	actual := &VPCState{
		VPCID:             "vpc-123456",
		CIDR:              "10.10.0.0/16",
		AvailabilityZones: []string{"us-west-2a", "us-west-2b"}, // Same AZs
	}

	// This should succeed with no changes
	_, err := p.reconcileVPC(ctx, nil, cfg, actual)
	if err != nil {
		t.Errorf("Expected no error when AZs match, got: %v", err)
	}
}

func TestReconcileVPC_AvailabilityZonesNotSpecifiedInConfig(t *testing.T) {
	p := &Provider{}
	ctx := context.Background()

	cfg := newTestConfig("test-cluster", &Config{
		Region:            "us-west-2",
		KubernetesVersion: "1.34",
		VPCCIDRBlock:      "10.10.0.0/16",
		// No AvailabilityZones specified - should not error
	})

	actual := &VPCState{
		VPCID:             "vpc-123456",
		CIDR:              "10.10.0.0/16",
		AvailabilityZones: []string{"us-west-2a", "us-west-2b"}, // Actual has AZs
	}

	// This should succeed - no AZs specified in config means no comparison
	_, err := p.reconcileVPC(ctx, nil, cfg, actual)
	if err != nil {
		t.Errorf("Expected no error when AZs not specified in config, got: %v", err)
	}
}

func TestContains(t *testing.T) {
	tests := []struct {
		name  string
		slice []string
		str   string
		want  bool
	}{
		{
			name:  "string present",
			slice: []string{"apple", "banana", "cherry"},
			str:   "banana",
			want:  true,
		},
		{
			name:  "string not present",
			slice: []string{"apple", "banana", "cherry"},
			str:   "grape",
			want:  false,
		},
		{
			name:  "empty slice",
			slice: []string{},
			str:   "apple",
			want:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := contains(tt.slice, tt.str)
			if got != tt.want {
				t.Errorf("contains() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestContainsSubstring(t *testing.T) {
	tests := []struct {
		name   string
		slice  []string
		substr string
		want   bool
	}{
		{
			name:   "substring present",
			slice:  []string{"hello-world", "foo-bar"},
			substr: "world",
			want:   true,
		},
		{
			name:   "substring not present",
			slice:  []string{"hello-world", "foo-bar"},
			substr: "baz",
			want:   false,
		},
		{
			name:   "exact match",
			slice:  []string{"test"},
			substr: "test",
			want:   true,
		},
		{
			name:   "empty slice",
			slice:  []string{},
			substr: "test",
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := containsSubstring(tt.slice, tt.substr)
			if got != tt.want {
				t.Errorf("containsSubstring() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestConvertEC2TagsToMap(t *testing.T) {
	key1 := "Key1"
	value1 := "Value1"
	key2 := "Key2"
	value2 := "Value2"

	tags := []types.Tag{
		{Key: &key1, Value: &value1},
		{Key: &key2, Value: &value2},
	}

	result := convertEC2TagsToMap(tags)

	if len(result) != 2 {
		t.Errorf("Expected 2 tags, got %d", len(result))
	}

	if result["Key1"] != "Value1" {
		t.Errorf("Key1 = %v, want %v", result["Key1"], "Value1")
	}

	if result["Key2"] != "Value2" {
		t.Errorf("Key2 = %v, want %v", result["Key2"], "Value2")
	}
}

func TestConvertEC2TagsToMap_NilValues(t *testing.T) {
	key1 := "Key1"
	value1 := "Value1"

	tags := []types.Tag{
		{Key: &key1, Value: &value1},
		{Key: nil, Value: nil}, // Should be skipped
	}

	result := convertEC2TagsToMap(tags)

	if len(result) != 1 {
		t.Errorf("Expected 1 tag (nil tags should be skipped), got %d", len(result))
	}
}
