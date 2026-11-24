#!/bin/sh
# Runs inside the LocalStack container once S3 is ready (/etc/localstack/init/ready.d).
# Creates the lab bucket. awslocal is preinstalled and targets the local endpoint.
set -e
BUCKET="${BUCKET:-acrynite-lab}"
awslocal s3 mb "s3://$BUCKET" 2>/dev/null || true
echo "acrynite-lab: S3 bucket '$BUCKET' ready"
