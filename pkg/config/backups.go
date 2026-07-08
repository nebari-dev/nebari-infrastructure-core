package config

import (
	"fmt"
	"regexp"
	"strings"
)

// BackupsConfig is the top-level `backups:` block. Today it only carries
// Longhorn backup configuration, but the block exists to group future backup
// concerns under one key.
type BackupsConfig struct {
	Longhorn *LonghornBackupConfig `yaml:"longhorn,omitempty"`
}

// LonghornBackupConfig drives the Longhorn snapshot/backup schedules, the
// cluster Setting, and the S3/azblob BackupTarget + credential Secret. Exactly
// one of S3 / Azure may be set when Enabled.
type LonghornBackupConfig struct {
	Enabled *bool              `yaml:"enabled,omitempty"`
	S3      *S3BackupTarget    `yaml:"s3,omitempty"`
	Azure   *AzureBackupTarget `yaml:"azure,omitempty"`

	// AllowRecurringJobWhileVolumeDetached maps to the cluster-wide Longhorn
	// Setting. nil defaults to true (the pack's behaviour): JupyterHub user PVCs
	// detach when servers idle out, and Longhorn's stock default of false would
	// silently skip them at the cron tick.
	AllowRecurringJobWhileVolumeDetached *bool `yaml:"allow_recurring_job_while_volume_detached,omitempty"`

	Schedules BackupSchedules `yaml:"schedules,omitempty"`
}

// S3BackupTarget configures an AWS-native or S3-compatible backup target.
type S3BackupTarget struct {
	Bucket             string     `yaml:"bucket"`
	Region             string     `yaml:"region"`
	Prefix             string     `yaml:"prefix,omitempty"`
	CreateBucket       bool       `yaml:"create_bucket,omitempty"`
	RetainOnDestroy    *bool      `yaml:"retain_on_destroy,omitempty"`
	Endpoint           string     `yaml:"endpoint,omitempty"`
	VirtualHostedStyle bool       `yaml:"virtual_hosted_style,omitempty"`
	AccessKeyIDEnv     string     `yaml:"access_key_id_env"`
	SecretAccessKeyEnv string     `yaml:"secret_access_key_env"`
	CACert             *CACertRef `yaml:"ca_cert,omitempty"`
}

// AzureBackupTarget configures a Longhorn-native azblob:// backup target.
type AzureBackupTarget struct {
	Container       string `yaml:"container"`
	StorageAccount  string `yaml:"storage_account"`
	Prefix          string `yaml:"prefix,omitempty"`
	CreateContainer bool   `yaml:"create_container,omitempty"`
	RetainOnDestroy *bool  `yaml:"retain_on_destroy,omitempty"`
	Endpoint        string `yaml:"endpoint,omitempty"`
	AccountNameEnv  string `yaml:"account_name_env"`
	AccountKeyEnv   string `yaml:"account_key_env"`
}

// CACertRef references a pre-existing Secret or ConfigMap key holding a PEM CA
// bundle. NIC reads it at deploy time and injects it as the AWS_CERT key.
type CACertRef struct {
	Kind      string `yaml:"kind"` // "secret" | "configmap"
	Name      string `yaml:"name"`
	Namespace string `yaml:"namespace,omitempty"`
	Key       string `yaml:"key"`
}

// BackupSchedules holds the two RecurringJob schedules.
type BackupSchedules struct {
	Snapshot ScheduleConfig `yaml:"snapshot"`
	Backup   ScheduleConfig `yaml:"backup"`
}

// ScheduleConfig is one RecurringJob's cron/retain/concurrency.
type ScheduleConfig struct {
	Cron        string `yaml:"cron"`
	Retain      int    `yaml:"retain"`
	Concurrency int    `yaml:"concurrency"`
}

// LonghornEnabled reports whether Longhorn backups should be configured. A nil
// BackupsConfig or nil Longhorn block is disabled (backups are opt-in).
func (c *BackupsConfig) LonghornEnabled() bool {
	if c == nil || c.Longhorn == nil {
		return false
	}
	return c.Longhorn.IsEnabled()
}

// LonghornConfig returns the nested Longhorn config, nil-safe.
func (c *BackupsConfig) LonghornConfig() *LonghornBackupConfig {
	if c == nil {
		return nil
	}
	return c.Longhorn
}

