package aws

import (
	"context"
	"fmt"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/cli"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/helm"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/status"
)

const (
	longhornRepoName     = "longhorn"
	longhornRepoURL      = "https://charts.longhorn.io"
	longhornChartName    = "longhorn/longhorn"
	longhornChartVersion = "1.8.1"
	longhornNamespace    = "longhorn-system"
	longhornReleaseName  = "longhorn"
	longhornTimeout      = 10 * time.Minute

	iscsiDaemonSetTimeout = 3 * time.Minute

	// iscsiDaemonSetYAML is the Longhorn iSCSI prerequisite DaemonSet.
	// Source: https://github.com/longhorn/longhorn/blob/v1.8.1/deploy/prerequisite/longhorn-iscsi-installation.yaml
	// Embedded to avoid runtime HTTP fetches and support air-gapped installs.
	iscsiDaemonSetYAML = `apiVersion: apps/v1
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
)

// ensureISCSI deploys the Longhorn iSCSI prerequisite DaemonSet and waits
// for all pods to become ready. This is required on Amazon Linux 2023 nodes
// which do not ship with open-iscsi/iscsi-initiator-utils.
func ensureISCSI(ctx context.Context, kubeconfigBytes []byte) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "aws.ensureISCSI")
	defer span.End()

	status.Send(ctx, status.NewUpdate(status.LevelProgress, "Installing iSCSI prerequisites on cluster nodes").
		WithResource("iscsi-daemonset").
		WithAction("installing"))

	client, err := newLonghornK8sClient(kubeconfigBytes)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to create Kubernetes client: %w", err)
	}

	return ensureISCSIWithClient(ctx, client)
}

// ensureISCSIWithClient performs the iSCSI DaemonSet deployment using the
// provided Kubernetes client interface. Separated from ensureISCSI to allow
// testing with fake clients.
func ensureISCSIWithClient(ctx context.Context, client kubernetes.Interface) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "aws.ensureISCSIWithClient")
	defer span.End()

	// Create the longhorn-system namespace if it doesn't exist
	if err := ensureNamespace(ctx, client, longhornNamespace); err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to ensure namespace %s: %w", longhornNamespace, err)
	}

	// Parse the embedded DaemonSet YAML
	var ds appsv1.DaemonSet
	if err := yaml.NewYAMLOrJSONDecoder(
		strings.NewReader(iscsiDaemonSetYAML), 4096,
	).Decode(&ds); err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to parse iSCSI DaemonSet YAML: %w", err)
	}

	// Create or update the DaemonSet for idempotency
	existing, err := client.AppsV1().DaemonSets(longhornNamespace).Get(ctx, ds.Name, metav1.GetOptions{})
	switch {
	case k8serrors.IsNotFound(err):
		status.Send(ctx, status.NewUpdate(status.LevelProgress, "Creating iSCSI prerequisite DaemonSet").
			WithResource("iscsi-daemonset").
			WithAction("creating"))
		if _, err := client.AppsV1().DaemonSets(longhornNamespace).Create(ctx, &ds, metav1.CreateOptions{}); err != nil {
			span.RecordError(err)
			return fmt.Errorf("failed to create iSCSI DaemonSet: %w", err)
		}
	case err != nil:
		span.RecordError(err)
		return fmt.Errorf("failed to get iSCSI DaemonSet: %w", err)
	default:
		status.Send(ctx, status.NewUpdate(status.LevelInfo, "Updating existing iSCSI prerequisite DaemonSet").
			WithResource("iscsi-daemonset").
			WithAction("updating"))
		ds.ResourceVersion = existing.ResourceVersion
		if _, err := client.AppsV1().DaemonSets(longhornNamespace).Update(ctx, &ds, metav1.UpdateOptions{}); err != nil {
			span.RecordError(err)
			return fmt.Errorf("failed to update iSCSI DaemonSet: %w", err)
		}
	}

	// Poll until all DaemonSet pods are ready
	if err := waitForDaemonSetReady(ctx, client, longhornNamespace, ds.Name, iscsiDaemonSetTimeout); err != nil {
		span.RecordError(err)
		return fmt.Errorf("iSCSI DaemonSet not ready: %w", err)
	}

	status.Send(ctx, status.NewUpdate(status.LevelSuccess, "iSCSI prerequisites installed on all nodes").
		WithResource("iscsi-daemonset").
		WithAction("ready"))

	return nil
}

// ensureNamespace creates a namespace if it doesn't already exist.
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

// waitForDaemonSetReady polls until the DaemonSet has all desired pods ready.
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

	// Immediate check
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

// newLonghornK8sClient creates a Kubernetes clientset from kubeconfig bytes.
func newLonghornK8sClient(kubeconfigBytes []byte) (*kubernetes.Clientset, error) {
	restConfig, err := clientcmd.RESTConfigFromKubeConfig(kubeconfigBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse kubeconfig: %w", err)
	}
	return kubernetes.NewForConfig(restConfig)
}

// longhornHelmValues generates the Helm values map for the Longhorn chart
// based on the AWS provider configuration.
func longhornHelmValues(cfg *Config) map[string]any {
	replicaCount := cfg.LonghornReplicaCount()

	persistence := map[string]any{
		"defaultClass":             true,
		"defaultClassReplicaCount": replicaCount,
		"defaultFsType":            "ext4",
	}

	settings := map[string]any{
		"replicaZoneSoftAntiAffinity": "true",
		"replicaAutoBalance":          "best-effort",
	}

	values := map[string]any{
		"persistence":     persistence,
		"defaultSettings": settings,
	}

	if cfg.Longhorn != nil && cfg.Longhorn.DedicatedNodes {
		settings["createDefaultDiskLabeledNodes"] = true

		nodeSelector := map[string]string{"node.longhorn.io/storage": "true"}
		if cfg.Longhorn.NodeSelector != nil {
			nodeSelector = cfg.Longhorn.NodeSelector
		}

		tolerations := []map[string]string{
			{
				"key":      "node.longhorn.io/storage",
				"operator": "Exists",
				"effect":   "NoSchedule",
			},
		}

		values["longhornManager"] = map[string]any{
			"nodeSelector": nodeSelector,
			"tolerations":  tolerations,
		}
		values["longhornDriver"] = map[string]any{
			"nodeSelector": nodeSelector,
			"tolerations":  tolerations,
		}
	}

	return values
}

// loadLonghornChart locates and loads the Longhorn Helm chart.
// This is extracted to avoid duplication between install and upgrade operations.
func loadLonghornChart(chartPathOptions action.ChartPathOptions) (*chart.Chart, error) {
	chartPath, err := chartPathOptions.LocateChart(longhornChartName, cli.New())
	if err != nil {
		return nil, fmt.Errorf("failed to locate Longhorn chart: %w", err)
	}

	loadedChart, err := loader.Load(chartPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load Longhorn chart: %w", err)
	}

	return loadedChart, nil
}

// installLonghorn installs or upgrades Longhorn on the cluster via Helm.
func installLonghorn(ctx context.Context, kubeconfigBytes []byte, cfg *Config) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "aws.installLonghorn")
	defer span.End()

	span.SetAttributes(
		attribute.String("chart_version", longhornChartVersion),
		attribute.Int("replica_count", cfg.LonghornReplicaCount()),
		attribute.Bool("dedicated_nodes", cfg.Longhorn != nil && cfg.Longhorn.DedicatedNodes),
	)

	// Install iSCSI prerequisites on all nodes before Longhorn
	if err := ensureISCSI(ctx, kubeconfigBytes); err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to install iSCSI prerequisites: %w", err)
	}

	kubeconfigPath, cleanup, err := helm.WriteTempKubeconfig(kubeconfigBytes)
	if err != nil {
		span.RecordError(err)
		return err
	}
	defer cleanup()

	if err := helm.AddRepo(ctx, longhornRepoName, longhornRepoURL); err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to add Longhorn Helm repository: %w", err)
	}

	actionConfig, err := helm.NewActionConfig(kubeconfigPath, longhornNamespace)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to create Helm action config: %w", err)
	}

	// Check if release already exists (idempotency)
	histClient := action.NewHistory(actionConfig)
	histClient.Max = 1
	if _, err := histClient.Run(longhornReleaseName); err == nil {
		status.Send(ctx, status.NewUpdate(status.LevelInfo, "Longhorn already installed, upgrading").
			WithResource("longhorn").
			WithAction("upgrading"))
		return upgradeLonghorn(ctx, actionConfig, cfg)
	}

	helmValues := longhornHelmValues(cfg)

	status.Send(ctx, status.NewUpdate(status.LevelProgress, "Installing Longhorn storage").
		WithResource("longhorn").
		WithAction("installing").
		WithMetadata("chart_version", longhornChartVersion))

	client := action.NewInstall(actionConfig)
	client.Namespace = longhornNamespace
	client.ReleaseName = longhornReleaseName
	client.CreateNamespace = true
	client.Wait = true
	client.Timeout = longhornTimeout
	client.Version = longhornChartVersion

	loadedChart, err := loadLonghornChart(client.ChartPathOptions)
	if err != nil {
		span.RecordError(err)
		return err
	}

	release, err := client.Run(loadedChart, helmValues)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to install Longhorn: %w", err)
	}

	span.SetAttributes(
		attribute.String("release_status", string(release.Info.Status)),
		attribute.Int("release_version", release.Version),
	)

	status.Send(ctx, status.NewUpdate(status.LevelSuccess, "Longhorn storage installed").
		WithResource("longhorn").
		WithAction("installed").
		WithMetadata("chart_version", longhornChartVersion))

	return nil
}

// upgradeLonghorn upgrades an existing Longhorn installation.
func upgradeLonghorn(ctx context.Context, actionConfig *action.Configuration, cfg *Config) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "aws.upgradeLonghorn")
	defer span.End()

	helmValues := longhornHelmValues(cfg)

	client := action.NewUpgrade(actionConfig)
	client.Namespace = longhornNamespace
	client.Wait = true
	client.Timeout = longhornTimeout
	client.Version = longhornChartVersion

	loadedChart, err := loadLonghornChart(client.ChartPathOptions)
	if err != nil {
		span.RecordError(err)
		return err
	}

	release, err := client.Run(longhornReleaseName, loadedChart, helmValues)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to upgrade Longhorn: %w", err)
	}

	span.SetAttributes(
		attribute.String("release_status", string(release.Info.Status)),
		attribute.Int("release_version", release.Version),
	)

	status.Send(ctx, status.NewUpdate(status.LevelSuccess, "Longhorn storage upgraded").
		WithResource("longhorn").
		WithAction("upgraded").
		WithMetadata("chart_version", longhornChartVersion))

	return nil
}
