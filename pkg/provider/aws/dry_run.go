package aws

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/status"
)

// dryRunDeploy shows what would be deployed without making changes
func (p *Provider) dryRunDeploy(ctx context.Context, cfg *config.NebariConfig) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "aws.dryRunDeploy")
	defer span.End()

	// Extract AWS configuration
	awsCfg, err := extractAWSConfig(ctx, cfg)
	if err != nil {
		span.RecordError(err)
		return err
	}

	clusterName := cfg.ProjectName
	region := awsCfg.Region

	span.SetAttributes(
		attribute.String("cluster_name", clusterName),
		attribute.String("region", region),
	)

	// Initialize AWS clients
	clients, err := newClientsFunc(ctx, region)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to create AWS clients: %w", err)
	}

	status.Info(ctx, "Discovering existing infrastructure...")

	// Discover current state
	actualVPC, _ := p.DiscoverVPC(ctx, clients, clusterName)
	actualCluster, _ := p.DiscoverCluster(ctx, clients, clusterName)
	actualNodeGroups, _ := p.DiscoverNodeGroups(ctx, clients, clusterName)
	actualIAM, _ := p.discoverIAMRoles(ctx, clients, clusterName)

	status.Send(ctx, status.NewUpdate(status.LevelInfo, "DRY RUN: Analyzing deployment changes").
		WithResource("dry-run").
		WithAction("analyze").
		WithMetadata("provider", "aws").
		WithMetadata("project_name", clusterName).
		WithMetadata("region", region))

	// Analyze VPC
	status.Send(ctx, status.NewUpdate(status.LevelInfo, "VPC / Network").
		WithResource("vpc").
		WithAction("analyze"))

	if actualVPC == nil {
		status.Send(ctx, status.NewUpdate(status.LevelInfo, "VPC: WILL CREATE").
			WithResource("vpc").
			WithAction("create").
			WithMetadata("cidr", getVPCCIDR(cfg)))
		status.Send(ctx, status.NewUpdate(status.LevelInfo, "Subnets: WILL CREATE (public + private across AZs)").
			WithResource("subnet").
			WithAction("create"))
		status.Send(ctx, status.NewUpdate(status.LevelInfo, "Internet Gateway: WILL CREATE").
			WithResource("internet-gateway").
			WithAction("create"))
		status.Send(ctx, status.NewUpdate(status.LevelInfo, "NAT Gateways: WILL CREATE (one per AZ)").
			WithResource("nat-gateway").
			WithAction("create"))
		status.Send(ctx, status.NewUpdate(status.LevelInfo, "Route Tables: WILL CREATE").
			WithResource("route-table").
			WithAction("create"))
	} else {
		status.Send(ctx, status.NewUpdate(status.LevelInfo, "VPC: EXISTS").
			WithResource("vpc").
			WithAction("exists").
			WithMetadata("vpc_id", actualVPC.VPCID).
			WithMetadata("cidr", actualVPC.CIDR))
		status.Send(ctx, status.NewUpdate(status.LevelInfo, "Subnets: EXISTS").
			WithResource("subnet").
			WithAction("exists").
			WithMetadata("public_count", len(actualVPC.PublicSubnetIDs)).
			WithMetadata("private_count", len(actualVPC.PrivateSubnetIDs)))
		if actualVPC.InternetGatewayID != "" {
			status.Send(ctx, status.NewUpdate(status.LevelInfo, "Internet Gateway: EXISTS").
				WithResource("internet-gateway").
				WithAction("exists").
				WithMetadata("igw_id", actualVPC.InternetGatewayID))
		}
		status.Send(ctx, status.NewUpdate(status.LevelInfo, "NAT Gateways: EXISTS").
			WithResource("nat-gateway").
			WithAction("exists").
			WithMetadata("count", len(actualVPC.NATGatewayIDs)))
	}

	// Analyze IAM
	status.Send(ctx, status.NewUpdate(status.LevelInfo, "IAM Roles").
		WithResource("iam").
		WithAction("analyze"))

	if actualIAM == nil {
		status.Send(ctx, status.NewUpdate(status.LevelInfo, "Cluster Role: WILL CREATE").
			WithResource("iam-role").
			WithAction("create").
			WithMetadata("role_type", "cluster"))
		status.Send(ctx, status.NewUpdate(status.LevelInfo, "Node Role: WILL CREATE").
			WithResource("iam-role").
			WithAction("create").
			WithMetadata("role_type", "node"))
	} else {
		if actualIAM.ClusterRoleARN != "" {
			status.Send(ctx, status.NewUpdate(status.LevelInfo, "Cluster Role: EXISTS").
				WithResource("iam-role").
				WithAction("exists").
				WithMetadata("role_type", "cluster"))
		} else {
			status.Send(ctx, status.NewUpdate(status.LevelInfo, "Cluster Role: WILL CREATE").
				WithResource("iam-role").
				WithAction("create").
				WithMetadata("role_type", "cluster"))
		}
		if actualIAM.NodeRoleARN != "" {
			status.Send(ctx, status.NewUpdate(status.LevelInfo, "Node Role: EXISTS").
				WithResource("iam-role").
				WithAction("exists").
				WithMetadata("role_type", "node"))
		} else {
			status.Send(ctx, status.NewUpdate(status.LevelInfo, "Node Role: WILL CREATE").
				WithResource("iam-role").
				WithAction("create").
				WithMetadata("role_type", "node"))
		}
	}

	// Analyze EKS Cluster
	status.Send(ctx, status.NewUpdate(status.LevelInfo, "EKS Cluster").
		WithResource("eks-cluster").
		WithAction("analyze"))

	desiredVersion := awsCfg.KubernetesVersion
	if desiredVersion == "" {
		desiredVersion = "1.34" // default
	}
	if actualCluster == nil {
		status.Send(ctx, status.NewUpdate(status.LevelInfo, "Cluster: WILL CREATE").
			WithResource("eks-cluster").
			WithAction("create").
			WithMetadata("version", desiredVersion))
	} else {
		status.Send(ctx, status.NewUpdate(status.LevelInfo, "Cluster: EXISTS").
			WithResource("eks-cluster").
			WithAction("exists").
			WithMetadata("name", actualCluster.Name).
			WithMetadata("version", actualCluster.Version).
			WithMetadata("status", actualCluster.Status))
		if actualCluster.Version != desiredVersion {
			status.Send(ctx, status.NewUpdate(status.LevelInfo, "Version: WILL UPDATE").
				WithResource("eks-cluster").
				WithAction("update").
				WithMetadata("current_version", actualCluster.Version).
				WithMetadata("desired_version", desiredVersion))
		}
	}

	// Analyze Node Groups
	status.Send(ctx, status.NewUpdate(status.LevelInfo, "Node Groups").
		WithResource("node-group").
		WithAction("analyze"))

	desiredNodeGroups := awsCfg.NodeGroups
	// Build map of actual node groups by node pool name (from tags), matching reconciliation logic
	actualNodeGroupMap := make(map[string]*NodeGroupState)
	for i := range actualNodeGroups {
		ng := &actualNodeGroups[i]
		nodePoolName, ok := ng.Tags[TagNodePool]
		if ok {
			actualNodeGroupMap[nodePoolName] = ng
		}
	}

	// Check for creates and updates
	for name, desired := range desiredNodeGroups {
		actual, exists := actualNodeGroupMap[name]
		if !exists {
			msg := fmt.Sprintf("%s: WILL CREATE", name)
			update := status.NewUpdate(status.LevelInfo, msg).
				WithResource("node-group").
				WithAction("create").
				WithMetadata("name", name).
				WithMetadata("instance", desired.Instance).
				WithMetadata("min_nodes", desired.MinNodes).
				WithMetadata("max_nodes", desired.MaxNodes)
			if desired.Spot {
				update = update.WithMetadata("spot", true)
			}
			if desired.GPU {
				update = update.WithMetadata("gpu", true)
			}
			status.Send(ctx, update)
		} else {
			changes := []string{}
			if desired.MinNodes != actual.MinSize {
				changes = append(changes, fmt.Sprintf("min: %d→%d", actual.MinSize, desired.MinNodes))
			}
			if desired.MaxNodes != actual.MaxSize {
				changes = append(changes, fmt.Sprintf("max: %d→%d", actual.MaxSize, desired.MaxNodes))
			}
			if len(changes) > 0 {
				msg := fmt.Sprintf("%s: WILL UPDATE (%s)", name, joinStrings(changes, ", "))
				status.Send(ctx, status.NewUpdate(status.LevelInfo, msg).
					WithResource("node-group").
					WithAction("update").
					WithMetadata("name", name).
					WithMetadata("changes", joinStrings(changes, ", ")))
			} else {
				msg := fmt.Sprintf("%s: NO CHANGES", name)
				status.Send(ctx, status.NewUpdate(status.LevelInfo, msg).
					WithResource("node-group").
					WithAction("no-change").
					WithMetadata("name", name).
					WithMetadata("instance", actual.InstanceTypes[0]).
					WithMetadata("min_size", actual.MinSize).
					WithMetadata("max_size", actual.MaxSize))
			}
		}
	}

	// Check for deletes (orphaned node groups)
	for name := range actualNodeGroupMap {
		if _, desired := desiredNodeGroups[name]; !desired {
			status.Send(ctx, status.NewUpdate(status.LevelInfo, fmt.Sprintf("%s: WILL DELETE (not in config)", name)).
				WithResource("node-group").
				WithAction("delete").
				WithMetadata("name", name))
		}
	}

	status.Send(ctx, status.NewUpdate(status.LevelSuccess, "Dry-run complete. No changes were made.").
		WithResource("dry-run").
		WithAction("complete"))
	status.Send(ctx, status.NewUpdate(status.LevelInfo, "Run without --dry-run flag to apply changes.").
		WithResource("dry-run").
		WithAction("instructions"))

	span.SetAttributes(attribute.Bool("dry_run_complete", true))
	return nil
}