// IsEnabled reports whether this Longhorn backup block is enabled. Unlike the
// install-side longhorn.Config (which defaults nil-Enabled to true), backups are
// opt-in: a present block with no `enabled` defaults to true, but a nil block is
// off (handled by BackupsConfig.LonghornEnabled).
func (c *LonghornBackupConfig) IsEnabled() bool {
	if c == nil {
		return false
	}
	if c.Enabled == nil {
		return true
	}
	return *c.Enabled
}

// AllowDetached returns the effective allow-recurring-job-while-volume-detached
// value, defaulting to true.
func (c *LonghornBackupConfig) AllowDetached() bool {
	if c == nil || c.AllowRecurringJobWhileVolumeDetached == nil {
		return true
	}
	return *c.AllowRecurringJobWhileVolumeDetached
}

// PodIdentityAuth reports whether this S3 target uses keyless IAM-role
// authentication (EKS Pod Identity) rather than static access keys. That path
// applies only to a native AWS S3 target — the aws provider with no custom
// endpoint — when neither credential env var is set. In that case NIC omits the
// AWS keys from the Longhorn credential Secret and provisions a Pod Identity
// association for Longhorn's service account so its AWS SDK resolves creds from
// the cluster role.
func (t *S3BackupTarget) PodIdentityAuth(providerName string) bool {
	if t == nil {
		return false
	}
	return providerName == "aws" &&
		t.Endpoint == "" &&
		t.AccessKeyIDEnv == "" &&
		t.SecretAccessKeyEnv == ""
}

// RetainOnDestroyEnabled defaults to true for an S3 target.
func (t *S3BackupTarget) RetainOnDestroyEnabled() bool {
	if t == nil || t.RetainOnDestroy == nil {
		return true
	}
	return *t.RetainOnDestroy
}

// RetainOnDestroyEnabled defaults to true for an Azure target.
func (t *AzureBackupTarget) RetainOnDestroyEnabled() bool {
	if t == nil || t.RetainOnDestroy == nil {
		return true
	}
	return *t.RetainOnDestroy
}

// normalizePrefix returns the prefix with a single trailing slash, or "" when
// empty. Longhorn requires the backupTargetURL to end in "/".
func normalizePrefix(p string) string {
	p = strings.Trim(p, "/")
	if p == "" {
		return ""
	}
	return p + "/"
}

// fiveFieldCron matches a 5-field cron expression (same guard the
// nebari-longhorn-backup-pack chart used). It is intentionally permissive about
// field contents — it only enforces the 5-field shape.
var fiveFieldCron = regexp.MustCompile(`^(\S+\s+){4}\S+$`)

// providersWithBucketModule lists cluster providers whose OpenTofu module can
// provision a backup bucket/container. Others must use an external endpoint.
var providersWithBucketModule = map[string]bool{"aws": true, "azure": true}

// Validate checks the backups block. nil-safe: a nil BackupsConfig (or disabled
// Longhorn block) validates clean. providerName is the selected cluster provider.
func (c *BackupsConfig) Validate(providerName string) error {
	if c == nil || c.Longhorn == nil {
		return nil
	}
	if !c.Longhorn.IsEnabled() {
		return nil
	}
	return c.Longhorn.Validate(providerName)
}

// Validate checks a single Longhorn backup block. Assumes the block is enabled.
func (c *LonghornBackupConfig) Validate(providerName string) error {
	// Exactly one target.
	targets := 0
	if c.S3 != nil {
		targets++
	}
	if c.Azure != nil {
		targets++
	}
	if targets != 1 {
		return fmt.Errorf("backups.longhorn: exactly one of s3 / azure must be set")
	}

	if err := validateSchedule("snapshot", c.Schedules.Snapshot); err != nil {
		return err
	}
	if err := validateSchedule("backup", c.Schedules.Backup); err != nil {
		return err
	}

	if c.S3 != nil {
		return c.S3.validate(providerName)
	}
	return c.Azure.validate(providerName)
}

func validateSchedule(name string, s ScheduleConfig) error {
	if !fiveFieldCron.MatchString(s.Cron) {
		return fmt.Errorf("backups.longhorn.schedules.%s.cron is not a valid 5-field cron expression: %q", name, s.Cron)
	}
	if s.Retain <= 0 {
		return fmt.Errorf("backups.longhorn.schedules.%s.retain must be > 0 (got %d)", name, s.Retain)
	}
	if s.Concurrency <= 0 {
		return fmt.Errorf("backups.longhorn.schedules.%s.concurrency must be > 0 (got %d)", name, s.Concurrency)
	}
	return nil
}

