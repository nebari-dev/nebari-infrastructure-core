package aws

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/efs"
	efstypes "github.com/aws/aws-sdk-go-v2/service/efs/types"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
)

// DiscoverEFS discovers existing EFS file systems by querying AWS with NIC tags
func (p *Provider) DiscoverEFS(ctx context.Context, clients *Clients, clusterName string) (*StorageState, error) {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "aws.DiscoverEFS")
	defer span.End()

	span.SetAttributes(attribute.String("cluster_name", clusterName))

	// Query EFS file systems - EFS doesn't support tag filtering in DescribeFileSystems,
	// so we need to list all and filter by tags
	result, err := clients.EFSClient.DescribeFileSystems(ctx, &efs.DescribeFileSystemsInput{})
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("DescribeFileSystems API call failed: %w", err)
	}

	// Find the file system with matching NIC tags
	var matchingFS *efstypes.FileSystemDescription
	for i := range result.FileSystems {
		fs := &result.FileSystems[i]
		if fs.Tags == nil {
			continue
		}
		tags := convertEFSTagsToMap(fs.Tags)
		if tags[TagManagedBy] == ManagedByValue &&
			tags[TagClusterName] == clusterName &&
			tags[TagResourceType] == ResourceTypeEFS {
			matchingFS = fs
			break
		}
	}

	if matchingFS == nil {
		// No EFS found - this is ok, means EFS doesn't exist yet
		return nil, nil
	}

	// Build storage state from discovered file system
	storageState := &StorageState{
		FileSystemID:    *matchingFS.FileSystemId,
		ARN:             *matchingFS.FileSystemArn,
		LifeCycleState:  string(matchingFS.LifeCycleState),
		PerformanceMode: string(matchingFS.PerformanceMode),
		ThroughputMode:  string(matchingFS.ThroughputMode),
		Encrypted:       aws.ToBool(matchingFS.Encrypted),
		Tags:            convertEFSTagsToMap(matchingFS.Tags),
	}

	if matchingFS.ProvisionedThroughputInMibps != nil {
		storageState.ProvisionedThroughputMiBps = *matchingFS.ProvisionedThroughputInMibps
	}
	if matchingFS.KmsKeyId != nil {
		storageState.KMSKeyID = *matchingFS.KmsKeyId
	}
	if matchingFS.SizeInBytes != nil {
		storageState.SizeInBytes = matchingFS.SizeInBytes.Value
	}
	if matchingFS.CreationTime != nil {
		storageState.CreatedAt = matchingFS.CreationTime.Format(time.RFC3339)
	}

	// Discover mount targets
	mountTargets, err := p.discoverMountTargets(ctx, clients, storageState.FileSystemID)
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("failed to discover mount targets: %w", err)
	}
	storageState.MountTargets = mountTargets

	// Collect security group IDs from mount targets
	sgIDs := make(map[string]bool)
	for _, mt := range mountTargets {
		for _, sg := range getSecurityGroupsForMountTarget(ctx, clients, mt.MountTargetID) {
			sgIDs[sg] = true
		}
	}
	for sg := range sgIDs {
		storageState.SecurityGroupIDs = append(storageState.SecurityGroupIDs, sg)
	}

	span.SetAttributes(
		attribute.String("file_system_id", storageState.FileSystemID),
		attribute.String("lifecycle_state", storageState.LifeCycleState),
		attribute.Int("mount_targets", len(storageState.MountTargets)),
	)

	return storageState, nil
}

// discoverMountTargets discovers mount targets for an EFS file system
func (p *Provider) discoverMountTargets(ctx context.Context, clients *Clients, fileSystemID string) ([]MountTarget, error) {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "aws.discoverMountTargets")
	defer span.End()

	span.SetAttributes(attribute.String("file_system_id", fileSystemID))

	result, err := clients.EFSClient.DescribeMountTargets(ctx, &efs.DescribeMountTargetsInput{
		FileSystemId: aws.String(fileSystemID),
	})
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("DescribeMountTargets API call failed: %w", err)
	}

	var mountTargets []MountTarget
	for _, mt := range result.MountTargets {
		mountTarget := MountTarget{
			MountTargetID:  aws.ToString(mt.MountTargetId),
			SubnetID:       aws.ToString(mt.SubnetId),
			IPAddress:      aws.ToString(mt.IpAddress),
			LifeCycleState: string(mt.LifeCycleState),
		}
		if mt.AvailabilityZoneName != nil {
			mountTarget.AvailabilityZone = *mt.AvailabilityZoneName
		}
		mountTargets = append(mountTargets, mountTarget)
	}

	span.SetAttributes(attribute.Int("mount_targets_found", len(mountTargets)))

	return mountTargets, nil
}

