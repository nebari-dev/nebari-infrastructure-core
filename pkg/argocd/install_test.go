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
