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

	clusterName := cfg.ProjectName
	region := cfg.AmazonWebServices.Region

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

	fmt.Println("\n🔍 DRY RUN: Analyzing deployment changes...")
	fmt.Printf("   Provider:     aws\n")
	fmt.Printf("   Project Name: %s\n", clusterName)
	fmt.Printf("   Region:       %s\n", region)

	// Analyze VPC
	fmt.Println("\n📦 VPC / Network:")
	if actualVPC == nil {
		fmt.Printf("   • VPC: WILL CREATE (CIDR: %s)\n", getVPCCIDR(cfg))
		fmt.Println("   • Subnets: WILL CREATE (public + private across AZs)")
		fmt.Println("   • Internet Gateway: WILL CREATE")
		fmt.Println("   • NAT Gateways: WILL CREATE (one per AZ)")
		fmt.Println("   • Route Tables: WILL CREATE")
	} else {
		fmt.Printf("   • VPC: EXISTS (%s, CIDR: %s)\n", actualVPC.VPCID, actualVPC.CIDR)
		fmt.Printf("   • Subnets: EXISTS (%d public, %d private)\n",
			len(actualVPC.PublicSubnetIDs), len(actualVPC.PrivateSubnetIDs))
		if actualVPC.InternetGatewayID != "" {
			fmt.Printf("   • Internet Gateway: EXISTS (%s)\n", actualVPC.InternetGatewayID)
		}
		fmt.Printf("   • NAT Gateways: EXISTS (%d)\n", len(actualVPC.NATGatewayIDs))
	}

	// Analyze IAM
	fmt.Println("\n🔐 IAM Roles:")
	if actualIAM == nil {
		fmt.Println("   • Cluster Role: WILL CREATE")
		fmt.Println("   • Node Role: WILL CREATE")
	} else {
		if actualIAM.ClusterRoleARN != "" {
			fmt.Println("   • Cluster Role: EXISTS")
		} else {
			fmt.Println("   • Cluster Role: WILL CREATE")
		}
		if actualIAM.NodeRoleARN != "" {
			fmt.Println("   • Node Role: EXISTS")
		} else {
			fmt.Println("   • Node Role: WILL CREATE")
		}
	}

	// Analyze EKS Cluster
	fmt.Println("\n☸️  EKS Cluster:")
	desiredVersion := cfg.AmazonWebServices.KubernetesVersion
	if desiredVersion == "" {
		desiredVersion = "1.34" // default
	}
	if actualCluster == nil {
		fmt.Printf("   • Cluster: WILL CREATE (version %s)\n", desiredVersion)
	} else {
		fmt.Printf("   • Cluster: EXISTS (%s, version %s, status: %s)\n",
			actualCluster.Name, actualCluster.Version, actualCluster.Status)
		if actualCluster.Version != desiredVersion {
			fmt.Printf("   • Version: WILL UPDATE (%s → %s)\n", actualCluster.Version, desiredVersion)
		}
	}

	// Analyze Node Groups
	fmt.Println("\n🖥️  Node Groups:")
	desiredNodeGroups := cfg.AmazonWebServices.NodeGroups
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
			spotStr := ""
			if desired.Spot {
				spotStr = " [SPOT]"
			}
			gpuStr := ""
			if desired.GPU {
				gpuStr = " [GPU]"
			}
			fmt.Printf("   • %s: WILL CREATE (%s, min:%d/max:%d)%s%s\n",
				name, desired.Instance, desired.MinNodes, desired.MaxNodes, spotStr, gpuStr)
		} else {
			changes := []string{}
			if desired.MinNodes != actual.MinSize {
				changes = append(changes, fmt.Sprintf("min: %d→%d", actual.MinSize, desired.MinNodes))
			}
			if desired.MaxNodes != actual.MaxSize {
				changes = append(changes, fmt.Sprintf("max: %d→%d", actual.MaxSize, desired.MaxNodes))
			}
			if len(changes) > 0 {
				fmt.Printf("   • %s: WILL UPDATE (%s)\n", name, joinStrings(changes, ", "))
			} else {
				fmt.Printf("   • %s: NO CHANGES (%s, min:%d/max:%d)\n",
					name, actual.InstanceTypes[0], actual.MinSize, actual.MaxSize)
			}
		}
	}

	// Check for deletes (orphaned node groups)
	for name := range actualNodeGroupMap {
		if _, desired := desiredNodeGroups[name]; !desired {
			fmt.Printf("   • %s: WILL DELETE (not in config)\n", name)
		}
	}

	fmt.Println("\n✓ Dry-run complete. No changes were made.")
	fmt.Println("  Run without --dry-run flag to apply changes.")

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

	fmt.Println("\n🔍 DRY RUN: Discovering infrastructure to destroy...")
	fmt.Printf("   Provider:     aws\n")
	fmt.Printf("   Project Name: %s\n", clusterName)
	fmt.Printf("   Region:       %s\n", region)

	// Check if anything exists
	if vpc == nil && cluster == nil && len(nodeGroups) == 0 && iamRoles == nil {
		fmt.Println("\n   ✓ No infrastructure found for this project.")
		fmt.Println("     Nothing to destroy.")
		span.SetAttributes(attribute.Bool("infrastructure_exists", false))
		return nil
	}

	span.SetAttributes(attribute.Bool("infrastructure_exists", true))
	fmt.Println("\n   Resources that would be deleted:")

	// Show cluster information
	if cluster != nil {
		fmt.Println("\n   • Kubernetes Cluster")
		fmt.Printf("     - Name:     %s\n", cluster.Name)
		fmt.Printf("     - ID:       %s\n", cluster.ARN)
		fmt.Printf("     - Version:  %s\n", cluster.Version)
		fmt.Printf("     - Status:   %s\n", cluster.Status)
		fmt.Printf("     - Endpoint: %s\n", cluster.Endpoint)
	}

	// Show node pools
	if len(nodeGroups) > 0 {
		fmt.Println("\n   • Node Groups")
		for _, ng := range nodeGroups {
			spotIndicator := ""
			if ng.CapacityType == "SPOT" {
				spotIndicator = " [SPOT]"
			}
			gpuIndicator := ""
			if ng.AMIType == "AL2_x86_64_GPU" || ng.AMIType == "AL2023_x86_64_NVIDIA" {
				gpuIndicator = " [GPU]"
			}
			instanceType := ""
			if len(ng.InstanceTypes) > 0 {
				instanceType = ng.InstanceTypes[0]
			}
			fmt.Printf("     - %s (%s, min:%d/max:%d)%s%s\n",
				ng.Name, instanceType, ng.MinSize, ng.MaxSize, spotIndicator, gpuIndicator)
		}
	}

	// Show network information
	if vpc != nil {
		fmt.Println("\n   • Network Infrastructure")
		fmt.Printf("     - VPC ID:         %s\n", vpc.VPCID)
		fmt.Printf("     - CIDR:           %s\n", vpc.CIDR)
		fmt.Printf("     - Public Subnets: %d\n", len(vpc.PublicSubnetIDs))
		fmt.Printf("     - Private Subnets: %d\n", len(vpc.PrivateSubnetIDs))
		if len(vpc.NATGatewayIDs) > 0 {
			fmt.Printf("     - NAT Gateways:   %d\n", len(vpc.NATGatewayIDs))
		}
		if vpc.InternetGatewayID != "" {
			fmt.Printf("     - Internet GW:    %s\n", vpc.InternetGatewayID)
		}
	}

	// Show IAM roles
	if iamRoles != nil {
		fmt.Println("\n   • IAM Resources")
		if iamRoles.ClusterRoleARN != "" {
			fmt.Println("     - Cluster Role")
		}
		if iamRoles.NodeRoleARN != "" {
			fmt.Println("     - Node Role")
		}
		if iamRoles.OIDCProviderARN != "" {
			fmt.Println("     - OIDC Provider")
		}
	}

	fmt.Println("\n✓ Dry-run complete. No resources were deleted.")
	fmt.Println("  Run without --dry-run flag to perform actual destruction.")

	span.SetAttributes(attribute.Bool("dry_run_complete", true))
	return nil
}

// Helper functions

func getVPCCIDR(cfg *config.NebariConfig) string {
	if cfg.AmazonWebServices.VPCCIDRBlock != "" {
		return cfg.AmazonWebServices.VPCCIDRBlock
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
