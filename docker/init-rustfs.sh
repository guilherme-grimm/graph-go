#!/bin/sh
# Seed data for graph-go development environment.
# Creates buckets and sample prefixes in RustFS over the S3 API, using curl's
# built-in SigV4 signing (the RustFS image ships curl, so no extra CLI image).

set -e

S3=http://rustfs:9000
sign() {
  curl --fail -sS --aws-sigv4 "aws:amz:us-east-1:s3" --user "$RUSTFS_ACCESS_KEY:$RUSTFS_SECRET_KEY" "$@"
}

# Create buckets (re-creating a bucket you own is a no-op)
sign -X PUT "$S3/assets"
sign -X PUT "$S3/backups"

# Create folder prefixes by uploading small placeholder files
for key in assets/images assets/documents backups/daily backups/weekly; do
  sign -X PUT --data-binary placeholder "$S3/$key/placeholder.txt"
done

echo "RustFS seed data initialized."
