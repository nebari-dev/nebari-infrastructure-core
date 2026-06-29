# Longhorn backup S3 bucket, provisioned only when create_bucket is set in
# backups.longhorn.s3 (and no external endpoint is configured). force_destroy is
# derived from the inverse of retain_on_destroy: when retain is on
# (force_destroy=false), `tofu destroy` refuses to delete a non-empty bucket,
# preserving backups — the cluster is replaceable, the bucket is the source of
# truth.
resource "aws_s3_bucket" "longhorn_backup" {
  count         = var.backup_bucket_create ? 1 : 0
  bucket        = var.backup_bucket_name
  force_destroy = var.backup_bucket_force_destroy
  tags          = var.tags
}

resource "aws_s3_bucket_versioning" "longhorn_backup" {
  count  = var.backup_bucket_create ? 1 : 0
  bucket = aws_s3_bucket.longhorn_backup[0].id
  versioning_configuration {
    status = "Enabled"
  }
}

resource "aws_s3_bucket_server_side_encryption_configuration" "longhorn_backup" {
  count  = var.backup_bucket_create ? 1 : 0
  bucket = aws_s3_bucket.longhorn_backup[0].id
  rule {
    apply_server_side_encryption_by_default {
      sse_algorithm = "AES256"
    }
  }
}
