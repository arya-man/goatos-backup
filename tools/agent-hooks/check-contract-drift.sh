#!/usr/bin/env bash
set -euo pipefail

validator_dir="tools/contract-validation"

for path in \
  contracts/openapi \
  contracts/jsonschema \
  contracts/examples \
  "$validator_dir"
do
  if [ ! -d "$path" ]; then
    echo "Missing expected contract path: $path"
    exit 1
  fi
done

if [ ! -d "$validator_dir/node_modules" ]; then
  if [ -f "$validator_dir/package-lock.json" ]; then
    npm --prefix "$validator_dir" ci --no-audit --no-fund
  else
    npm --prefix "$validator_dir" install --no-audit --no-fund
  fi
fi

npm --prefix "$validator_dir" run validate

if [ -f "packages/api-client/package.json" ]; then
  make api-client-check
else
  echo "Generated-client drift checks deferred until generated clients exist."
fi
