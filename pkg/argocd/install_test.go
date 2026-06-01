package argocd

import (
	"context"
	"errors"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestAddLocalGitopsMount(t *testing.T) {
	tests := []struct {
		name      string
		values    map[string]any
		localPath string
	}{
		{
			name:      "empty values map",
			values:    map[string]any{},
			localPath: "/tmp/nebari-gitops-test",
		},
		{
			name: "existing repoServer section",
			values: map[string]any{
				"repoServer": map[string]any{
					"replicas": 2,
				},
			},
			localPath: "/Users/dev/my-gitops-repo.git",
		},
		{
			name: "existing non-map repoServer gets overwritten",
			values: map[string]any{
				"repoServer": "not-a-map",
			},
			localPath: "/tmp/repo",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			addLocalGitopsMount(context.Background(), tt.values, tt.localPath)

			repoServer, ok := tt.values["repoServer"].(map[string]any)
			if !ok {
				t.Fatal("repoServer should be a map[string]any")
			}

			// Check volumes
			volumes, ok := repoServer["volumes"].([]map[string]any)
			if !ok {
				t.Fatal("volumes should be []map[string]any")
			}
			if len(volumes) != 1 {
				t.Fatalf("expected 1 volume, got %d", len(volumes))
			}
			if volumes[0]["name"] != "local-gitops" {
				t.Errorf("volume name = %v, want local-gitops", volumes[0]["name"])
			}
			hostPath, ok := volumes[0]["hostPath"].(map[string]any)
			if !ok {
				t.Fatal("hostPath should be a map[string]any")
			}
			if hostPath["path"] != tt.localPath {
				t.Errorf("hostPath.path = %v, want %v", hostPath["path"], tt.localPath)
			}
			if hostPath["type"] != "Directory" {
				t.Errorf("hostPath.type = %v, want Directory", hostPath["type"])
			}

			// Check volumeMounts
			volumeMounts, ok := repoServer["volumeMounts"].([]map[string]any)
			if !ok {
				t.Fatal("volumeMounts should be []map[string]any")
			}
			if len(volumeMounts) != 1 {
				t.Fatalf("expected 1 volumeMount, got %d", len(volumeMounts))
			}
			if volumeMounts[0]["name"] != "local-gitops" {
				t.Errorf("volumeMount name = %v, want local-gitops", volumeMounts[0]["name"])
			}
			if volumeMounts[0]["mountPath"] != tt.localPath {
				t.Errorf("volumeMount mountPath = %v, want %v", volumeMounts[0]["mountPath"], tt.localPath)
			}
		})
	}
}

func TestAddLocalGitopsMountPreservesExistingKeys(t *testing.T) {
	values := map[string]any{
		"repoServer": map[string]any{
			"replicas": 2,
			"image":    "custom-image:latest",
		},
	}

	addLocalGitopsMount(context.Background(), values, "/tmp/test-repo")

	repoServer := values["repoServer"].(map[string]any)
	if repoServer["replicas"] != 2 {
		t.Errorf("existing replicas key was overwritten, got %v", repoServer["replicas"])
	}
	if repoServer["image"] != "custom-image:latest" {
		t.Errorf("existing image key was overwritten, got %v", repoServer["image"])
	}
	if _, ok := repoServer["volumes"]; !ok {
		t.Error("volumes key was not added")
	}
	if _, ok := repoServer["volumeMounts"]; !ok {
		t.Error("volumeMounts key was not added")
	}
}

func TestAppendToSlice(t *testing.T) {
	newItem := map[string]any{"name": "new-volume"}

	tests := []struct {
		name     string
		existing any
		wantLen  int
	}{
		{
			name:     "nil existing",
			existing: nil,
			wantLen:  1,
		},
		{
			name:     "empty typed slice",
			existing: []map[string]any{},
			wantLen:  1,
		},
		{
			name: "typed slice with one item",
			existing: []map[string]any{
				{"name": "existing-volume"},
			},
			wantLen: 2,
		},
		{
			name: "untyped slice (from YAML parsing)",
			existing: []any{
				map[string]any{"name": "existing-volume"},
			},
			wantLen: 2,
		},
		{
			name: "untyped slice with multiple items",
			existing: []any{
				map[string]any{"name": "vol1"},
				map[string]any{"name": "vol2"},
			},
			wantLen: 3,
		},
		{
			name:     "invalid type returns just new item",
			existing: "not-a-slice",
			wantLen:  1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := appendToSlice(tt.existing, newItem)
			if len(result) != tt.wantLen {
				t.Errorf("appendToSlice() len = %d, want %d", len(result), tt.wantLen)
			}
			// Last item should always be our new item
			if result[len(result)-1]["name"] != "new-volume" {
				t.Errorf("appendToSlice() last item = %v, want new-volume", result[len(result)-1])
			}
		})
	}
}

