// Package deployinfo records which NIC build produced a cluster.
//
// NIC's version and commit are injected into the binary at build time and
// printed by `nic version`, but that only answers the question on the machine
// holding the binary. Triage starts from the other end: someone has kubectl
// access to a misbehaving cluster and needs to know which NIC build deployed
// it, without SSH'ing to a bastion and hoping nobody rebuilt the binary since
// the last deploy.
//
// This package writes that answer into the cluster itself, as a ConfigMap that
// `nic deploy` upserts on every run.
package deployinfo

import (
	"context"
	"fmt"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/status"
)

const (
	// Namespace and Name locate the ConfigMap. They are deliberately fixed
	// rather than configurable: the whole point is that an operator holding
	// only kubectl can find it without knowing anything about the config that
	// produced the cluster.
	Namespace = "kube-system"
	Name      = "nic-deployment-info"

	// managedByLabel matches the marker every other NIC-created object carries,
	// so the ConfigMap shows up in the same `-l` selector as the rest.
	managedByLabel = "app.kubernetes.io/managed-by"
	managedByValue = "nebari-infrastructure-core"
)

// Build identifies the NIC binary that ran the deploy. The values come from the
// -ldflags -X variables in package main, so they are passed in from the cmd
// layer rather than read here; a source build with no ldflags yields the same
// "dev"/"none"/"unknown" defaults that `nic version` prints.
type Build struct {
	Version string
	Commit  string
	Date    string
}

// Info is everything recorded about a deploy.
type Info struct {
	// Build is the NIC binary's identity.
	Build Build

	// ClusterProvider is the cluster provider that provisioned the cluster
	// (e.g. "aws"). Recorded because a cluster's provider is not always
	// obvious from inside it, and several triage paths branch on it.
	ClusterProvider string

	// ProjectName is the deployment's project_name, which ties the cluster back
	// to the config file and to the provider's state.
	ProjectName string

	// LastDeploy is when this deploy ran. Callers pass it explicitly so the
	// value is testable and so a single deploy stamps one consistent time.
	LastDeploy time.Time
}

// Data renders Info as ConfigMap data. Keys are stable API: anything reading
// this ConfigMap (runbooks, support scripts, future `nic` subcommands) depends
// on them, so they are only ever added to, never renamed. Empty values are
// still written, so a reader can tell "NIC did not record this" from "this key
// belongs to a newer NIC than the one that deployed".
func (i Info) Data() map[string]string {
	return map[string]string{
		"nic-version":           i.Build.Version,
		"nic-commit":            i.Build.Commit,
		"nic-build-date":        i.Build.Date,
		"cluster-provider":      i.ClusterProvider,
		"project-name":          i.ProjectName,
		"last-deploy-timestamp": i.LastDeploy.UTC().Format(time.RFC3339),
	}
}

// ConfigMap renders Info as the object Apply upserts. Exported so callers can
// render the manifest without a cluster (and so tests can assert on it).
func (i Info) ConfigMap() *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      Name,
			Namespace: Namespace,
			Labels: map[string]string{
				managedByLabel: managedByValue,
			},
		},
		Data: i.Data(),
	}
}

// Apply upserts the deployment-info ConfigMap.
//
// Every deploy overwrites the previous values: this records the build that last
// deployed the cluster, not a history of builds. Redeploying with the same
// binary is a no-op beyond the timestamp.
//
// Callers are expected to treat a failure here as a warning rather than a
// failed deploy - the cluster is fine, only its provenance record is missing -
// so the error is descriptive enough to explain what was lost.
func Apply(ctx context.Context, client kubernetes.Interface, info Info) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "deployinfo.Apply")
	defer span.End()

	span.SetAttributes(
		attribute.String("nic.version", info.Build.Version),
		attribute.String("nic.commit", info.Build.Commit),
		attribute.String("cluster.provider", info.ClusterProvider),
	)

	cm := info.ConfigMap()
	configMaps := client.CoreV1().ConfigMaps(Namespace)

	existing, err := configMaps.Get(ctx, Name, metav1.GetOptions{})
	if err != nil {
		if !apierrors.IsNotFound(err) {
			// A permission or transient error must not be read as "absent":
			// blindly creating would mask the real failure.
			span.RecordError(err)
			return fmt.Errorf("get configmap %s/%s: %w", Namespace, Name, err)
		}
		if _, err := configMaps.Create(ctx, cm, metav1.CreateOptions{}); err != nil {
			span.RecordError(err)
			return fmt.Errorf("create configmap %s/%s: %w", Namespace, Name, err)
		}
		sendRecorded(ctx, info, "created")
		return nil
	}

	// Reconcile data and the managed-by label, leaving any labels or
	// annotations someone else added in place.
	existing.Data = cm.Data
	if existing.Labels == nil {
		existing.Labels = map[string]string{}
	}
	existing.Labels[managedByLabel] = managedByValue
	if _, err := configMaps.Update(ctx, existing, metav1.UpdateOptions{}); err != nil {
		span.RecordError(err)
		return fmt.Errorf("update configmap %s/%s: %w", Namespace, Name, err)
	}
	sendRecorded(ctx, info, "updated")
	return nil
}

// sendRecorded reports the write on the status channel. The version is echoed
// so the deploy log itself carries the build identity, which makes a captured
// log as useful as the ConfigMap for the same triage question.
func sendRecorded(ctx context.Context, info Info, action string) {
	status.Send(ctx, status.NewUpdate(status.LevelInfo, fmt.Sprintf("Recorded NIC deployment metadata (%s)", info.Build.Version)).
		WithResource(Name).
		WithAction(action).
		WithMetadata("nic_version", info.Build.Version).
		WithMetadata("nic_commit", info.Build.Commit))
}
