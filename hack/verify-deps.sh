#!/bin/bash
set -euo pipefail

function print_failure {
  echo "There are unexpected changes to the vendor tree following 'go mod vendor' and 'go mod tidy'. Please"
  echo "run these commands locally and double-check the Git repository for unexpected changes which may"
  echo "need to be committed."
  exit 1
}

if [ "${OPENSHIFT_CI:-false}" = true ]; then
  for module in . tests-extension; do
    (
      cd "$module"
      go mod vendor
      go mod tidy
      test -z "$(git status --porcelain -- go.mod go.sum vendor)" || print_failure
    )
  done
  test -z "$(git status --porcelain)" || print_failure
  echo "verified Go modules"
fi
