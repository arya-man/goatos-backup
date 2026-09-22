#!/usr/bin/env bash
# Month-to-date OCI cost by service, to prove the dashboard automation VM stays
# inside Always Free. Uses the OCI_CLI_PROFILE API-key profile (default BILLING).
# The private key comes from Google Secret Manager; nothing secret lives in the repo.
set -euo pipefail

PROFILE="${OCI_CLI_PROFILE:-BILLING}"
FROM="${1:-$(date -u +%Y-%m-01)}"
TO="${2:-$(date -u -v+1d +%Y-%m-%d 2>/dev/null || date -u -d tomorrow +%Y-%m-%d)}"
TENANCY="$(awk -v p="[$PROFILE]" '$0==p{f=1;next} /^\[/{f=0} f&&/^tenancy=/{sub(/^tenancy=/,"");print;exit}' "${OCI_CLI_CONFIG_FILE:-$HOME/.oci/config}")"
[[ -n "$TENANCY" ]] || { echo "oci-billing: profile [$PROFILE] has no tenancy in ~/.oci/config" >&2; exit 2; }

oci usage-api usage-summary request-summarized-usages \
  --profile "$PROFILE" \
  --tenant-id "$TENANCY" \
  --time-usage-started "${FROM}T00:00:00Z" \
  --time-usage-ended "${TO}T00:00:00Z" \
  --granularity MONTHLY \
  --query-type COST \
  --group-by '["service"]' \
  --query 'data.items[].{service:service,amount:"computed-amount",currency:currency}' \
  --output table
