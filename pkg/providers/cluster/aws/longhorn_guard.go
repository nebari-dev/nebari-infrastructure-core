package aws

import (
	"context"
	"errors"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/service/eks"
	ekstypes "github.com/aws/aws-sdk-go-v2/service/eks/types"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/status"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/storage/longhorn"
)

// checkUnmanagedLonghorn fails when the cluster already runs the Longhorn
// Helm release but the config does not enable Longhorn. Call it only when
// Longhorn is disabled. Longhorn used to be installed by default on AWS, so a
// cluster deployed before it became opt-in can have no longhorn block. A
// deploy would then stop managing Longhorn (its webhook security group rules,
// the storage nodes' disk label, its manifests) while its volumes still hold
// data. A cluster that doesn't exist yet passes. Any other failure to check is
// returned, since the deploy can't tell whether it is safe. installed reports
// whether the release is present (longhorn.IsInstalled outside tests).
func checkUnmanagedLonghorn(ctx context.Context, client EKSClient, clusterName, region string, installed func(context.Context, []byte) (bool, error)) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "aws.checkUnmanagedLonghorn")
	defer span.End()
	span.SetAttributes(
		attribute.String("cluster_name", clusterName),
		attribute.String(attrKeyRegion, region),
	)

	if _, err := client.DescribeCluster(ctx, &eks.DescribeClusterInput{Name: &clusterName}); err != nil {
		var notFound *ekstypes.ResourceNotFoundException
		if errors.As(err, &notFound) {
			span.SetAttributes(attribute.Bool("cluster_exists", false))
			return nil
		}
		span.RecordError(err)
		return fmt.Errorf("failed to check for an existing Longhorn install: %w", err)
	}
	span.SetAttributes(attribute.Bool("cluster_exists", true))

	kubeconfigBytes, err := fetchEKSKubeconfig(ctx, client, clusterName, region)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to check for an existing Longhorn install: %w", err)
	}

	found, err := installed(ctx, kubeconfigBytes)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to check for an existing Longhorn install: %w", err)
	}
	span.SetAttributes(attribute.Bool("longhorn_installed", found))
	if !found {
		return nil
	}

	err = fmt.Errorf("cluster %q runs Longhorn (Helm release %q in namespace %q) but the config does not enable it. "+
		"Longhorn is no longer installed by default on AWS. To keep using it, add `longhorn: {enabled: true}` under cluster.aws. "+
		"To stop using it, move your volumes off Longhorn and uninstall it before deploying",
		clusterName, longhorn.ReleaseName, longhorn.Namespace)
	span.RecordError(err)
	return err
}

// uninstallLonghornBeforeDestroy runs the Longhorn uninstall that Destroy
// does before tofu destroy, whatever the config says, since a cluster from the
// old AWS default runs Longhorn without a longhorn block. uninstall is
// longhorn.Uninstall outside tests, which returns early when there is no
// release. A missing kubeconfig only skips the step with a warning. A failed
// uninstall is returned as err (abort the destroy) without force, or as
// forced (keep going and report it at the end) with force.
func uninstallLonghornBeforeDestroy(ctx context.Context, kubeconfigBytes []byte, kubeconfigErr error, force bool, uninstall func(context.Context, []byte) error) (forced error, err error) {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "aws.uninstallLonghornBeforeDestroy")
	defer span.End()
	span.SetAttributes(attribute.Bool("force", force))

	if kubeconfigErr != nil {
		status.Send(ctx, status.NewUpdate(status.LevelWarning, fmt.Sprintf("Skipping Longhorn uninstall — kubeconfig unavailable: %v", kubeconfigErr)).
			WithResource("longhorn").WithAction("uninstalling"))
		return nil, nil
	}

	if uerr := uninstall(ctx, kubeconfigBytes); uerr != nil {
		span.RecordError(uerr)
		if !force {
			return nil, fmt.Errorf("failed to uninstall Longhorn: %w", uerr)
		}
		status.Send(ctx, status.NewUpdate(status.LevelWarning, fmt.Sprintf("Longhorn uninstall failed, continuing with --force: %v", uerr)).
			WithResource("longhorn").WithAction("uninstalling"))
		return fmt.Errorf("uninstall Longhorn: %w", uerr), nil
	}
	return nil, nil
}
