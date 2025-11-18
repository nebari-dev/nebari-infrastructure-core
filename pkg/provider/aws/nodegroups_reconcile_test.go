package aws

import (
	"testing"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
)

// TestOrphanDetection tests the logic for detecting orphaned node groups
func TestOrphanDetection(t *testing.T) {
	tests := []struct {
		name             string
		desiredNGs       map[string]config.AWSNodeGroup
		actualNGs        []NodeGroupState
		expectedOrphans  []string
		expectedToCreate []string
		expectedToUpdate []string
	}{
		{
			name: "no orphans - all match",
			desiredNGs: map[string]config.AWSNodeGroup{
				"general": {Instance: "t3.medium"},
				"compute": {Instance: "c5.2xlarge"},
			},
			actualNGs: []NodeGroupState{
				{
					Name: "test-cluster-ng-general",
					Tags: map[string]string{
						TagNodePool: "general",
					},
				},
				{
					Name: "test-cluster-ng-compute",
					Tags: map[string]string{
						TagNodePool: "compute",
					},
				},
			},
			expectedOrphans:  []string{},
			expectedToCreate: []string{},
			expectedToUpdate: []string{"general", "compute"},
		},
		{
			name: "one orphan - node group removed from config",
			desiredNGs: map[string]config.AWSNodeGroup{
				"general": {Instance: "t3.medium"},
			},
			actualNGs: []NodeGroupState{
				{
					Name: "test-cluster-ng-general",
					Tags: map[string]string{
						TagNodePool: "general",
					},
				},
				{
					Name: "test-cluster-ng-old-pool",
					Tags: map[string]string{
						TagNodePool: "old-pool",
					},
				},
			},
			expectedOrphans:  []string{"test-cluster-ng-old-pool"},
			expectedToCreate: []string{},
			expectedToUpdate: []string{"general"},
		},
		{
			name: "multiple orphans",
			desiredNGs: map[string]config.AWSNodeGroup{
				"general": {Instance: "t3.medium"},
			},
			actualNGs: []NodeGroupState{
				{
					Name: "test-cluster-ng-general",
					Tags: map[string]string{
						TagNodePool: "general",
					},
				},
				{
					Name: "test-cluster-ng-compute",
					Tags: map[string]string{
						TagNodePool: "compute",
					},
				},
				{
					Name: "test-cluster-ng-gpu",
					Tags: map[string]string{
						TagNodePool: "gpu",
					},
				},
			},
			expectedOrphans:  []string{"test-cluster-ng-compute", "test-cluster-ng-gpu"},
			expectedToCreate: []string{},
			expectedToUpdate: []string{"general"},
		},
		{
			name: "new node group to create",
			desiredNGs: map[string]config.AWSNodeGroup{
				"general": {Instance: "t3.medium"},
				"compute": {Instance: "c5.2xlarge"},
			},
			actualNGs: []NodeGroupState{
				{
					Name: "test-cluster-ng-general",
					Tags: map[string]string{
						TagNodePool: "general",
					},
				},
			},
			expectedOrphans:  []string{},
			expectedToCreate: []string{"compute"},
			expectedToUpdate: []string{"general"},
		},
		{
			name: "mix of create, update, and delete",
			desiredNGs: map[string]config.AWSNodeGroup{
				"general": {Instance: "t3.medium"},
				"gpu":     {Instance: "p3.2xlarge"},
			},
			actualNGs: []NodeGroupState{
				{
					Name: "test-cluster-ng-general",
					Tags: map[string]string{
						TagNodePool: "general",
					},
				},
				{
					Name: "test-cluster-ng-compute",
					Tags: map[string]string{
						TagNodePool: "compute",
					},
				},
			},
			expectedOrphans:  []string{"test-cluster-ng-compute"},
			expectedToCreate: []string{"gpu"},
			expectedToUpdate: []string{"general"},
		},
		{
			name: "no actual node groups - create all",
			desiredNGs: map[string]config.AWSNodeGroup{
				"general": {Instance: "t3.medium"},
				"compute": {Instance: "c5.2xlarge"},
				"gpu":     {Instance: "p3.2xlarge"},
			},
			actualNGs:        []NodeGroupState{},
			expectedOrphans:  []string{},
			expectedToCreate: []string{"general", "compute", "gpu"},
			expectedToUpdate: []string{},
		},
		{
			name:       "all actual node groups are orphans",
			desiredNGs: map[string]config.AWSNodeGroup{},
			actualNGs: []NodeGroupState{
				{
					Name: "test-cluster-ng-general",
					Tags: map[string]string{
						TagNodePool: "general",
					},
				},
				{
					Name: "test-cluster-ng-compute",
					Tags: map[string]string{
						TagNodePool: "compute",
					},
				},
			},
			expectedOrphans:  []string{"test-cluster-ng-general", "test-cluster-ng-compute"},
			expectedToCreate: []string{},
			expectedToUpdate: []string{},
		},
		{
			name: "node group without tag - should not crash",
			desiredNGs: map[string]config.AWSNodeGroup{
				"general": {Instance: "t3.medium"},
			},
			actualNGs: []NodeGroupState{
				{
					Name: "test-cluster-ng-general",
					Tags: map[string]string{
						TagNodePool: "general",
					},
				},
				{
					Name: "test-cluster-ng-untagged",
					Tags: map[string]string{
						// Missing TagNodePool
					},
				},
			},
			expectedOrphans:  []string{},
			expectedToCreate: []string{},
			expectedToUpdate: []string{"general"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Build map of actual node groups by node pool name (from tags)
			actualNodeGroups := make(map[string]*NodeGroupState)
			for i := range tt.actualNGs {
				nodePoolName, ok := tt.actualNGs[i].Tags[TagNodePool]
				if ok {
					actualNodeGroups[nodePoolName] = &tt.actualNGs[i]
				}
			}

			// Detect orphans
			orphanedNodeGroups := []string{}
			for nodePoolName, actualNG := range actualNodeGroups {
				if _, exists := tt.desiredNGs[nodePoolName]; !exists {
					orphanedNodeGroups = append(orphanedNodeGroups, actualNG.Name)
				}
			}

			// Check orphan count
			if len(orphanedNodeGroups) != len(tt.expectedOrphans) {
				t.Errorf("expected %d orphans, got %d: %v",
					len(tt.expectedOrphans), len(orphanedNodeGroups), orphanedNodeGroups)
			}

			// Check that expected orphans are detected
			for _, expectedOrphan := range tt.expectedOrphans {
				found := false
				for _, orphan := range orphanedNodeGroups {
					if orphan == expectedOrphan {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expected orphan %s not found in %v", expectedOrphan, orphanedNodeGroups)
				}
			}

			// Detect new node groups to create
			toCreate := []string{}
			for nodeGroupName := range tt.desiredNGs {
				if _, exists := actualNodeGroups[nodeGroupName]; !exists {
					toCreate = append(toCreate, nodeGroupName)
				}
			}

			// Check create count
			if len(toCreate) != len(tt.expectedToCreate) {
				t.Errorf("expected %d to create, got %d: %v",
					len(tt.expectedToCreate), len(toCreate), toCreate)
			}

			// Detect node groups to update
			toUpdate := []string{}
			for nodeGroupName := range tt.desiredNGs {
				if _, exists := actualNodeGroups[nodeGroupName]; exists {
					toUpdate = append(toUpdate, nodeGroupName)
				}
			}

			// Check update count
			if len(toUpdate) != len(tt.expectedToUpdate) {
				t.Errorf("expected %d to update, got %d: %v",
					len(tt.expectedToUpdate), len(toUpdate), toUpdate)
			}
		})
	}
}

// TestReconcileNodeGroup_ScalingChanges tests detection of scaling configuration changes
func TestReconcileNodeGroup_ScalingChanges(t *testing.T) {
	tests := []struct {
		name         string
		desired      config.AWSNodeGroup
		actual       *NodeGroupState
		expectUpdate bool
		description  string
	}{
		{
			name: "no changes needed",
			desired: config.AWSNodeGroup{
				Instance: "t3.medium",
				MinNodes: 1,
				MaxNodes: 3,
			},
			actual: &NodeGroupState{
				Name:          "test-ng",
				MinSize:       1,
				MaxSize:       3,
				InstanceTypes: []string{"t3.medium"},
			},
			expectUpdate: false,
			description:  "scaling config matches",
		},
		{
			name: "min nodes changed",
			desired: config.AWSNodeGroup{
				Instance: "t3.medium",
				MinNodes: 2,
				MaxNodes: 3,
			},
			actual: &NodeGroupState{
				Name:          "test-ng",
				MinSize:       1,
				MaxSize:       3,
				InstanceTypes: []string{"t3.medium"},
			},
			expectUpdate: true,
			description:  "min nodes increased from 1 to 2",
		},
		{
			name: "max nodes changed",
			desired: config.AWSNodeGroup{
				Instance: "t3.medium",
				MinNodes: 1,
				MaxNodes: 5,
			},
			actual: &NodeGroupState{
				Name:          "test-ng",
				MinSize:       1,
				MaxSize:       3,
				InstanceTypes: []string{"t3.medium"},
			},
			expectUpdate: true,
			description:  "max nodes increased from 3 to 5",
		},
		{
			name: "both min and max changed",
			desired: config.AWSNodeGroup{
				Instance: "t3.medium",
				MinNodes: 2,
				MaxNodes: 10,
			},
			actual: &NodeGroupState{
				Name:          "test-ng",
				MinSize:       1,
				MaxSize:       3,
				InstanceTypes: []string{"t3.medium"},
			},
			expectUpdate: true,
			description:  "both scaling limits changed",
		},
		{
			name: "zero values use defaults (1 and 3)",
			desired: config.AWSNodeGroup{
				Instance: "t3.medium",
				MinNodes: 0,
				MaxNodes: 0,
			},
			actual: &NodeGroupState{
				Name:          "test-ng",
				MinSize:       1,
				MaxSize:       3,
				InstanceTypes: []string{"t3.medium"},
			},
			expectUpdate: false,
			description:  "zero values default to 1 and 3",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Simulate the scaling check logic
			desiredMinSize := tt.desired.MinNodes
			desiredMaxSize := tt.desired.MaxNodes
			if desiredMinSize == 0 {
				desiredMinSize = 1
			}
			if desiredMaxSize == 0 {
				desiredMaxSize = 3
			}

			updateNeeded := tt.actual.MinSize != desiredMinSize || tt.actual.MaxSize != desiredMaxSize

			if updateNeeded != tt.expectUpdate {
				t.Errorf("%s: expected updateNeeded=%v, got %v",
					tt.description, tt.expectUpdate, updateNeeded)
			}
		})
	}
}