// getSecurityGroupsForMountTarget retrieves security groups for a mount target
// Note: This is a helper that would require DescribeMountTargetSecurityGroups API
// For simplicity, returns empty slice - in real implementation would query API
func getSecurityGroupsForMountTarget(ctx context.Context, clients *Clients, mountTargetID string) []string {
	// EFS mount target security groups require separate API call
	// DescribeMountTargetSecurityGroups - not currently in our interface
	// For now, we track security groups at the StorageState level during reconciliation
	return nil
}

// reconcileEFS ensures the EFS file system matches the desired configuration
func (p *Provider) reconcileEFS(ctx context.Context, clients *Clients, cfg *config.NebariConfig, vpc *VPCState, actual *StorageState) (*StorageState, error) {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "aws.reconcileEFS")
	defer span.End()

	awsCfg := cfg.AmazonWebServices
	if awsCfg == nil || awsCfg.EFS == nil || !awsCfg.EFS.Enabled {
		// EFS not configured, nothing to do
		span.SetAttributes(attribute.Bool("efs_enabled", false))
		return nil, nil
	}

	efsCfg := awsCfg.EFS
	span.SetAttributes(
		attribute.Bool("efs_enabled", true),
		attribute.String("performance_mode", efsCfg.PerformanceMode),
		attribute.String("throughput_mode", efsCfg.ThroughputMode),
	)

	if actual == nil {
		// Create new EFS file system
		return p.createEFS(ctx, clients, cfg, vpc)
	}

	// Check if EFS is available
	if actual.LifeCycleState != string(efstypes.LifeCycleStateAvailable) {
		return nil, fmt.Errorf("EFS file system %s is in state %s, cannot reconcile",
			actual.FileSystemID, actual.LifeCycleState)
	}

	// Validate immutable fields

	// Performance mode is immutable
	desiredPerfMode := "generalPurpose"
	if efsCfg.PerformanceMode != "" {
		desiredPerfMode = efsCfg.PerformanceMode
	}
	if actual.PerformanceMode != desiredPerfMode {
		err := fmt.Errorf("EFS performance mode is immutable and cannot be changed (current: %s, desired: %s). Manual intervention required - destroy and recreate EFS", actual.PerformanceMode, desiredPerfMode)
		span.RecordError(err)
		return nil, err
	}

	// Encryption is immutable
	if actual.Encrypted != efsCfg.Encrypted {
		err := fmt.Errorf("EFS encryption setting is immutable and cannot be changed (current: %t, desired: %t). Manual intervention required - destroy and recreate EFS", actual.Encrypted, efsCfg.Encrypted)
		span.RecordError(err)
		return nil, err
	}

	// KMS key is immutable (only check if encryption is enabled)
	if efsCfg.Encrypted && actual.KMSKeyID != efsCfg.KMSKeyID {
		// Both empty is fine, but any change is an error
		if actual.KMSKeyID != "" || efsCfg.KMSKeyID != "" {
			err := fmt.Errorf("EFS KMS key is immutable and cannot be changed (current: %q, desired: %q). Manual intervention required - destroy and recreate EFS", actual.KMSKeyID, efsCfg.KMSKeyID)
			span.RecordError(err)
			return nil, err
		}
	}

	// Throughput mode is mutable - we can update it if needed
	if needsEFSThroughputUpdate(actual, efsCfg) {
		if err := p.updateEFSThroughput(ctx, clients, actual, efsCfg); err != nil {
			span.RecordError(err)
			return nil, fmt.Errorf("failed to update EFS throughput: %w", err)
		}
	}

	// Reconcile mount targets - ensure one in each private subnet
	if err := p.reconcileMountTargets(ctx, clients, cfg.ProjectName, actual.FileSystemID, vpc); err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("failed to reconcile mount targets: %w", err)
	}

	// Re-discover to get updated state
	return p.DiscoverEFS(ctx, clients, cfg.ProjectName)
}