// dryRunDestroy shows what would be destroyed without making changes
func (p *Provider) dryRunDestroy(ctx context.Context, clients *Clients, clusterName, region string) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "aws.dryRunDestroy")
	defer span.End()

	span.SetAttributes(
		attribute.String("cluster_name", clusterName),
		attribute.String("region", region),
	)

	status.Info(ctx, "Discovering infrastructure to destroy...")

	// Discover current state
	vpc, _ := p.DiscoverVPC(ctx, clients, clusterName)
	cluster, _ := p.DiscoverCluster(ctx, clients, clusterName)
	nodeGroups, _ := p.DiscoverNodeGroups(ctx, clients, clusterName)
	iamRoles, _ := p.discoverIAMRoles(ctx, clients, clusterName)

	status.Send(ctx, status.NewUpdate(status.LevelInfo, "DRY RUN: Discovering infrastructure to destroy").
		WithResource("dry-run").
		WithAction("discover").
		WithMetadata("provider", "aws").
		WithMetadata("project_name", clusterName).
		WithMetadata("region", region))

	// Check if anything exists
	if vpc == nil && cluster == nil && len(nodeGroups) == 0 && iamRoles == nil {
		status.Send(ctx, status.NewUpdate(status.LevelInfo, "No infrastructure found for this project").
			WithResource("dry-run").
			WithAction("none"))
		status.Send(ctx, status.NewUpdate(status.LevelInfo, "Nothing to destroy").
			WithResource("dry-run").
			WithAction("complete"))
		span.SetAttributes(attribute.Bool("infrastructure_exists", false))
		return nil
	}

	span.SetAttributes(attribute.Bool("infrastructure_exists", true))
	status.Send(ctx, status.NewUpdate(status.LevelInfo, "Resources that would be deleted").
		WithResource("dry-run").
		WithAction("list"))

	// Show cluster information
	if cluster != nil {
		status.Send(ctx, status.NewUpdate(status.LevelInfo, "Kubernetes Cluster").
			WithResource("eks-cluster").
			WithAction("destroy").
			WithMetadata("name", cluster.Name).
			WithMetadata("arn", cluster.ARN).
			WithMetadata("version", cluster.Version).
			WithMetadata("status", cluster.Status).
			WithMetadata("endpoint", cluster.Endpoint))
	}

	// Show node pools
	if len(nodeGroups) > 0 {
		status.Send(ctx, status.NewUpdate(status.LevelInfo, "Node Groups").
			WithResource("node-group").
			WithAction("analyze").
			WithMetadata("count", len(nodeGroups)))
		for _, ng := range nodeGroups {
			update := status.NewUpdate(status.LevelInfo, ng.Name).
				WithResource("node-group").
				WithAction("destroy").
				WithMetadata("name", ng.Name).
				WithMetadata("min_size", ng.MinSize).
				WithMetadata("max_size", ng.MaxSize)
			if len(ng.InstanceTypes) > 0 {
				update = update.WithMetadata("instance_type", ng.InstanceTypes[0])
			}
			if ng.CapacityType == "SPOT" {
				update = update.WithMetadata("spot", true)
			}
			if ng.AMIType == "AL2_x86_64_GPU" || ng.AMIType == "AL2023_x86_64_NVIDIA" {
				update = update.WithMetadata("gpu", true)
			}
			status.Send(ctx, update)
		}
	}

	// Show network information
	if vpc != nil {
		status.Send(ctx, status.NewUpdate(status.LevelInfo, "Network Infrastructure").
			WithResource("vpc").
			WithAction("destroy").
			WithMetadata("vpc_id", vpc.VPCID).
			WithMetadata("cidr", vpc.CIDR).
			WithMetadata("public_subnets", len(vpc.PublicSubnetIDs)).
			WithMetadata("private_subnets", len(vpc.PrivateSubnetIDs)).
			WithMetadata("nat_gateways", len(vpc.NATGatewayIDs)).
			WithMetadata("internet_gateway", vpc.InternetGatewayID))
	}

	// Show IAM roles
	if iamRoles != nil {
		status.Send(ctx, status.NewUpdate(status.LevelInfo, "IAM Resources").
			WithResource("iam").
			WithAction("analyze"))
		if iamRoles.ClusterRoleARN != "" {
			status.Send(ctx, status.NewUpdate(status.LevelInfo, "Cluster Role").
				WithResource("iam-role").
				WithAction("destroy").
				WithMetadata("role_type", "cluster"))
		}
		if iamRoles.NodeRoleARN != "" {
			status.Send(ctx, status.NewUpdate(status.LevelInfo, "Node Role").
				WithResource("iam-role").
				WithAction("destroy").
				WithMetadata("role_type", "node"))
		}
		if iamRoles.OIDCProviderARN != "" {
			status.Send(ctx, status.NewUpdate(status.LevelInfo, "OIDC Provider").
				WithResource("oidc-provider").
				WithAction("destroy"))
		}
	}

	status.Send(ctx, status.NewUpdate(status.LevelSuccess, "Dry-run complete. No resources were deleted.").
		WithResource("dry-run").
		WithAction("complete"))
	status.Send(ctx, status.NewUpdate(status.LevelInfo, "Run without --dry-run flag to perform actual destruction.").
		WithResource("dry-run").
		WithAction("instructions"))

	span.SetAttributes(attribute.Bool("dry_run_complete", true))
	return nil
}

// Helper functions

func getVPCCIDR(cfg *config.NebariConfig) string {
	awsCfg, err := extractAWSConfig(context.Background(), cfg)
	if err != nil {
		return "10.0.0.0/16" // default
	}
	if awsCfg.VPCCIDRBlock != "" {
		return awsCfg.VPCCIDRBlock
	}
	return "10.0.0.0/16" // default
}

func joinStrings(strs []string, sep string) string {
	if len(strs) == 0 {
		return ""
	}
	result := strs[0]
	for i := 1; i < len(strs); i++ {
		result += sep + strs[i]
	}
	return result
}