// TestCheckLabelsUpdate_Logic tests the label update detection logic
func TestCheckLabelsUpdate_Logic(t *testing.T) {
	provider := &Provider{}

	tests := []struct {
		name         string
		desired      config.AWSNodeGroup
		actual       *NodeGroupState
		expectUpdate bool
	}{
		{
			name:    "labels match",
			desired: config.AWSNodeGroup{},
			actual: &NodeGroupState{
				Labels: map[string]string{
					"node-group":               "general",
					"nic.nebari.dev/node-pool": "general",
				},
				Tags: map[string]string{
					TagNodePool: "general",
				},
			},
			expectUpdate: false,
		},
		{
			name:    "missing node-group label",
			desired: config.AWSNodeGroup{},
			actual: &NodeGroupState{
				Labels: map[string]string{
					"nic.nebari.dev/node-pool": "general",
				},
				Tags: map[string]string{
					TagNodePool: "general",
				},
			},
			expectUpdate: true,
		},
		{
			name:    "incorrect node-group label value",
			desired: config.AWSNodeGroup{},
			actual: &NodeGroupState{
				Labels: map[string]string{
					"node-group":               "wrong-value",
					"nic.nebari.dev/node-pool": "general",
				},
				Tags: map[string]string{
					TagNodePool: "general",
				},
			},
			expectUpdate: true,
		},
		{
			name:    "no labels exist",
			desired: config.AWSNodeGroup{},
			actual: &NodeGroupState{
				Labels: map[string]string{},
				Tags: map[string]string{
					TagNodePool: "general",
				},
			},
			expectUpdate: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			updateNeeded := provider.checkLabelsUpdate(tt.desired, tt.actual)

			if updateNeeded != tt.expectUpdate {
				t.Errorf("expected updateNeeded=%v, got %v", tt.expectUpdate, updateNeeded)
			}
		})
	}
}