// createEFS creates a new EFS file system with mount targets
func (p *Provider) createEFS(ctx context.Context, clients *Clients, cfg *config.NebariConfig, vpc *VPCState) (*StorageState, error) {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "aws.createEFS")
	defer span.End()

	awsCfg := cfg.AmazonWebServices
	efsCfg := awsCfg.EFS

	// Determine performance mode
	perfMode := efstypes.PerformanceModeGeneralPurpose
	if efsCfg.PerformanceMode == "maxIO" {
		perfMode = efstypes.PerformanceModeMaxIo
	}

	// Determine throughput mode
	throughputMode := efstypes.ThroughputModeBursting
	switch efsCfg.ThroughputMode {
	case "provisioned":
		throughputMode = efstypes.ThroughputModeProvisioned
	case "elastic":
		throughputMode = efstypes.ThroughputModeElastic
	}

	// Generate tags
	baseTags := GenerateBaseTags(ctx, cfg.ProjectName, ResourceTypeEFS)
	baseTags["Name"] = GenerateResourceName(cfg.ProjectName, "efs", "")

	// Merge with user tags
	allTags := MergeTags(ctx, baseTags, awsCfg.Tags)
	efsTags := convertMapToEFSTags(allTags)

	// Create file system input
	input := &efs.CreateFileSystemInput{
		CreationToken:   aws.String(GenerateResourceName(cfg.ProjectName, "efs", "")),
		PerformanceMode: perfMode,
		ThroughputMode:  throughputMode,
		Encrypted:       aws.Bool(efsCfg.Encrypted),
		Tags:            efsTags,
	}

	// Add KMS key if specified
	if efsCfg.KMSKeyID != "" {
		input.KmsKeyId = aws.String(efsCfg.KMSKeyID)
	}

	// Add provisioned throughput if specified
	if throughputMode == efstypes.ThroughputModeProvisioned && efsCfg.ProvisionedMBps > 0 {
		input.ProvisionedThroughputInMibps = aws.Float64(float64(efsCfg.ProvisionedMBps))
	}

	span.SetAttributes(
		attribute.String("performance_mode", string(perfMode)),
		attribute.String("throughput_mode", string(throughputMode)),
		attribute.Bool("encrypted", efsCfg.Encrypted),
	)

	// Create the file system
	result, err := clients.EFSClient.CreateFileSystem(ctx, input)
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("CreateFileSystem API call failed: %w", err)
	}

	fileSystemID := *result.FileSystemId
	span.SetAttributes(attribute.String("file_system_id", fileSystemID))

	// Wait for file system to become available
	if err := p.waitForEFSAvailable(ctx, clients, fileSystemID); err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("failed waiting for EFS to become available: %w", err)
	}

	// Create mount targets in each private subnet
	if err := p.reconcileMountTargets(ctx, clients, cfg.ProjectName, fileSystemID, vpc); err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("failed to create mount targets: %w", err)
	}

	// Discover and return the created EFS state
	return p.DiscoverEFS(ctx, clients, cfg.ProjectName)
}

// waitForEFSAvailable waits for an EFS file system to become available
func (p *Provider) waitForEFSAvailable(ctx context.Context, clients *Clients, fileSystemID string) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "aws.waitForEFSAvailable")
	defer span.End()

	span.SetAttributes(attribute.String("file_system_id", fileSystemID))

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	timeout := time.After(5 * time.Minute)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timeout:
			return fmt.Errorf("timeout waiting for EFS file system %s to become available", fileSystemID)
		case <-ticker.C:
			result, err := clients.EFSClient.DescribeFileSystems(ctx, &efs.DescribeFileSystemsInput{
				FileSystemId: aws.String(fileSystemID),
			})
			if err != nil {
				span.RecordError(err)
				return fmt.Errorf("DescribeFileSystems API call failed: %w", err)
			}
			if len(result.FileSystems) > 0 {
				state := result.FileSystems[0].LifeCycleState
				if state == efstypes.LifeCycleStateAvailable {
					return nil
				}
				if state == efstypes.LifeCycleStateError {
					return fmt.Errorf("EFS file system %s is in error state", fileSystemID)
				}
			}
		}
	}
}

// reconcileMountTargets ensures mount targets exist in each private subnet
func (p *Provider) reconcileMountTargets(ctx context.Context, clients *Clients, clusterName string, fileSystemID string, vpc *VPCState) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "aws.reconcileMountTargets")
	defer span.End()

	span.SetAttributes(
		attribute.String("file_system_id", fileSystemID),
		attribute.Int("private_subnets", len(vpc.PrivateSubnetIDs)),
	)

	// Get existing mount targets
	existingMTs, err := p.discoverMountTargets(ctx, clients, fileSystemID)
	if err != nil {
		return fmt.Errorf("failed to discover existing mount targets: %w", err)
	}

	// Build map of subnets with mount targets
	existingSubnets := make(map[string]bool)
	for _, mt := range existingMTs {
		existingSubnets[mt.SubnetID] = true
	}

	// Find security group for EFS mount targets
	// Use the first security group from VPC state, or we need to create one
	var securityGroupID string
	if len(vpc.SecurityGroupIDs) > 0 {
		securityGroupID = vpc.SecurityGroupIDs[0]
	}

	// Create mount targets in each private subnet that doesn't have one
	for _, subnetID := range vpc.PrivateSubnetIDs {
		if existingSubnets[subnetID] {
			continue
		}

		input := &efs.CreateMountTargetInput{
			FileSystemId: aws.String(fileSystemID),
			SubnetId:     aws.String(subnetID),
		}
		if securityGroupID != "" {
			input.SecurityGroups = []string{securityGroupID}
		}

		_, err := clients.EFSClient.CreateMountTarget(ctx, input)
		if err != nil {
			span.RecordError(err)
			return fmt.Errorf("CreateMountTarget API call failed for subnet %s: %w", subnetID, err)
		}
	}

	return nil
}

