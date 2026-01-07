package kubernetes

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/status"
)

// CreateNamespace creates a namespace if it doesn't already exist (idempotent)
func CreateNamespace(ctx context.Context, client *kubernetes.Clientset, namespace string) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "kubernetes.CreateNamespace")
	defer span.End()

	span.SetAttributes(
		attribute.String("namespace", namespace),
	)

	status.Send(ctx, status.NewUpdate(status.LevelProgress, fmt.Sprintf("Creating namespace: %s", namespace)).
		WithResource("namespace").
		WithAction("creating").
		WithMetadata("namespace", namespace))

	// Check if namespace already exists
	_, err := client.CoreV1().Namespaces().Get(ctx, namespace, metav1.GetOptions{})
	if err == nil {
		// Namespace already exists
		status.Send(ctx, status.NewUpdate(status.LevelInfo, fmt.Sprintf("Namespace %s already exists", namespace)).
			WithResource("namespace").
			WithAction("exists").
			WithMetadata("namespace", namespace))
		return nil
	}

	// If error is not "NotFound", return it
	if !errors.IsNotFound(err) {
		span.RecordError(err)
		return fmt.Errorf("failed to check namespace existence: %w", err)
	}

	// Create the namespace
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: namespace,
		},
	}

	_, err = client.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to create namespace: %w", err)
	}

	status.Send(ctx, status.NewUpdate(status.LevelSuccess, fmt.Sprintf("Created namespace: %s", namespace)).
		WithResource("namespace").
		WithAction("created").
		WithMetadata("namespace", namespace))

	return nil
}
