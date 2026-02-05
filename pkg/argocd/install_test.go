package argocd

import (
	"context"
	"errors"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

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