func TestAddLocalGitopsMountAppendsToExistingVolumes(t *testing.T) {
	// Test that existing volumes are preserved (not replaced)
	values := map[string]any{
		"repoServer": map[string]any{
			"volumes": []map[string]any{
				{
					"name":     "existing-vol",
					"emptyDir": map[string]any{},
				},
			},
			"volumeMounts": []map[string]any{
				{
					"name":      "existing-vol",
					"mountPath": "/existing",
				},
			},
		},
	}

	addLocalGitopsMount(context.Background(), values, "/tmp/test-repo")

	repoServer := values["repoServer"].(map[string]any)
	volumes := repoServer["volumes"].([]map[string]any)
	volumeMounts := repoServer["volumeMounts"].([]map[string]any)

	if len(volumes) != 2 {
		t.Errorf("expected 2 volumes (existing + new), got %d", len(volumes))
	}
	if len(volumeMounts) != 2 {
		t.Errorf("expected 2 volumeMounts (existing + new), got %d", len(volumeMounts))
	}

	// Check that existing volume is preserved
	if volumes[0]["name"] != "existing-vol" {
		t.Errorf("existing volume was not preserved, got %v", volumes[0]["name"])
	}
	// Check that new volume was appended
	if volumes[1]["name"] != "local-gitops" {
		t.Errorf("new volume name = %v, want local-gitops", volumes[1]["name"])
	}
}

func ptr[T any](v T) *T {
	return &v
}

