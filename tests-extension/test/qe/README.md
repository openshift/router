# router OTE Test Extension

All 44 Route test cases have been migrated from openshift-tests-private to the OTE (openshift-tests-extension) framework.

## Test Files

| File | Tests | Description |
|------|-------|-------------|
| `route.go` | 44 | Router/Route lifecycle tests (route types, timeouts, cookies, annotations, whitelisting, allowlisting) |

## Test Suites

### router/all
All 44 tests (use `--max-concurrency=1` for safety since some are disruptive)

### router/non-disruptive
41 non-disruptive tests (safe to run in parallel)

### router/disruptive
3 disruptive tests (may affect cluster state, all serial):
- 53696 - Route status should updates accordingly when ingress routes cleaned up
- 55895 - Ingress should be in degraded status when canary route is not available
- 56240 - Canary daemonset can schedule pods to both worker and infra nodes

### router/conformance/parallel
Level0 non-serial, non-disruptive tests

### router/conformance/serial
Level0 serial, non-disruptive tests

## How to Run

```bash
# Run these commands from the repository root
cd tests-extension

export KUBECONFIG=/path/to/kubeconfig

# Build the binary
make build

# List all available suites
./bin/router-tests-ext list suites

# List all tests
./bin/router-tests-ext list tests
```

### Run All Tests

```bash
# Run all 44 tests (serially recommended)
./bin/router-tests-ext run-suite router/all --max-concurrency=1
```

### Run by Suite

```bash
# Run non-disruptive tests (parallel-safe, default concurrency is fine)
./bin/router-tests-ext run-suite router/non-disruptive

# Run only disruptive tests (serially)
./bin/router-tests-ext run-suite router/disruptive --max-concurrency=1
```

### Run a Single Test

```bash
# Run a single test by its full name
./bin/router-tests-ext run-test "[OTP][sig-network-edge] Network_Edge Component_Router Author:mjoseph-Critical-10024-Route could NOT be updated after created"
```

**Important**: 3 of 44 tests are `[Disruptive]` and **must** run with `--max-concurrency=1`. Disruptive tests modify shared cluster resources (ingress controllers, canary routes) and cannot run concurrently. The 2 `[Serial]` tests (85274, 88075) also require serial execution.

## Test Execution Time

- Single test: 2-10 minutes
- Non-disruptive suite: 15-30 minutes
- Disruptive suite: 15-30 minutes
- All tests: 45-90 minutes

## Files

```text
tests-extension/
├── cmd/main.go                         # OTE entry point, suite definitions
├── test/
│   ├── util.go                         # Shared helper functions (package util)
│   └── qe/
│       ├── route.go                    # 44 Route test implementations
│       ├── bindata.go                  # Embedded test fixtures (auto-generated)
│       ├── fixtures.go                 # TestdataDir/FixturePath helpers
│       ├── bindata.mk                  # Bindata generation makefile
│       ├── README.md                   # This file
│       └── testdata/
│           ├── 49802-route.yaml        # Edge+HTTP route template (test 49802)
│           ├── ca-bundle.pem           # CA certificate for reencrypt routes
│           ├── httpbin-deploy.yaml     # Httpbin deployment for timeout tests
│           ├── ingress-with-class.yaml # Ingress with class template (test 88075)
│           ├── ingresscontroller-np.yaml  # Custom ingresscontroller template
│           ├── template-web-server-deploy.yaml  # Templated web server (test 12091)
│           ├── test-client-pod.yaml    # Client pod for internal curl tests
│           ├── web-server-deploy.yaml  # Standard web server deployment
│           ├── web-server-signed-deploy.yaml  # Web server with TLS certificates
│           ├── websocket-deploy.yaml   # WebSocket server (test 17145)
│           └── subdomain-routes/
│               └── route.yaml          # Subdomain route template (test 10024)
├── Makefile                            # Build targets
├── go.mod                              # Dependencies
├── go.sum                              # Dependency checksums
└── vendor/                             # Vendored dependencies
```

## All Test Names

