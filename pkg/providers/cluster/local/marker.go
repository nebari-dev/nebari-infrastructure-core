package local

import (
	"context"
	"fmt"
	"strconv"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	kindcluster "sigs.k8s.io/kind/pkg/cluster"
)

// The provisioning marker records inputs that kind consumes only at cluster
// creation, so a later deploy can compare its config against what the
// cluster was actually built with. kind offers no way to read the host port
// mappings back from a running cluster, and they are invisible from inside
// it, so the record made at creation is the only source. The pattern follows
// kubeadm's kubeadm-config ConfigMap: provisioning-time inputs live in the
// cluster itself, not in host state a later deploy may not have.
const (
	markerNamespace = metav1.NamespaceSystem
	markerName      = "nic-local-cluster"
	markerHTTPKey   = "http_port"
	markerHTTPSKey  = "https_port"
)

// clusterClient builds a Kubernetes client for the named kind cluster.
func clusterClient(kp *kindcluster.Provider, name string) (kubernetes.Interface, error) {
	kubeconfig, err := kp.KubeConfig(name, false)
	if err != nil {
		return nil, fmt.Errorf("get kubeconfig for kind cluster %s: %w", name, err)
	}
	restCfg, err := clientcmd.RESTConfigFromKubeConfig([]byte(kubeconfig))
	if err != nil {
		return nil, fmt.Errorf("parse kubeconfig for kind cluster %s: %w", name, err)
	}
	client, err := kubernetes.NewForConfig(restCfg)
	if err != nil {
		return nil, fmt.Errorf("build kubernetes client for kind cluster %s: %w", name, err)
	}
	return client, nil
}

// recordClusterPorts writes the effective host ports into the provisioning
// marker, creating it or overwriting an existing one.
func recordClusterPorts(ctx context.Context, client kubernetes.Interface, httpPort, httpsPort int32) error {
	marker := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      markerName,
			Namespace: markerNamespace,
			Labels: map[string]string{
				"app.kubernetes.io/managed-by": "nebari-infrastructure-core",
			},
		},
		Data: map[string]string{
			markerHTTPKey:  strconv.Itoa(int(httpPort)),
			markerHTTPSKey: strconv.Itoa(int(httpsPort)),
		},
	}
	_, err := client.CoreV1().ConfigMaps(markerNamespace).Create(ctx, marker, metav1.CreateOptions{})
	if apierrors.IsAlreadyExists(err) {
		_, err = client.CoreV1().ConfigMaps(markerNamespace).Update(ctx, marker, metav1.UpdateOptions{})
	}
	if err != nil {
		return fmt.Errorf("record the cluster provisioning marker: %w", err)
	}
	return nil
}

// verifyClusterPorts compares the effective configured host ports against the
// marker recorded when the cluster was created, and returns an error when
// they differ. The mappings cannot change on a live cluster, so deploying
// would move the gateway listeners and printed URLs to ports the host does
// not publish. A cluster without a marker (or with a marker missing a key)
// predates it, so the configured values are adopted and recorded to make the
// next deploy verifiable.
func verifyClusterPorts(ctx context.Context, client kubernetes.Interface, clusterName string, httpPort, httpsPort int32) error {
	marker, err := client.CoreV1().ConfigMaps(markerNamespace).Get(ctx, markerName, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return recordClusterPorts(ctx, client, httpPort, httpsPort)
	}
	if err != nil {
		return fmt.Errorf("read the cluster provisioning marker: %w", err)
	}

	for _, port := range []struct {
		key        string
		configured int32
	}{
		{markerHTTPKey, httpPort},
		{markerHTTPSKey, httpsPort},
	} {
		recorded := marker.Data[port.key]
		if recorded == "" {
			return recordClusterPorts(ctx, client, httpPort, httpsPort)
		}
		if recorded != strconv.Itoa(int(port.configured)) {
			return fmt.Errorf("kind cluster %s was created with %s %s but the config now says %d: kind port mappings are fixed at cluster creation, so restore the original value or recreate the cluster (nic destroy, then nic deploy)", clusterName, port.key, recorded, port.configured)
		}
	}
	return nil
}
