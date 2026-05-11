package longhorn

import (
	"context"
	"fmt"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/status"
)

const iscsiDaemonSetTimeout = 3 * time.Minute

// iscsiDaemonSetYAML is the Longhorn iSCSI prerequisite DaemonSet.
// Source: https://github.com/longhorn/longhorn/blob/v1.9.2/deploy/prerequisite/longhorn-iscsi-installation.yaml
// (removed upstream in v1.10.0; see longhorn/longhorn@600801b5 — the
// embedded DaemonSet content still works against newer engine versions.)
// Embedded to avoid runtime HTTP fetches and support air-gapped installs.
const iscsiDaemonSetYAML = `apiVersion: apps/v1
kind: DaemonSet
metadata:
  name: longhorn-iscsi-installation
  namespace: longhorn-system
  labels:
    app: longhorn-iscsi-installation
spec:
  selector:
    matchLabels:
      app: longhorn-iscsi-installation
  template:
    metadata:
      labels:
        app: longhorn-iscsi-installation
    spec:
      hostNetwork: true
      hostPID: true
      initContainers:
      - name: iscsi-installation
        command:
          - nsenter
          - --mount=/proc/1/ns/mnt
          - --
          - bash
          - -c
          - |
            OS=$(grep -E "^ID_LIKE=" /etc/os-release | cut -d '=' -f 2)
            if [[ -z "${OS}" ]]; then
              OS=$(grep -E "^ID=" /etc/os-release | cut -d '=' -f 2)
            fi
            if [[ "${OS}" == *"debian"* ]]; then
              sudo apt-get update -q -y && sudo apt-get install -q -y open-iscsi && sudo systemctl -q enable iscsid && sudo systemctl start iscsid && sudo modprobe iscsi_tcp
            elif [[ "${OS}" == *"suse"* ]]; then
              sudo zypper --gpg-auto-import-keys -q refresh && sudo zypper --gpg-auto-import-keys -q install -y open-iscsi && sudo systemctl -q enable iscsid && sudo systemctl start iscsid && sudo modprobe iscsi_tcp
            else
              sudo yum makecache -q -y && sudo yum --setopt=tsflags=noscripts install -q -y iscsi-initiator-utils && echo "InitiatorName=$(/sbin/iscsi-iname)" > /etc/iscsi/initiatorname.iscsi && sudo systemctl -q enable iscsid && sudo systemctl start iscsid && sudo modprobe iscsi_tcp
            fi
            if [ $? -eq 0 ]; then echo "iscsi install successfully"; else echo "iscsi install failed error code $?"; fi
        image: alpine:3.17
        securityContext:
          privileged: true
      containers:
      - name: sleep
        image: registry.k8s.io/pause:3.1
  updateStrategy:
    type: RollingUpdate
`

// newK8sClient builds a Kubernetes client from raw kubeconfig bytes.
func newK8sClient(kubeconfigBytes []byte) (*kubernetes.Clientset, error) {
	restConfig, err := clientcmd.RESTConfigFromKubeConfig(kubeconfigBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse kubeconfig: %w", err)
	}
	return kubernetes.NewForConfig(restConfig)
}

// ensureISCSI deploys the Longhorn iSCSI prerequisite DaemonSet and waits
// for all pods to become ready. Required for nodes that don't ship with
// open-iscsi/iscsi-initiator-utils (Amazon Linux 2023, k3s minimal images).
func ensureISCSI(ctx context.Context, kubeconfigBytes []byte) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "longhorn.ensureISCSI")
	defer span.End()

	status.Send(ctx, status.NewUpdate(status.LevelProgress, "Installing iSCSI prerequisites on cluster nodes").
		WithResource("iscsi-daemonset").
		WithAction("installing"))

	client, err := newK8sClient(kubeconfigBytes)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to create Kubernetes client: %w", err)
	}

	if err := ensureISCSIWithClient(ctx, client); err != nil {
		span.RecordError(err)
		return err
	}
	return nil
}

// ensureISCSIWithClient is the testable inner form of ensureISCSI; takes a
// kubernetes.Interface so unit tests can inject a fake client. Errors are
// recorded against the parent span (longhorn.ensureISCSI) via the inherited
// ctx — no separate span here since this is the same logical operation.
func ensureISCSIWithClient(ctx context.Context, client kubernetes.Interface) error {
	if err := ensureNamespace(ctx, client, Namespace); err != nil {
		return fmt.Errorf("failed to ensure namespace %s: %w", Namespace, err)
	}

	var ds appsv1.DaemonSet
	if err := yaml.NewYAMLOrJSONDecoder(
		strings.NewReader(iscsiDaemonSetYAML), 4096,
	).Decode(&ds); err != nil {
		return fmt.Errorf("failed to parse iSCSI DaemonSet YAML: %w", err)
	}

	existing, err := client.AppsV1().DaemonSets(Namespace).Get(ctx, ds.Name, metav1.GetOptions{})
	switch {
	case k8serrors.IsNotFound(err):
		status.Send(ctx, status.NewUpdate(status.LevelProgress, "Creating iSCSI prerequisite DaemonSet").
			WithResource("iscsi-daemonset").
			WithAction("creating"))
		if _, err := client.AppsV1().DaemonSets(Namespace).Create(ctx, &ds, metav1.CreateOptions{}); err != nil {
			return fmt.Errorf("failed to create iSCSI DaemonSet: %w", err)
		}
	case err != nil:
		return fmt.Errorf("failed to get iSCSI DaemonSet: %w", err)
	default:
		status.Send(ctx, status.NewUpdate(status.LevelInfo, "Updating existing iSCSI prerequisite DaemonSet").
			WithResource("iscsi-daemonset").
			WithAction("updating"))
		ds.ResourceVersion = existing.ResourceVersion
		if _, err := client.AppsV1().DaemonSets(Namespace).Update(ctx, &ds, metav1.UpdateOptions{}); err != nil {
			return fmt.Errorf("failed to update iSCSI DaemonSet: %w", err)
		}
	}

	if err := waitForDaemonSetReady(ctx, client, Namespace, ds.Name, iscsiDaemonSetTimeout); err != nil {
		return fmt.Errorf("iSCSI DaemonSet not ready: %w", err)
	}

	status.Send(ctx, status.NewUpdate(status.LevelSuccess, "iSCSI prerequisites installed on all nodes").
		WithResource("iscsi-daemonset").
		WithAction("ready"))

	return nil
}

func ensureNamespace(ctx context.Context, client kubernetes.Interface, namespace string) error {
	_, err := client.CoreV1().Namespaces().Get(ctx, namespace, metav1.GetOptions{})
	if k8serrors.IsNotFound(err) {
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{Name: namespace},
		}
		_, err = client.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})
		if err != nil {
			return fmt.Errorf("failed to create namespace %s: %w", namespace, err)
		}
		return nil
	}
	return err
}

func waitForDaemonSetReady(ctx context.Context, client kubernetes.Interface, namespace, name string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	checkReady := func() (bool, error) {
		ds, err := client.AppsV1().DaemonSets(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		return ds.Status.DesiredNumberScheduled > 0 &&
			ds.Status.DesiredNumberScheduled == ds.Status.NumberReady, nil
	}

	if ready, err := checkReady(); err == nil && ready {
		return nil
	}

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for DaemonSet %s/%s: %w", namespace, name, ctx.Err())
		case <-ticker.C:
			ready, err := checkReady()
			if err != nil {
				continue
			}
			if ready {
				return nil
			}
		}
	}
}