```text
[OTP][sig-network-edge] Network_Edge Component_Router Author:mjoseph-Critical-10024-Route could NOT be updated after created
[OTP][sig-network-edge] Network_Edge Component_Router Author:shudili-ROSA-OSD_CCS-ARO-Critical-10043-Set balance leastconn for passthrough routes
[OTP][sig-network-edge] Network_Edge Component_Router Author:shudili-ROSA-OSD_CCS-ARO-Medium-10207-NetworkEdge Should use the same cookies for secure and insecure access when insecureEdgeTerminationPolicy set to allow for edge/reencrypt route
[OTP][sig-network-edge] Network_Edge Component_Router Author:mjoseph-ROSA-OSD_CCS-ARO-Critical-10660-Service endpoint can be work well if the mapping pod ip is updated
[OTP][sig-network-edge] Network_Edge Component_Router Author:iamin-ROSA-OSD_CCS-ARO-Low-10943-NetworkEdge Set invalid timeout server for route
[OTP][sig-network-edge] Network_Edge Component_Router Author:iamin-ROSA-OSD_CCS-ARO-NonHyperShiftHOST-Critical-11036-NetworkEdge Set insecureEdgeTerminationPolicy to Redirect for passthrough/edge/reencrypt route
[OTP][sig-network-edge] Network_Edge Component_Router Author:iamin-ROSA-OSD_CCS-ARO-Medium-11067-NetworkEdge oc help information should contain option wildcard-policy
[OTP][sig-network-edge] Network_Edge Component_Router Author:shudili-ROSA-OSD_CCS-ARO-Critical-11130-NetworkEdge Enable/Disable haproxy cookies based sticky session for edge termination routes
[OTP][sig-network-edge] Network_Edge Component_Router Author:mjoseph-ROSA-OSD_CCS-ARO-Critical-11619-Limit the number of TCP connection per IP in specified time period
[OTP][sig-network-edge] Network_Edge Component_Router Author:iamin-ROSA-OSD_CCS-ARO-Critical-11635-NetworkEdge Set timeout server for passthough route
[OTP][sig-network-edge] Network_Edge Component_Router Author:shudili-ROSA-OSD_CCS-ARO-Medium-11728-haproxy hash based sticky session for tcp mode passthrough routes
[OTP][sig-network-edge] Network_Edge Component_Router Author:iamin-ROSA-OSD_CCS-ARO-High-11982-NetworkEdge Set timeout server for http route
[OTP][sig-network-edge] Network_Edge Component_Router Author:shudili-ROSA-OSD_CCS-ARO-Critical-12091-haproxy config information should be clean when changing the service to another route
[OTP][sig-network-edge] Network_Edge Component_Router Author:mjoseph-High-12506-reencrypt route with no cert if a router is configured with a default wildcard cert
[OTP][sig-network-edge] Network_Edge Component_Router Author:mjoseph-ROSA-OSD_CCS-ARO-Critical-12562-The path specified in route can work well for edge/unsecure termination
[OTP][sig-network-edge] Network_Edge Component_Router Author:mjoseph-Critical-12564-The path specified in route can work well for reencrypt terminated
[OTP][sig-network-edge] Network_Edge Component_Router Author:mjoseph-ROSA-OSD_CCS-ARO-Critical-12652-The later route should be HostAlreadyClaimed when there is a same host exist
[OTP][sig-network-edge] Network_Edge Component_Router Author:iamin-ROSA-OSD_CCS-ARO-NonHyperShiftHOST-Critical-13753-NetworkEdge Check the cookie if using secure mode when insecureEdgeTerminationPolicy to Redirect for edge/reencrypt route
[OTP][sig-network-edge] Network_Edge Component_Router Author:iamin-ROSA-OSD_CCS-ARO-NonHyperShiftHOST-Critical-13839-NetworkEdge Set insecureEdgeTerminationPolicy to Allow for reencrypt/edge route
[OTP][sig-network-edge] Network_Edge Component_Router Author:iamin-ROSA-OSD_CCS-ARO-NonHyperShiftHOST-Critical-14678-NetworkEdge Only the host in whitelist could access unsecure/edge/reencrypt/passthrough routes
[OTP][sig-network-edge] Network_Edge Component_Router Author:iamin-ROSA-OSD_CCS-ARO-Low-14680-NetworkEdge Add invalid value in annotation whitelist to route
[OTP][sig-network-edge] Network_Edge Component_Router Author:mjoseph-ROSA-OSD_CCS-ARO-High-15028-router can do a case-insensitive match of a hostname for unsecure/edge/passthrough/reencrypt route
[OTP][sig-network-edge] Network_Edge Component_Router Author:shudili-ROSA-OSD_CCS-ARO-Critical-15873-NetworkEdge can set cookie name for edge/reen routes by annotation
[OTP][sig-network-edge] Network_Edge Component_Router Author:iamin-ROSA-OSD_CCS-ARO-Medium-16732-NetworkEdge Check haproxy.config when overwriting 'timeout server' which was already specified
[OTP][sig-network-edge] Network_Edge Component_Router Author:mjoseph-NonHyperShiftHOST-ROSA-OSD_CCS-ARO-Critical-17145-haproxy router support websocket via unsecure route
[OTP][sig-network-edge] Network_Edge Component_Router Author:iamin-ROSA-OSD_CCS-ARO-Critical-18482-NetworkEdge limits backend pod max concurrent connections for unsecure, edge, reen, passthrough route
[OTP][sig-network-edge] Network_Edge Component_Router Author:iamin-ROSA-OSD_CCS-ARO-Medium-18490-NetworkEdge limits multiple backend pods max concurrent connections
[OTP][sig-network-edge] Network_Edge Component_Router Author:mjoseph-ROSA-OSD_CCS-ARO-Medium-19804-Unsecure route with path and another tls route with same hostname can work at the same time
[OTP][sig-network-edge] Network_Edge Component_Router Author:iamin-ROSA-OSD_CCS-ARO-High-34106-NetworkEdge Routes annotated with 'haproxy.router.openshift.io/rewrite-target=/path' will replace and rewrite http request with specified '/path'
[OTP][sig-network-edge] Network_Edge Component_Router Author:iamin-ROSA-OSD_CCS-ARO-Critical-38671-NetworkEdge 'haproxy.router.openshift.io/timeout-tunnel' annotation gets applied alongside 'haproxy.router.openshift.io/timeout' for clear/edge/reencrypt routes
[OTP][sig-network-edge] Network_Edge Component_Router Author:iamin-ROSA-OSD_CCS-ARO-High-38672-NetworkEdge 'haproxy.router.openshift.io/timeout-tunnel' annotation takes precedence over 'haproxy.router.openshift.io/timeout' values for passthrough routes
[OTP][sig-network-edge] Network_Edge Component_Router Author:aiyengar-ROSA-OSD_CCS-ARO-Medium-42230-route can be configured to whitelist more than 61 ips/CIDRs
[OTP][sig-network-edge] Network_Edge Component_Router Author:mjoseph-ROSA-OSD_CCS-ARO-High-45399-ingress controller continue to function normally with unexpected high timeout value
[OTP][sig-network-edge] Network_Edge Component_Router Author:hongli-ROSA-OSD_CCS-ARO-High-45741-ingress canary route redirects http to https
[OTP][sig-network-edge] Network_Edge Component_Router Author:mjoseph-ROSA-OSD_CCS-ARO-High-49802-HTTPS redirect happens even if there is a more specific http-only
[OTP][sig-network-edge] Network_Edge Component_Router Author:mjoseph-Critical-53696-Route status should updates accordingly when ingress routes cleaned up [Disruptive]
[OTP][sig-network-edge] Network_Edge Component_Router Author:mjoseph-NonHyperShiftHOST-High-55895-Ingress should be in degraded status when canary route is not available [Disruptive]
[OTP][sig-network-edge] Network_Edge Component_Router Author:mjoseph-NonHyperShiftHOST-NonPreRelease-High-56240-Canary daemonset can schedule pods to both worker and infra nodes [Disruptive]
[OTP][sig-network-edge] Network_Edge Component_Router Author:mjoseph-ROSA-OSD_CCS-ARO-Medium-63004-Ipv6 addresses are also acceptable for whitelisting
[OTP][sig-network-edge] Network_Edge Component_Router Author:iamin-ROSA-OSD_CCS-ARO-Critical-77080-NetworkEdge Only host in allowlist can access unsecure/edge/reencrypt/passthrough routes
[OTP][sig-network-edge] Network_Edge Component_Router Author:iamin-ROSA-OSD_CCS-ARO-Critical-77082-NetworkEdge Route gives allowlist precedence when whitelist and allowlist annotations are both present
[OTP][sig-network-edge] Network_Edge Component_Router Author:iamin-ROSA-OSD_CCS-ARO-High-77091-NetworkEdge Route does not enable allowlist with than 61 CIDRs and if invalid IP annotation is given
[OTP][sig-network-edge] Network_Edge Component_Router Author:shudili-ROSA-OSD_CCS-ARO-Critical-85274-Route spec path that have specail characters should not cause HaProxy error and ingress degraded [Serial]
[OTP][sig-network-edge] Network_Edge Component_Router Author:mjoseph-ROSA-OSD_CCS-ARO-High-88075-UnmanagedRoutes metric should filter ingress by both name and namespace [Serial]
```
