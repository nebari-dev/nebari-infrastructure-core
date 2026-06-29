package config

import "strings"

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
		return "azblob://" + c.Azure.Container + "@core.windows.net/" + normalizePrefix(c.Azure.Prefix)
	default:
		return ""
	}
}
