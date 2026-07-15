package aws

import (
	"fmt"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

// newK8sClient creates a Kubernetes clientset from kubeconfig bytes.
// Used by post-deploy steps (Longhorn, EFS StorageClass) that need to
// interact with the cluster after terraform provisioning.
func newK8sClient(kubeconfigBytes []byte) (*kubernetes.Clientset, error) {
	restConfig, err := clientcmd.RESTConfigFromKubeConfig(kubeconfigBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse kubeconfig: %w", err)
	}
	return kubernetes.NewForConfig(restConfig)
}
