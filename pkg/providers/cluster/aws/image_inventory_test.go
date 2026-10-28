package aws

import (
	"strings"
	"testing"

	"github.com/nebari-dev/nebari-infrastructure-core/internal/imageinventory"
)

func TestDeployedImageInventory(t *testing.T) {
	inv := imageinventory.New(t)
	cfg := &Config{Region: "us-east-1", KubernetesVersion: "1.34"}
	inv.Chart(t, "pkg/providers/cluster/aws/aws_load_balancer_controller.go", lbcRepoURL, strings.TrimPrefix(lbcChartName, lbcRepoName+"/"), cfg.LoadBalancerControllerChartVersion(), lbcReleaseName, lbcNamespace, awsLoadBalancerControllerHelmValues(cfg, "inventory", "vpc-inventory"))
	inv.Chart(t, "pkg/providers/cluster/aws/cluster_autoscaler.go", caRepoURL, strings.TrimPrefix(caChartName, caRepoName+"/"), cfg.ClusterAutoscalerChartVersion(), caReleaseName, caNamespace, clusterAutoscalerHelmValues(cfg, "inventory"))
	defaultVersion := &Config{Region: "us-east-1"}
	inv.Chart(t, "pkg/providers/cluster/aws/cluster_autoscaler.go", caRepoURL, strings.TrimPrefix(caChartName, caRepoName+"/"), defaultVersion.ClusterAutoscalerChartVersion(), caReleaseName, caNamespace, clusterAutoscalerHelmValues(defaultVersion, "inventory"))
	inv.Chart(t, "pkg/providers/cluster/aws/gpu_operator.go", gpuOperatorRepoURL, strings.TrimPrefix(gpuOperatorChartName, gpuOperatorRepoName+"/"), gpuOperatorChartVersion, gpuOperatorReleaseName, gpuOperatorNamespace, gpuOperatorHelmValues())
	inv.Notes = append(inv.Notes, "AWS: autoscaler enabled with both the chart default and Kubernetes 1.34's override; GPU operator enabled with NIC's AL2023 defaults (driver/toolkit disabled). Cluster version and user image/chart overrides vary by deployment.", "AWS OpenTofu module 0.10.0 installs EKS-managed coredns, kube-proxy, vpc-cni, pod-identity-agent and EBS/EFS CSI add-ons without version pins. AWS selects their images at deployment time; scan a live cluster for those. Azure module 0.2.0 likewise delegates system/CSI/CNI images to AKS, including optional managed Karpenter. Neither module installs Helm releases.")
	inv.Save(t, "aws")
}