func TestIsClusterReady(t *testing.T) {
	tests := []struct {
		name  string
		nodes []corev1.Node
		want  bool
	}{
		{
			name:  "empty nodes list",
			nodes: []corev1.Node{},
			want:  false,
		},
		{
			name: "single ready node",
			nodes: []corev1.Node{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "node-1"},
					Status: corev1.NodeStatus{
						Conditions: []corev1.NodeCondition{
							{Type: corev1.NodeReady, Status: corev1.ConditionTrue},
						},
					},
				},
			},
			want: true,
		},
		{
			name: "single not ready node",
			nodes: []corev1.Node{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "node-1"},
					Status: corev1.NodeStatus{
						Conditions: []corev1.NodeCondition{
							{Type: corev1.NodeReady, Status: corev1.ConditionFalse},
						},
					},
				},
			},
			want: false,
		},
		{
			name: "multiple nodes one ready",
			nodes: []corev1.Node{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "node-1"},
					Status: corev1.NodeStatus{
						Conditions: []corev1.NodeCondition{
							{Type: corev1.NodeReady, Status: corev1.ConditionFalse},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "node-2"},
					Status: corev1.NodeStatus{
						Conditions: []corev1.NodeCondition{
							{Type: corev1.NodeReady, Status: corev1.ConditionTrue},
						},
					},
				},
			},
			want: true,
		},
		{
			name: "multiple nodes none ready",
			nodes: []corev1.Node{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "node-1"},
					Status: corev1.NodeStatus{
						Conditions: []corev1.NodeCondition{
							{Type: corev1.NodeReady, Status: corev1.ConditionFalse},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "node-2"},
					Status: corev1.NodeStatus{
						Conditions: []corev1.NodeCondition{
							{Type: corev1.NodeReady, Status: corev1.ConditionUnknown},
						},
					},
				},
			},
			want: false,
		},
		{
			name: "node with multiple conditions",
			nodes: []corev1.Node{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "node-1"},
					Status: corev1.NodeStatus{
						Conditions: []corev1.NodeCondition{
							{Type: corev1.NodeMemoryPressure, Status: corev1.ConditionFalse},
							{Type: corev1.NodeDiskPressure, Status: corev1.ConditionFalse},
							{Type: corev1.NodeReady, Status: corev1.ConditionTrue},
						},
					},
				},
			},
			want: true,
		},
		{
			name: "node with no ready condition",
			nodes: []corev1.Node{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "node-1"},
					Status: corev1.NodeStatus{
						Conditions: []corev1.NodeCondition{
							{Type: corev1.NodeMemoryPressure, Status: corev1.ConditionFalse},
						},
					},
				},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isClusterReady(tt.nodes)
			if got != tt.want {
				t.Errorf("isClusterReady() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestWaitForClusterReadyWithLister(t *testing.T) {
	readyNode := []corev1.Node{
		{
			ObjectMeta: metav1.ObjectMeta{Name: "node-1"},
			Status: corev1.NodeStatus{
				Conditions: []corev1.NodeCondition{
					{Type: corev1.NodeReady, Status: corev1.ConditionTrue},
				},
			},
		},
	}

	notReadyNode := []corev1.Node{
		{
			ObjectMeta: metav1.ObjectMeta{Name: "node-1"},
			Status: corev1.NodeStatus{
				Conditions: []corev1.NodeCondition{
					{Type: corev1.NodeReady, Status: corev1.ConditionFalse},
				},
			},
		},
	}

	t.Run("returns immediately when cluster is ready", func(t *testing.T) {
		callCount := 0
		listNodes := func(ctx context.Context) ([]corev1.Node, error) {
			callCount++
			return readyNode, nil
		}

		ctx := context.Background()
		err := waitForClusterReadyWithLister(ctx, listNodes, 10*time.Second)

		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}
		if callCount != 1 {
			t.Errorf("expected listNodes to be called once, got %d", callCount)
		}
	})

	t.Run("times out when cluster never becomes ready", func(t *testing.T) {
		listNodes := func(ctx context.Context) ([]corev1.Node, error) {
			return notReadyNode, nil
		}

		ctx := context.Background()
		// Use a very short timeout for the test
		err := waitForClusterReadyWithLister(ctx, listNodes, 100*time.Millisecond)

		if err == nil {
			t.Error("expected timeout error, got nil")
		}
	})

	t.Run("retries on list error", func(t *testing.T) {
		callCount := 0
		listNodes := func(ctx context.Context) ([]corev1.Node, error) {
			callCount++
			if callCount < 3 {
				return nil, errors.New("temporary error")
			}
			return readyNode, nil
		}

		ctx := context.Background()
		err := waitForClusterReadyWithLister(ctx, listNodes, 30*time.Second)

		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}
		if callCount < 3 {
			t.Errorf("expected at least 3 calls, got %d", callCount)
		}
	})

	t.Run("respects context cancellation", func(t *testing.T) {
		listNodes := func(ctx context.Context) ([]corev1.Node, error) {
			return notReadyNode, nil
		}

		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		err := waitForClusterReadyWithLister(ctx, listNodes, 10*time.Second)

		if err == nil {
			t.Error("expected error due to cancelled context, got nil")
		}
	})
}