// TestCheckTaintsUpdate_Logic tests the taint update detection logic
func TestCheckTaintsUpdate_Logic(t *testing.T) {
	provider := &Provider{}

	tests := []struct {
		name         string
		desired      config.AWSNodeGroup
		actual       *NodeGroupState
		expectUpdate bool
	}{
		{
			name: "taints match",
			desired: config.AWSNodeGroup{
				Taints: []config.Taint{
					{Key: "nvidia.com/gpu", Value: "true", Effect: "NoSchedule"},
				},
			},
			actual: &NodeGroupState{
				Taints: []Taint{
					{Key: "nvidia.com/gpu", Value: "true", Effect: "NoSchedule"},
				},
			},
			expectUpdate: false,
		},
		{
			name: "different taint count",
			desired: config.AWSNodeGroup{
				Taints: []config.Taint{
					{Key: "nvidia.com/gpu", Value: "true", Effect: "NoSchedule"},
					{Key: "workload", Value: "batch", Effect: "NoExecute"},
				},
			},
			actual: &NodeGroupState{
				Taints: []Taint{
					{Key: "nvidia.com/gpu", Value: "true", Effect: "NoSchedule"},
				},
			},
			expectUpdate: true,
		},
		{
			name: "taint key mismatch",
			desired: config.AWSNodeGroup{
				Taints: []config.Taint{
					{Key: "nvidia.com/gpu", Value: "true", Effect: "NoSchedule"},
				},
			},
			actual: &NodeGroupState{
				Taints: []Taint{
					{Key: "amd.com/gpu", Value: "true", Effect: "NoSchedule"},
				},
			},
			expectUpdate: true,
		},
		{
			name: "taint value mismatch",
			desired: config.AWSNodeGroup{
				Taints: []config.Taint{
					{Key: "nvidia.com/gpu", Value: "true", Effect: "NoSchedule"},
				},
			},
			actual: &NodeGroupState{
				Taints: []Taint{
					{Key: "nvidia.com/gpu", Value: "false", Effect: "NoSchedule"},
				},
			},
			expectUpdate: true,
		},
		{
			name: "taint effect mismatch",
			desired: config.AWSNodeGroup{
				Taints: []config.Taint{
					{Key: "nvidia.com/gpu", Value: "true", Effect: "NoSchedule"},
				},
			},
			actual: &NodeGroupState{
				Taints: []Taint{
					{Key: "nvidia.com/gpu", Value: "true", Effect: "NoExecute"},
				},
			},
			expectUpdate: true,
		},
		{
			name: "no taints on both sides",
			desired: config.AWSNodeGroup{
				Taints: []config.Taint{},
			},
			actual: &NodeGroupState{
				Taints: []Taint{},
			},
			expectUpdate: false,
		},
		{
			name: "desired has no taints, actual has taints",
			desired: config.AWSNodeGroup{
				Taints: []config.Taint{},
			},
			actual: &NodeGroupState{
				Taints: []Taint{
					{Key: "nvidia.com/gpu", Value: "true", Effect: "NoSchedule"},
				},
			},
			expectUpdate: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			updateNeeded := provider.checkTaintsUpdate(tt.desired, tt.actual)

			if updateNeeded != tt.expectUpdate {
				t.Errorf("expected updateNeeded=%v, got %v", tt.expectUpdate, updateNeeded)
			}
		})
	}
}