func (t *S3BackupTarget) validate(providerName string) error {
	if t.Bucket == "" {
		return fmt.Errorf("backups.longhorn.s3.bucket is required")
	}
	if t.Region == "" {
		return fmt.Errorf("backups.longhorn.s3.region is required")
	}
	// Credentials are optional only for a real AWS S3 target (aws provider, no
	// custom endpoint): omitting both env vars selects keyless auth, where
	// Longhorn assumes an IAM role via the EKS Pod Identity association NIC
	// provisions for its service account (see PodIdentityAuth). For any other
	// target — a non-aws provider, or an S3-compatible endpoint (e.g. Hetzner) —
	// Pod Identity cannot reach the store, so static keys stay required.
	switch {
	case t.AccessKeyIDEnv == "" && t.SecretAccessKeyEnv == "":
		if !t.PodIdentityAuth(providerName) {
			return fmt.Errorf("backups.longhorn.s3.access_key_id_env and secret_access_key_env are required (keyless IAM-role auth is only available for a native AWS S3 target: the aws provider with no custom endpoint)")
		}
	case t.AccessKeyIDEnv == "" || t.SecretAccessKeyEnv == "":
		return fmt.Errorf("backups.longhorn.s3: set both access_key_id_env and secret_access_key_env, or neither (neither selects keyless IAM-role auth on AWS)")
	}
	if t.CreateBucket {
		if t.Endpoint != "" {
			return fmt.Errorf("backups.longhorn.s3: create_bucket cannot be set when endpoint is set (an external bucket is never created by NIC)")
		}
		if !providersWithBucketModule[providerName] {
			return fmt.Errorf("backups.longhorn.s3.create_bucket is only supported on providers with a Terraform module (aws, azure); provider %q must reference a pre-existing bucket via endpoint", providerName)
		}
	}
	if t.CACert != nil {
		if err := t.CACert.validate(); err != nil {
			return err
		}
	}
	return nil
}

func (t *AzureBackupTarget) validate(providerName string) error {
	if t.Container == "" {
		return fmt.Errorf("backups.longhorn.azure.container is required")
	}
	if t.StorageAccount == "" {
		return fmt.Errorf("backups.longhorn.azure.storage_account is required")
	}
	if t.AccountNameEnv == "" {
		return fmt.Errorf("backups.longhorn.azure.account_name_env is required")
	}
	if t.AccountKeyEnv == "" {
		return fmt.Errorf("backups.longhorn.azure.account_key_env is required")
	}
	if t.CreateContainer && t.Endpoint != "" {
		return fmt.Errorf("backups.longhorn.azure: create_container cannot be set when endpoint is set (an external container is never created by NIC)")
	}
	if t.CreateContainer && providerName != "azure" {
		return fmt.Errorf("backups.longhorn.azure.create_container requires the azure provider (got %q)", providerName)
	}
	return nil
}

func (r *CACertRef) validate() error {
	switch r.Kind {
	case "secret", "configmap":
	default:
		return fmt.Errorf("backups.longhorn.s3.ca_cert.kind must be \"secret\" or \"configmap\" (got %q)", r.Kind)
	}
	if r.Name == "" {
		return fmt.Errorf("backups.longhorn.s3.ca_cert.name is required")
	}
	if r.Key == "" {
		return fmt.Errorf("backups.longhorn.s3.ca_cert.key is required")
	}
	return nil
}

// BackupTargetURL builds the Longhorn backupTargetURL for the configured target.
//   - S3:    s3://<bucket>@<region>/<prefix>
//   - azblob: azblob://<container>@core.windows.net/<prefix>
//
// Returns "" when no target is set.
func (c *LonghornBackupConfig) BackupTargetURL() string {
	switch {
	case c == nil:
		return ""
	case c.S3 != nil:
		return "s3://" + c.S3.Bucket + "@" + c.S3.Region + "/" + normalizePrefix(c.S3.Prefix)
	case c.Azure != nil:
		// The "core.windows.net" suffix is intentional even for custom endpoints:
		// Longhorn drives the real endpoint via the AZBLOB_ENDPOINT secret key, so
		// this URL suffix is cosmetic and does not need to match a custom endpoint.
		return "azblob://" + c.Azure.Container + "@core.windows.net/" + normalizePrefix(c.Azure.Prefix)
	default:
		return ""
	}
}
