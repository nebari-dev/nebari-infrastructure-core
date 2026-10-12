package aws

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/fsx"
	fsxtypes "github.com/aws/aws-sdk-go-v2/service/fsx/types"
	"github.com/hashicorp/terraform-exec/tfexec"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/status"
)

// FSxClient defines the FSx operations needed to find the child volumes that
// would block deleting the FSx for OpenZFS filesystem.
type FSxClient interface {
	DescribeVolumes(ctx context.Context, params *fsx.DescribeVolumesInput, optFns ...func(*fsx.Options)) (*fsx.DescribeVolumesOutput, error)
}

func newFSxClient(ctx context.Context, region string) (FSxClient, error) {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "aws.newFSxClient")
	defer span.End()
	span.SetAttributes(attribute.String(attrKeyRegion, region))

	cfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(region))
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}
	return fsx.NewFromConfig(cfg), nil
}

// fsxOpenZFSFilesystem identifies the deployed filesystem from tofu outputs.
// Both IDs are empty when FSx was never created, for example on a first-time
// dry run against a local backend or when fsx_openzfs is disabled.
type fsxOpenZFSFilesystem struct {
	ID           string
	RootVolumeID string
}

// fsxOpenZFSFilesystemFromOutputs reads the filesystem and root volume IDs
// from tofu outputs. A missing or null output yields an empty ID rather than
// an error, since there is then nothing for destroy to check.
func fsxOpenZFSFilesystemFromOutputs(outputs map[string]tfexec.OutputMeta) (fsxOpenZFSFilesystem, error) {
	var fs fsxOpenZFSFilesystem
	for key, dst := range map[string]*string{
		"fsx_openzfs_id":             &fs.ID,
		"fsx_openzfs_root_volume_id": &fs.RootVolumeID,
	} {
		out, ok := outputs[key]
		if !ok || len(out.Value) == 0 {
			continue
		}
		if err := json.Unmarshal(out.Value, dst); err != nil {
			return fsxOpenZFSFilesystem{}, fmt.Errorf("failed to unmarshal %s: %w", key, err)
		}
	}
	return fs, nil
}

// listFSxOpenZFSChildVolumes returns every volume on the filesystem except its
// root volume, formatted as "<id> (<name>)". These are the volumes the CSI
// driver, or anyone else, created outside tofu.
func listFSxOpenZFSChildVolumes(ctx context.Context, client FSxClient, fs fsxOpenZFSFilesystem) ([]string, error) {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "aws.listFSxOpenZFSChildVolumes")
	defer span.End()
	span.SetAttributes(attribute.String("fsx_openzfs_id", fs.ID))

	var children []string
	paginator := fsx.NewDescribeVolumesPaginator(client, &fsx.DescribeVolumesInput{
		Filters: []fsxtypes.VolumeFilter{{
			Name:   fsxtypes.VolumeFilterNameFileSystemId,
			Values: []string{fs.ID},
		}},
	})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			span.RecordError(err)
			return nil, fmt.Errorf("failed to describe FSx volumes for %s: %w", fs.ID, err)
		}
		for _, v := range page.Volumes {
			id := aws.ToString(v.VolumeId)
			if id == fs.RootVolumeID {
				continue
			}
			children = append(children, fmt.Sprintf("%s (%s)", id, aws.ToString(v.Name)))
		}
	}

	span.SetAttributes(attribute.Int("child_volume_count", len(children)))
	return children, nil
}

// checkFSxOpenZFSChildVolumes fails when the filesystem still has child
// volumes and delete_child_volumes_on_destroy is off. FSx refuses to delete
// such a filesystem, so tofu destroy would otherwise fail partway, after load
// balancers and Longhorn are already gone. Run it before any teardown step.
func checkFSxOpenZFSChildVolumes(ctx context.Context, client FSxClient, cfg *Config, fs fsxOpenZFSFilesystem) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "aws.checkFSxOpenZFSChildVolumes")
	defer span.End()

	if !cfg.FSxOpenZFSEnabled() || cfg.FSxOpenZFS.DeleteChildVolumesOnDestroy || fs.ID == "" {
		return nil
	}

	children, err := listFSxOpenZFSChildVolumes(ctx, client, fs)
	if err != nil {
		span.RecordError(err)
		return err
	}
	if len(children) == 0 {
		return nil
	}

	err = fmt.Errorf("FSx for OpenZFS filesystem %s still has %d child volume(s): %s. "+
		"FSx will not delete the filesystem while they exist. "+
		"Delete the PersistentVolumeClaims that use the %q StorageClass while the cluster is up, "+
		"or set fsx_openzfs.delete_child_volumes_on_destroy: true to delete them with the filesystem",
		fs.ID, len(children), strings.Join(children, ", "), cfg.FSxOpenZFSStorageClassName())
	span.RecordError(err)
	return err
}

// precheckFSxOpenZFSDestroy reads the filesystem from tofu outputs and runs
// checkFSxOpenZFSChildVolumes. It returns the filesystem so the caller can
// report the final backup after destroy.
func precheckFSxOpenZFSDestroy(ctx context.Context, outputs map[string]tfexec.OutputMeta, cfg *Config) (fsxOpenZFSFilesystem, error) {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "aws.precheckFSxOpenZFSDestroy")
	defer span.End()

	fs, err := fsxOpenZFSFilesystemFromOutputs(outputs)
	if err != nil {
		span.RecordError(err)
		return fsxOpenZFSFilesystem{}, err
	}
	if !cfg.FSxOpenZFSEnabled() || fs.ID == "" {
		return fs, nil
	}

	status.Send(ctx, status.NewUpdate(status.LevelInfo, "Checking FSx for OpenZFS filesystem for child volumes").
		WithResource("fsx-openzfs").
		WithAction("checking").
		WithMetadata("fsx_openzfs_id", fs.ID))

	client, err := newFSxClient(ctx, cfg.Region)
	if err != nil {
		span.RecordError(err)
		return fs, err
	}
	if err := checkFSxOpenZFSChildVolumes(ctx, client, cfg, fs); err != nil {
		span.RecordError(err)
		return fs, err
	}
	return fs, nil
}