// needsEFSThroughputUpdate checks if EFS throughput configuration needs updating
func needsEFSThroughputUpdate(actual *StorageState, efsCfg *config.EFSConfig) bool {
	desiredMode := "bursting"
	if efsCfg.ThroughputMode != "" {
		desiredMode = efsCfg.ThroughputMode
	}

	if actual.ThroughputMode != desiredMode {
		return true
	}

	if desiredMode == "provisioned" {
		return actual.ProvisionedThroughputMiBps != float64(efsCfg.ProvisionedMBps)
	}

	return false
}

// updateEFSThroughput updates the throughput mode of an EFS file system
// Note: EFS UpdateFileSystem API would be needed here, not currently in interface
func (p *Provider) updateEFSThroughput(ctx context.Context, clients *Clients, actual *StorageState, efsCfg *config.EFSConfig) error {
	// EFS throughput updates require UpdateFileSystem API
	// This is a placeholder - would need to add to interface
	return fmt.Errorf("EFS throughput updates not yet implemented")
}

// deleteEFS deletes an EFS file system and its mount targets
func (p *Provider) deleteEFS(ctx context.Context, clients *Clients, storage *StorageState) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "aws.deleteEFS")
	defer span.End()

	if storage == nil {
		return nil
	}

	span.SetAttributes(attribute.String("file_system_id", storage.FileSystemID))

	// First, delete all mount targets
	for _, mt := range storage.MountTargets {
		if err := p.deleteMountTarget(ctx, clients, mt.MountTargetID); err != nil {
			span.RecordError(err)
			return fmt.Errorf("failed to delete mount target %s: %w", mt.MountTargetID, err)
		}
	}

	// Wait for mount targets to be deleted before deleting the file system
	if err := p.waitForMountTargetsDeleted(ctx, clients, storage.FileSystemID); err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed waiting for mount targets to be deleted: %w", err)
	}

	// Delete the file system
	_, err := clients.EFSClient.DeleteFileSystem(ctx, &efs.DeleteFileSystemInput{
		FileSystemId: aws.String(storage.FileSystemID),
	})
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("DeleteFileSystem API call failed: %w", err)
	}

	return nil
}

// deleteMountTarget deletes a single mount target
func (p *Provider) deleteMountTarget(ctx context.Context, clients *Clients, mountTargetID string) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "aws.deleteMountTarget")
	defer span.End()

	span.SetAttributes(attribute.String("mount_target_id", mountTargetID))

	_, err := clients.EFSClient.DeleteMountTarget(ctx, &efs.DeleteMountTargetInput{
		MountTargetId: aws.String(mountTargetID),
	})
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("DeleteMountTarget API call failed: %w", err)
	}

	return nil
}

// waitForMountTargetsDeleted waits for all mount targets to be deleted
func (p *Provider) waitForMountTargetsDeleted(ctx context.Context, clients *Clients, fileSystemID string) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "aws.waitForMountTargetsDeleted")
	defer span.End()

	span.SetAttributes(attribute.String("file_system_id", fileSystemID))

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	timeout := time.After(5 * time.Minute)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timeout:
			return fmt.Errorf("timeout waiting for mount targets to be deleted for EFS %s", fileSystemID)
		case <-ticker.C:
			result, err := clients.EFSClient.DescribeMountTargets(ctx, &efs.DescribeMountTargetsInput{
				FileSystemId: aws.String(fileSystemID),
			})
			if err != nil {
				span.RecordError(err)
				return fmt.Errorf("DescribeMountTargets API call failed: %w", err)
			}
			if len(result.MountTargets) == 0 {
				return nil
			}
		}
	}
}

// convertEFSTagsToMap converts EFS tags to a map
func convertEFSTagsToMap(tags []efstypes.Tag) map[string]string {
	tagMap := make(map[string]string, len(tags))
	for _, tag := range tags {
		if tag.Key != nil && tag.Value != nil {
			tagMap[*tag.Key] = *tag.Value
		}
	}
	return tagMap
}

// convertMapToEFSTags converts a map to EFS tags
func convertMapToEFSTags(tags map[string]string) []efstypes.Tag {
	efsTags := make([]efstypes.Tag, 0, len(tags))
	for k, v := range tags {
		key := k
		value := v
		efsTags = append(efsTags, efstypes.Tag{
			Key:   &key,
			Value: &value,
		})
	}
	return efsTags
}