func TestIsDeploymentReady(t *testing.T) {
	tests := []struct {
		name   string
		deploy *appsv1.Deployment
		want   bool
	}{
		{
			name: "deployment ready with all replicas",
			deploy: &appsv1.Deployment{
				Spec:   appsv1.DeploymentSpec{Replicas: ptr(int32(3))},
				Status: appsv1.DeploymentStatus{ReadyReplicas: 3},
			},
			want: true,
		},
		{
			name: "deployment ready with more than required replicas",
			deploy: &appsv1.Deployment{
				Spec:   appsv1.DeploymentSpec{Replicas: ptr(int32(2))},
				Status: appsv1.DeploymentStatus{ReadyReplicas: 3},
			},
			want: true,
		},
		{
			name: "deployment not ready with fewer replicas",
			deploy: &appsv1.Deployment{
				Spec:   appsv1.DeploymentSpec{Replicas: ptr(int32(3))},
				Status: appsv1.DeploymentStatus{ReadyReplicas: 2},
			},
			want: false,
		},
		{
			name: "deployment not ready with zero replicas",
			deploy: &appsv1.Deployment{
				Spec:   appsv1.DeploymentSpec{Replicas: ptr(int32(3))},
				Status: appsv1.DeploymentStatus{ReadyReplicas: 0},
			},
			want: false,
		},
		{
			name: "deployment with nil replicas spec and ready replicas",
			deploy: &appsv1.Deployment{
				Spec:   appsv1.DeploymentSpec{Replicas: nil},
				Status: appsv1.DeploymentStatus{ReadyReplicas: 1},
			},
			want: true,
		},
		{
			name: "deployment with nil replicas spec and no ready replicas",
			deploy: &appsv1.Deployment{
				Spec:   appsv1.DeploymentSpec{Replicas: nil},
				Status: appsv1.DeploymentStatus{ReadyReplicas: 0},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isDeploymentReady(tt.deploy)
			if got != tt.want {
				t.Errorf("isDeploymentReady() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAreDeploymentsReady(t *testing.T) {
	readyDeploy := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "ready-deploy"},
		Spec:       appsv1.DeploymentSpec{Replicas: ptr(int32(1))},
		Status:     appsv1.DeploymentStatus{ReadyReplicas: 1},
	}

	notReadyDeploy := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "not-ready-deploy"},
		Spec:       appsv1.DeploymentSpec{Replicas: ptr(int32(3))},
		Status:     appsv1.DeploymentStatus{ReadyReplicas: 1},
	}

	tests := []struct {
		name    string
		deploys []*appsv1.Deployment
		want    bool
	}{
		{
			name:    "empty list",
			deploys: []*appsv1.Deployment{},
			want:    false,
		},
		{
			name:    "single ready deployment",
			deploys: []*appsv1.Deployment{readyDeploy},
			want:    true,
		},
		{
			name:    "single not ready deployment",
			deploys: []*appsv1.Deployment{notReadyDeploy},
			want:    false,
		},
		{
			name:    "all deployments ready",
			deploys: []*appsv1.Deployment{readyDeploy, readyDeploy},
			want:    true,
		},
		{
			name:    "one deployment not ready",
			deploys: []*appsv1.Deployment{readyDeploy, notReadyDeploy},
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := areDeploymentsReady(tt.deploys)
			if got != tt.want {
				t.Errorf("areDeploymentsReady() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestWaitForArgoCDReadyWithLister(t *testing.T) {
	readyDeploys := []*appsv1.Deployment{
		{
			ObjectMeta: metav1.ObjectMeta{Name: "argocd-server"},
			Spec:       appsv1.DeploymentSpec{Replicas: ptr(int32(1))},
			Status:     appsv1.DeploymentStatus{ReadyReplicas: 1},
		},
		{
			ObjectMeta: metav1.ObjectMeta{Name: "argocd-repo-server"},
			Spec:       appsv1.DeploymentSpec{Replicas: ptr(int32(1))},
			Status:     appsv1.DeploymentStatus{ReadyReplicas: 1},
		},
	}

	notReadyDeploys := []*appsv1.Deployment{
		{
			ObjectMeta: metav1.ObjectMeta{Name: "argocd-server"},
			Spec:       appsv1.DeploymentSpec{Replicas: ptr(int32(1))},
			Status:     appsv1.DeploymentStatus{ReadyReplicas: 0},
		},
	}

	requiredDeployments := []string{"argocd-server", "argocd-repo-server"}

	t.Run("returns immediately when all deployments are ready", func(t *testing.T) {
		callCount := 0
		listDeploys := func(ctx context.Context, names []string) ([]*appsv1.Deployment, error) {
			callCount++
			return readyDeploys, nil
		}

		ctx := context.Background()
		err := waitForArgoCDReadyWithLister(ctx, listDeploys, requiredDeployments, 10*time.Second)

		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}
		if callCount != 1 {
			t.Errorf("expected listDeploys to be called once, got %d", callCount)
		}
	})

	t.Run("times out when deployments never become ready", func(t *testing.T) {
		listDeploys := func(ctx context.Context, names []string) ([]*appsv1.Deployment, error) {
			return notReadyDeploys, nil
		}

		ctx := context.Background()
		err := waitForArgoCDReadyWithLister(ctx, listDeploys, requiredDeployments, 100*time.Millisecond)

		if err == nil {
			t.Error("expected timeout error, got nil")
		}
	})

	t.Run("retries on list error", func(t *testing.T) {
		callCount := 0
		listDeploys := func(ctx context.Context, names []string) ([]*appsv1.Deployment, error) {
			callCount++
			if callCount < 3 {
				return nil, errors.New("temporary error")
			}
			return readyDeploys, nil
		}

		ctx := context.Background()
		err := waitForArgoCDReadyWithLister(ctx, listDeploys, requiredDeployments, 30*time.Second)

		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}
		if callCount < 3 {
			t.Errorf("expected at least 3 calls, got %d", callCount)
		}
	})
}
