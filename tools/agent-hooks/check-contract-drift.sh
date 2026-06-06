#!/usr/bin/env bash
set -euo pipefail

missing=0
for path in \
  contracts/openapi \
  contracts/jsonschema \
  packages/api-client \
  packages/forms-dsl
do
  if [ ! -d "$path" ]; then
    echo "Missing expected contract path: $path"
    missing=1
  fi
done

exit "$missing"
