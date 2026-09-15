#!/bin/sh
set -eu
cd "$(dirname "$0")"
for test_file in judge-*.cjs; do
  node "$test_file"
done
