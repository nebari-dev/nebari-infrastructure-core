package longhorn

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/status"
)

// defaultStorageClassAnnotation is the Kubernetes-defined key that marks a
// StorageClass as the cluster default. Multiple StorageClasses set to "true"
// is treated as undefined behavior — the DefaultStorageClass admission
// controller picks the newest and emits a warning. Hetzner ships
// hcloud-volumes pre-annotated, so installing Longhorn with
// persistence.defaultClass=true leaves two defaults until we demote the
// previous one.
const defaultStorageClassAnnotation = "storageclass.kubernetes.io/is-default-class"

// ensureSoleDefaultStorageClass demotes any StorageClass other than keep
// that still carries the default-class annotation, leaving keep as the
// cluster's only default StorageClass.
func ensureSoleDefaultStorageClass(ctx context.Context, kubeconfigBytes []byte, keep string) error {
	client, err := newK8sClient(kubeconfigBytes)
	if err != nil {
		return fmt.Errorf("failed to create Kubernetes client: %w", err)
	}
	return ensureSoleDefaultStorageClassWithClient(ctx, client, keep)
}

// ensureSoleDefaultStorageClassWithClient is the testable inner form of
// ensureSoleDefaultStorageClass; takes a kubernetes.Interface so unit tests
// can inject a fake client.
func ensureSoleDefaultStorageClassWithClient(ctx context.Context, client kubernetes.Interface, keep string) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "longhorn.ensureSoleDefaultStorageClass")
	defer span.End()

	classes, err := client.StorageV1().StorageClasses().List(ctx, metav1.ListOptions{})
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to list StorageClasses: %w", err)
	}

	patch := []byte(`{"metadata":{"annotations":{"` + defaultStorageClassAnnotation + `":"false"}}}`)

	for i := range classes.Items {
		sc := &classes.Items[i]
		if sc.Name == keep {
			continue
		}
		if sc.Annotations[defaultStorageClassAnnotation] != "true" {
			continue
		}

		if _, err := client.StorageV1().StorageClasses().Patch(ctx, sc.Name, types.MergePatchType, patch, metav1.PatchOptions{}); err != nil {
			span.RecordError(err)
			return fmt.Errorf("failed to clear default-class annotation on StorageClass %q: %w", sc.Name, err)
		}

		status.Send(ctx, status.NewUpdate(status.LevelInfo,
			fmt.Sprintf("Cleared default-class annotation from previous default StorageClass %q", sc.Name)).
			WithResource("storageclass").
			WithAction("updating"))
	}

	return nil
}
