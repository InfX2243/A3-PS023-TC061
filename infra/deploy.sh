#!/usr/bin/env bash
set -Eeuo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
STACK_NAME="${1:-}"
ADMIN_CIDR="${2:-}"
GIT_REF="${3:-feat/cloudformation-infrastructure-monitoring}"
REGION="${AWS_REGION:-${AWS_DEFAULT_REGION:-}}"

if [[ -z "$STACK_NAME" || -z "$ADMIN_CIDR" || -z "$REGION" ]]; then
  cat >&2 <<'USAGE'
Usage:
  AWS_REGION=ap-south-1 ./infra/deploy.sh <stack-name> <admin-ip-cidr> [git-ref]

Example:
  AWS_REGION=ap-south-1 ./infra/deploy.sh aiori-dev 203.0.113.10/32 feat/cloudformation-infrastructure-monitoring

The script uses your normal AWS CLI credential chain. It does not accept or store access keys.
USAGE
  exit 2
fi

if [[ "$ADMIN_CIDR" == "0.0.0.0/0" ]]; then
  echo "Refusing to expose the controller UI to the public internet." >&2
  exit 2
fi

aws cloudformation deploy \
  --template-file "$ROOT/aiori-compute.yaml" \
  --stack-name "$STACK_NAME" \
  --region "$REGION" \
  --capabilities CAPABILITY_IAM \
  --parameter-overrides "AdminCIDR=$ADMIN_CIDR" "GitRef=$GIT_REF"

aws cloudformation describe-stacks \
  --stack-name "$STACK_NAME" \
  --region "$REGION" \
  --query 'Stacks[0].Outputs[*].[OutputKey,OutputValue]' \
  --output table
