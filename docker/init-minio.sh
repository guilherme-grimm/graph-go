#!/bin/sh
# Seed data for graph-info development environment.
# Creates buckets and sample prefixes in MinIO using the mc CLI.

set -e

# Configure mc alias for the local MinIO instance
mc alias set local http://minio:9000 minioadmin minioadmin

# Create buckets
mc mb --ignore-existing local/assets
mc mb --ignore-existing local/backups

# Create folder prefixes by uploading small placeholder files
echo "placeholder" | mc pipe local/assets/images/placeholder.txt
echo "placeholder" | mc pipe local/assets/documents/placeholder.txt
echo "placeholder" | mc pipe local/backups/daily/placeholder.txt
echo "placeholder" | mc pipe local/backups/weekly/placeholder.txt

echo "MinIO seed data initialized."
