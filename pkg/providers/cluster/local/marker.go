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

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/status"
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
// adopts the configured values so the next deploy is verifiable, and warns:
// the adopted values are unverified, and if they differ from the ports the
// cluster was really created with, the check now defends the wrong ones.
func verifyClusterPorts(ctx context.Context, client kubernetes.Interface, clusterName string, httpPort, httpsPort int32) error {
	marker, err := client.CoreV1().ConfigMaps(markerNamespace).Get(ctx, markerName, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return adoptClusterPorts(ctx, client, clusterName, httpPort, httpsPort)
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
			return adoptClusterPorts(ctx, client, clusterName, httpPort, httpsPort)
		}
		if recorded != strconv.Itoa(int(port.configured)) {
			return fmt.Errorf("kind cluster %s was created with %s %s but the config now says %d: kind port mappings are fixed at cluster creation, so restore the original value or recreate the cluster (nic destroy, then nic deploy)", clusterName, port.key, recorded, port.configured)
		}
	}
	return nil
}

// adoptClusterPorts records the configured ports for a cluster whose marker
// is missing or incomplete, and warns that they are unverified: kind cannot
// report the real mappings, so the config is taken on faith. The marker is
// normally written at creation, so this runs when it was deleted, when the
// creation-time write failed, or on a cluster created before the marker
// existed.
func adoptClusterPorts(ctx context.Context, client kubernetes.Interface, clusterName string, httpPort, httpsPort int32) error {
	status.Send(ctx, status.NewUpdate(status.LevelWarning, fmt.Sprintf("Kind cluster %s has no record of its host ports, recording http_port %d and https_port %d from the config unverified. If these differ from the ports the cluster was created with, recreate it (nic destroy, then nic deploy)", clusterName, httpPort, httpsPort)).
		WithResource("provider").
		WithAction("deploy").
		WithMetadata("cluster_name", clusterName))
	return recordClusterPorts(ctx, client, httpPort, httpsPort)
}
