#!/bin/bash
set -e

# Commit hashes
ORIGIN_LATEST="be1e34a4749571c2e0ed8c0f62187c86ded27302"
K8S_LATEST="e4a4167c40c9ed543c5a80f2e08cc73696f7a4f6"
GINKGO_LATEST="696928a6a0d778c77209a4cdfa5771cccf28910d"

# Short hashes
ORIGIN_SHORT="${ORIGIN_LATEST:0:12}"
K8S_SHORT="${K8S_LATEST:0:12}"
GINKGO_SHORT="${GINKGO_LATEST:0:12}"

echo "Fetching commit timestamps (parallel shallow clones)..."

# Create temp directories
TEMP_ORIGIN=$(mktemp -d)
TEMP_K8S=$(mktemp -d)
TEMP_GINKGO=$(mktemp -d)

# Clone all repos in parallel
(git clone --depth=1 https://github.com/openshift/origin.git "$TEMP_ORIGIN" >/dev/null 2>&1) &
PID_ORIGIN=$!

(git clone --depth=1 https://github.com/openshift/kubernetes.git "$TEMP_K8S" >/dev/null 2>&1) &
PID_K8S=$!

(git clone --depth=1 --branch=v2.27.2-openshift-4.22 https://github.com/openshift/onsi-ginkgo.git "$TEMP_GINKGO" >/dev/null 2>&1) &
PID_GINKGO=$!

# Wait for all clones
wait $PID_ORIGIN $PID_K8S $PID_GINKGO

# Extract timestamps
ORIGIN_TIMESTAMP=$(cd "$TEMP_ORIGIN" && git show -s --format=%ct HEAD)
K8S_TIMESTAMP=$(cd "$TEMP_K8S" && git show -s --format=%ct HEAD)
GINKGO_TIMESTAMP=$(cd "$TEMP_GINKGO" && git show -s --format=%ct HEAD)

# Cleanup
rm -rf "$TEMP_ORIGIN" "$TEMP_K8S" "$TEMP_GINKGO"

# Generate pseudo-version dates
ORIGIN_DATE=$(date -u -d @${ORIGIN_TIMESTAMP} +%Y%m%d%H%M%S)
K8S_DATE=$(date -u -d @${K8S_TIMESTAMP} +%Y%m%d%H%M%S)
GINKGO_DATE=$(date -u -d @${GINKGO_TIMESTAMP} +%Y%m%d%H%M%S)

# Generate version strings
ORIGIN_VERSION="v0.0.0-${ORIGIN_DATE}-${ORIGIN_SHORT}"
K8S_VERSION="v1.30.1-0.${K8S_DATE}-${K8S_SHORT}"
GINKGO_VERSION="v2.6.1-0.${GINKGO_DATE}-${GINKGO_SHORT}"

echo "ORIGIN_VERSION=$ORIGIN_VERSION"
echo "K8S_VERSION=$K8S_VERSION"
echo "GINKGO_VERSION=$GINKGO_VERSION"
echo "ORIGIN_SHORT=$ORIGIN_SHORT"
echo "K8S_SHORT=$K8S_SHORT"
echo "GINKGO_SHORT=$GINKGO_SHORT"
echo "K8S_DATE=$K8S_DATE"
