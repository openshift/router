#!/bin/bash
set -euo pipefail

IMAGE="quay.io/btofel/router:patched-bt"
NAMESPACE="router-scale-test"
NUM_ROUTES=1000
MAX_STALL=60 # Seconds to wait without progress before declaring a stall

while [[ $# -gt 0 ]]; do
  case $1 in
    --single)
      NUM_ROUTES=1
      shift
      ;;
    *)
      IMAGE="$1"
      shift
      ;;
  esac
done

echo "========================================================="
echo "  Router Scale Test"
echo "  Image:  $IMAGE"
echo "  Routes: $NUM_ROUTES"
echo "========================================================="

echo -e "\n=== 1. Scaling down Operators (CVO & Ingress) ==="
# Stop CVO from overwriting the ingress operator
oc scale --replicas 0 -n openshift-cluster-version deployments/cluster-version-operator 2>/dev/null || true
# Stop Ingress Operator from overwriting our custom router image
oc scale --replicas 0 -n openshift-ingress-operator deployments ingress-operator 2>/dev/null || true

echo -e "\n=== 2. Patching router deployment ==="
# Inject the custom image into the router deployment
oc -n openshift-ingress set image deployment router-default router="$IMAGE"
# Wait for the rollout to complete
echo "Waiting for router pod to roll out with the new image..."
oc -n openshift-ingress rollout status deployment router-default --timeout=5m
echo -e "\n=== 3. Setting up test namespace: $NAMESPACE ==="
if oc get project "$NAMESPACE" &>/dev/null; then
  echo "Deleting old project..."
  oc delete project "$NAMESPACE"
  # Wait for project to fully terminate
  while oc get project "$NAMESPACE" &>/dev/null; do sleep 2; done
fi
oc new-project "$NAMESPACE" >/dev/null

echo -e "\n=== 4. Generating $NUM_ROUTES secrets and routes ==="
# Generate a valid dummy certificate so the ExtendedValidator doesn't reject it
openssl req -x509 -newkey rsa:2048 -keyout /tmp/tls.key -out /tmp/tls.crt -days 365 -nodes -subj "/CN=test-scale.com" 2>/dev/null
CRT_B64=$(cat /tmp/tls.crt | base64 | tr -d '\n')
KEY_B64=$(cat /tmp/tls.key | base64 | tr -d '\n')

# Create a dummy service to satisfy the route 'To' target
oc create service clusterip dummy-svc --tcp=8080:8080 -n "$NAMESPACE" >/dev/null

YAML_FILE="/tmp/scale-test-resources.yaml"
> "$YAML_FILE"

# Grant the router service account permission to read secrets in this namespace
cat <<EOF >> "$YAML_FILE"
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: router-secret-reader
  namespace: $NAMESPACE
rules:
- apiGroups: [""]
  resources: ["secrets"]
  verbs: ["get", "list", "watch"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: router-secret-reader-binding
  namespace: $NAMESPACE
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: Role
  name: router-secret-reader
subjects:
- kind: ServiceAccount
  name: router
  namespace: openshift-ingress
---
EOF

for i in $(seq 1 $NUM_ROUTES); do
  cat <<EOF >> "$YAML_FILE"
apiVersion: v1
kind: Secret
metadata:
  name: test-secret-$i
  namespace: $NAMESPACE
type: kubernetes.io/tls
data:
  tls.crt: $CRT_B64
  tls.key: $KEY_B64
---
apiVersion: route.openshift.io/v1
kind: Route
metadata:
  name: test-route-$i
  namespace: $NAMESPACE
spec:
  host: test-route-$i.apps.example.com
  tls:
    termination: edge
    externalCertificate:
      name: test-secret-$i
  to:
    kind: Service
    name: dummy-svc
---
EOF
done

echo -e "\n=== 5. Applying resources & Measuring Performance ==="
# We start the clock right before applying
START_TIME=$(date +%s)
oc apply -f "$YAML_FILE" >/dev/null
echo "All objects applied. Starting monitor..."

LAST_COUNT=0
STALL_TIMER=0

while true; do
  # Extract the Admitted status from all routes in the namespace. 
  # We use jsonpath with single quotes to avoid shell expansion of () or ?.
  # We count occurrences of "True" or "False".
  ALL_STATUSES=$(oc get routes -n "$NAMESPACE" -o jsonpath='{range .items[*]}{.status.ingress[*].conditions[?(@.type=="Admitted")].status}{"\n"}{end}' 2>/dev/null || true)
  ADMITTED=$(echo "$ALL_STATUSES" | grep -c "True" || true)
  REJECTED=$(echo "$ALL_STATUSES" | grep -c "False" || true)
  CURRENT_TIME=$(date +%s)
  ELAPSED=$((CURRENT_TIME - START_TIME))

  echo "[${ELAPSED}s] Routes Admitted: $ADMITTED / $NUM_ROUTES (Rejected: $REJECTED)"

  if [ "$ADMITTED" -eq "$NUM_ROUTES" ]; then
    echo -e "\n🎉 SUCCESS: All $NUM_ROUTES routes successfully admitted in $ELAPSED seconds!"
    break
  fi
  
  if [[ "$REJECTED" -gt 0 && "$((ADMITTED + REJECTED))" -eq "$NUM_ROUTES" ]]; then
    echo -e "\n⚠️ WARNING: Processing finished but $REJECTED routes were rejected. Total time: $ELAPSED seconds."
    break
  fi

  # Check for stall
  if [ "$ADMITTED" -eq "$LAST_COUNT" ]; then
    STALL_TIMER=$((STALL_TIMER + 5))
    if [ "$STALL_TIMER" -ge "$MAX_STALL" ]; then
       echo -e "\n❌ FAILURE: Route admission stalled at $ADMITTED routes for $MAX_STALL seconds."
       echo "Total time elapsed before failure declared: $ELAPSED seconds."
       break
    fi
  else
    STALL_TIMER=0
    LAST_COUNT=$ADMITTED
  fi

  sleep 5
done
