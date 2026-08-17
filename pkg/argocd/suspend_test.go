package argocd

import (
	"context"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

func controllerStatefulSet(replicas int32) *appsv1.StatefulSet {
	return &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: defaultNamespace,
			Name:      applicationControllerStatefulSet,
		},
		Spec: appsv1.StatefulSetSpec{Replicas: &replicas},
	}
}

func controllerPod(name string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: defaultNamespace,
			Name:      name,
			Labels:    map[string]string{"app.kubernetes.io/name": applicationControllerStatefulSet},
		},
	}
}

func TestSuspendReconciliation(t *testing.T) {
	t.Run("scales the controller to zero and returns once pods are gone", func(t *testing.T) {
		client := fake.NewSimpleClientset(controllerStatefulSet(1))

		if err := suspendReconciliation(context.Background(), client, time.Minute, time.Millisecond); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		sts, err := client.AppsV1().StatefulSets(defaultNamespace).Get(context.Background(), applicationControllerStatefulSet, metav1.GetOptions{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if sts.Spec.Replicas == nil || *sts.Spec.Replicas != 0 {
			t.Errorf("expected controller scaled to 0 replicas, got %v", sts.Spec.Replicas)
		}
	})

	t.Run("waits until the controller pods terminate", func(t *testing.T) {
		client := fake.NewSimpleClientset(controllerStatefulSet(1), controllerPod("argocd-application-controller-0"))

		// The fake clientset runs no controllers, so nothing would ever delete
		// the pod. Return it on the first list and vanish it afterwards to
		// simulate termination while the wait loop is polling.
		listCalls := 0
		client.PrependReactor("list", "pods", func(action k8stesting.Action) (bool, runtime.Object, error) {
			listCalls++
			if listCalls == 1 {
				return true, &corev1.PodList{Items: []corev1.Pod{*controllerPod("argocd-application-controller-0")}}, nil
			}
			return true, &corev1.PodList{}, nil
		})

		if err := suspendReconciliation(context.Background(), client, time.Minute, time.Millisecond); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if listCalls < 2 {
			t.Errorf("expected at least a second poll before returning, got %d list calls", listCalls)
		}
	})

	t.Run("missing controller is benign", func(t *testing.T) {
		client := fake.NewSimpleClientset()

		if err := suspendReconciliation(context.Background(), client, time.Minute, time.Millisecond); err != nil {
			t.Fatalf("expected nil for absent controller, got: %v", err)
		}
	})

	t.Run("returns error when pods persist past the timeout", func(t *testing.T) {
		client := fake.NewSimpleClientset(controllerStatefulSet(1), controllerPod("argocd-application-controller-0"))

		err := suspendReconciliation(context.Background(), client, 0, time.Millisecond)
		if err == nil {
			t.Fatal("expected timeout error, got nil")
		}
	})

	t.Run("respects context cancellation while waiting", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		client := fake.NewSimpleClientset(controllerStatefulSet(1), controllerPod("argocd-application-controller-0"))
		client.PrependReactor("list", "pods", func(action k8stesting.Action) (bool, runtime.Object, error) {
			cancel() // Cancel after the first poll so the wait exits instead of sleeping.
			return true, &corev1.PodList{Items: []corev1.Pod{*controllerPod("argocd-application-controller-0")}}, nil
		})

		if err := suspendReconciliation(ctx, client, time.Minute, time.Millisecond); err == nil {
			t.Fatal("expected error, got nil")
		}
	})
}
