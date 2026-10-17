// Package storageclass holds provider-agnostic helpers for managing which
// StorageClass is the cluster default.
package storageclass

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/status"
)

// DefaultAnnotation is the Kubernetes-defined key that marks a StorageClass
// as the cluster default. Multiple StorageClasses set to "true" is treated
// as undefined behavior: the DefaultStorageClass admission controller picks
// the newest and emits a warning.
const DefaultAnnotation = "storageclass.kubernetes.io/is-default-class"

// EnsureSoleDefault demotes any StorageClass other than keep that still
// carries the default-class annotation, leaving keep as the cluster's only
// default StorageClass. It does not annotate keep itself.
func EnsureSoleDefault(ctx context.Context, client kubernetes.Interface, keep string) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "storageclass.EnsureSoleDefault")
	defer span.End()

	span.SetAttributes(attribute.String("keep", keep))

	classes, err := client.StorageV1().StorageClasses().List(ctx, metav1.ListOptions{})
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to list StorageClasses: %w", err)
	}

	patch := annotationPatch("false")

	for i := range classes.Items {
		sc := &classes.Items[i]
		if sc.Name == keep {
			continue
		}
		if sc.Annotations[DefaultAnnotation] != "true" {
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

// MarkDefaultIfNone annotates name as the cluster default StorageClass, but
// only when no StorageClass is already the default. A default chosen by the
// cluster admin (or by another installer such as Longhorn) is left alone.
// It returns true when name is the default after the call, either because
// it already was or because this call marked it.
func MarkDefaultIfNone(ctx context.Context, client kubernetes.Interface, name string) (bool, error) {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "storageclass.MarkDefaultIfNone")
	defer span.End()

	span.SetAttributes(attribute.String("storage_class_name", name))

	classes, err := client.StorageV1().StorageClasses().List(ctx, metav1.ListOptions{})
	if err != nil {
		span.RecordError(err)
		return false, fmt.Errorf("failed to list StorageClasses: %w", err)
	}

	found := false
	for i := range classes.Items {
		sc := &classes.Items[i]
		if sc.Name == name {
			found = true
		}
		if sc.Annotations[DefaultAnnotation] != "true" {
			continue
		}
		if sc.Name == name {
			span.SetAttributes(attribute.Bool("is_default", true))
			return true, nil
		}

		span.SetAttributes(
			attribute.Bool("is_default", false),
			attribute.String("existing_default", sc.Name),
		)
		status.Send(ctx, status.NewUpdate(status.LevelInfo,
			fmt.Sprintf("StorageClass %q is already the cluster default; leaving %q non-default", sc.Name, name)).
			WithResource("storageclass").
			WithAction("skipped").
			WithMetadata("name", name))
		return false, nil
	}

	if !found {
		err := fmt.Errorf("StorageClass %q not found", name)
		span.RecordError(err)
		return false, err
	}

	if _, err := client.StorageV1().StorageClasses().Patch(ctx, name, types.MergePatchType, annotationPatch("true"), metav1.PatchOptions{}); err != nil {
		span.RecordError(err)
		return false, fmt.Errorf("failed to set default-class annotation on StorageClass %q: %w", name, err)
	}

	span.SetAttributes(attribute.Bool("is_default", true))
	status.Send(ctx, status.NewUpdate(status.LevelInfo,
		fmt.Sprintf("Marked StorageClass %q as the cluster default", name)).
		WithResource("storageclass").
		WithAction("updating").
		WithMetadata("name", name))

	return true, nil
}

func annotationPatch(value string) []byte {
	return []byte(`{"metadata":{"annotations":{"` + DefaultAnnotation + `":"` + value + `"}}}`)
}
