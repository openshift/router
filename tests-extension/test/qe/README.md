# Router OTE Test Extension

Route, HAProxy, cookie, route-logging, route-weight, route-headers, route-HSTS, dynamic-config-manager, and route-admission test cases migrated from `openshift-tests-private` to the OTE
(openshift-tests-extension) framework, under the NE-2126 migration epic.

| File | Tests | Description |
|------|-------|-------------|
| `route.go` | 42 | Router/Route lifecycle tests (route types, timeouts, cookies, annotations, whitelisting, allowlisting) |
| `haproxy.go` | 29 | HAProxy health checks, protocol settings, balancing, error pages, and ingress-controller behavior |
| `cookies.go` | 5 | Route cookie affinity, cookie naming, and SameSite annotations; excludes low-priority OCP-35549 |
| `logging.go` | 3 | Router access logging for cookies, headers, and UniqueID; excludes low-priority OCP-34178 and OCP-34189 |
| `route-weight.go` | 6 | Route alternate-backend weights and endpoint weight distribution; excludes low-priority OCP-11351 |
| `headers.go` | 14 | Forwarded-header policies and route/ingress-controller HTTP header actions; excludes OCP-34157 |
| `route-hsts.go` | 7 | Route HSTS annotations and global HSTS policies; excludes OCP-15977, OCP-43478, and OCP-43966 |
| `dynamic_config_manager.go` | 8 | Dynamic Configuration Manager behavior across route types and endpoint scaling |
| `route-admission.go` | 5 | Route-admission namespace ownership and wildcard policies |

## Test naming conventions

Route tests use the `Component_Route` prefix:

```text
[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Route <description> [<execution tags>]
```

HAProxy tests use the `Component_Haproxy` prefix:

```text
[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Haproxy <description> [<execution tags>]
```

Cookie tests use the `Component_Route-Cookies` prefix:

```text
[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Route-Cookies <description> [<execution tags>]
```

Route-logging tests use the `Component_Route-Logging` prefix:

```text
[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Route-Logging <description> [<execution tags>]
```

Route-weight tests use the `Component_Route-Weight` prefix:

```text
[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Route-Weight <description> [<execution tags>]
```

Route-headers tests use the `Component_Route-Headers` prefix:

```text
[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Route-Headers <description> [<execution tags>]
```

Route-HSTS tests use the `Component_Route-HSTS` prefix:

```text
[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Route-HSTS <description> [<execution tags>]
```

Dynamic Configuration Manager tests use the `Component_Route-DCM` prefix:

```text
[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Route-DCM <description> [<execution tags>]
```

Route-admission tests use the `Component_Route-Admission` prefix:

```text
[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Route-Admission <description> [<execution tags>]
```

All prefixes carry `[sig-network-edge]` and `[Feature:Router]`, which is what
ci-test-mapping and the origin suite qualifiers key on, so the split does not
change ownership or suite membership.

| Token | Meaning |
|-------|---------|
| `[OTP]` | Ported from openshift-tests-private; used to track migration volume. |
| `[sig-network-edge]` | Broad-area grouping required for component mapping. |
| `[Feature:Router]` | Matches the existing router ownership rule in ci-test-mapping. |
| `[Level0]` | Tracking tag that selects the test into the `router/conformance/*` suites. |
| `OCP-<case ID>` label | Canonical case identity stored with `g.Label`, independent of the title. |
| `[Serial]` / `[Disruptive]` | Scheduling isolation; see [Test suites](#test-suites). |

Case IDs are Ginkgo labels, and environment selectors use those labels. Priority
prefixes are omitted. The prefix and execution-tag spacing match the DNS extension.
The metadata records the current titles, including `[Feature:Router]`, without
legacy-name aliases. The canonical OCP labels remain unchanged.

`[Level0]` should reflect the QE case classification. The current OTP source has
no explicit Level0 markers for these 119 migrated cases, so the existing tags are
retained pending confirmation against the QE-approved Level0 plan or Polarion
export. Priority and suite membership alone do not establish that classification.


Authors are recorded as a source comment immediately above each `g.It`, not in
the test name:

```go
//Author:shudili
g.It("Set balance leastconn for passthrough routes", g.Label("OCP-10043"), func() {
```

### Ownership

OTE requires each test to have an owning component, declared either as a
`[Jira:Component]` tag in the test name or as an entry in
[ci-test-mapping](https://github.com/openshift-eng/ci-test-mapping).  These tests
use the ci-test-mapping route, so **confirming ownership coverage is required
as part of migration**. Informing status does not waive this requirement.
All 119 names contain both `[sig-network-edge]` and `[Feature:Router]`, matching
the existing router SIG rule in `pkg/components/networking/router/component.go`.
This applies when the payload runner reports JUnit suite `openshift-tests`;
it does not depend on the private runner's `Network_Edge` suite matcher.

This initial registration baseline does not include `originalName` fields or
a companion `TestRenames` mapping. Component Readiness can assign the current
names to the router component, but historical results under earlier names
are not joined automatically. The three disruptive cases continue to carry
only `[Disruptive]` in their current names.

### Renaming or removing a test

`make update-metadata` compares the binary against the committed
`.openshift-tests-extension/openshift_payload_router.json` and **refuses to
regenerate it** when a test has disappeared from the binary without being
declared:

```text
Missing Tests:
  * [sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Route <previous description>
Error: missing tests, if you've renamed tests you must add their names to
       OriginalName, or mark them obsolete
```

A rename counts as a disappearance, so declare it *before* regenerating. For
the first future rename, record the previously registered name as `OriginalName`
on the spec in [`cmd/main.go`](../../cmd/main.go). For later renames, retain
that first recorded name without overwriting an existing `OriginalName`:

```go
componentSpecs.Walk(func(spec *et.ExtensionTestSpec) {
	if spec.Name == "<current name>" && spec.OriginalName == "" {
		spec.OriginalName = "<first recorded name>"
	}
})
```

For a test that is being dropped rather than renamed, list its full previous
name instead:

```go
ext.IgnoreObsoleteTests("<previous name>")
```

If the removed test is listed in `hyperShiftUnsupportedTests` in `cmd/main.go`,
also remove its case ID from that list before regenerating metadata. Otherwise,
`applyEnvironmentSelectors` panics at startup because the list no longer matches
the registered tests. Keep that consistency guard in place.
Also update any case-specific selectors, such as the `SingleReplica` exclusion
for case 56240, when removing their test.

From `tests-extension/`, regenerate the metadata:

```bash
make update-metadata
```

Commit the test changes and regenerated metadata together before verifying.
`make verify` rejects any uncommitted metadata change, including staged changes:

```bash
make verify
```

This bookkeeping is required from the moment the metadata file exists, and is
independent of whether the test has any CI history. For tests already tracked by
Sippy or Component Readiness, coordinate renames with a matching
[ci-test-mapping](https://github.com/openshift-eng/ci-test-mapping) update.

## Lifecycle

All tests are registered as **informing** (`et.LifecycleInforming`, set in
`cmd/main.go`).  Informing keeps routine failures non-fatal while pass rates are
ratcheted up to the 99% bar that `openshift/conformance/*` requires.  A test
should only be promoted to blocking once its stable ID has been tracked in
Sippy/Component Readiness and the component team agrees it is ready.

## Environment selectors

Constraints are expressed as CEL environment selectors rather than name tokens,
so that an unsupported environment produces a clean skip instead of a setup
failure.  See `applyEnvironmentSelectors` in `cmd/main.go`.

- All tests require the `config.openshift.io` API group, which excludes
  MicroShift without excluding bare metal or other supported platforms.
- All 119 tests are temporarily excluded on disconnected clusters
  (`externalConnectivity == "Disconnected"`), including IPv4, IPv6-only, and
  dual-stack, because fixture setup still requires the source registry.
  Connected and proxied clusters remain eligible on every network stack,
  provided the source registry is reachable. See
  [Temporary disconnected exclusion](#temporary-disconnected-exclusion).
- Cases 11036, 13753, 13839, 14678, 17145, 55895, 56240, 43474, 43475,
  43476, 43479, 43480, 43884, 30190, and 30191 are excluded when the control
  plane topology is `External` (HyperShift).
- Case 56240 requires worker and infra Machines and is also excluded on
  `SingleReplica` (single-node) clusters.

`list tests` without environment flags lists all registered tests. To inspect
environment selection, supply the supported API group together with the other
environment flags. For example, this lists the 104 tests selected for HyperShift:

```bash
./bin/router-tests-ext list tests --api-group config.openshift.io --topology External -o names
```

The standalone `run-test` and `run-suite` commands do not apply environment
selectors; they run the names or suite requested. The payload `openshift-tests`
runner supplies cluster facts during discovery. When selecting tests locally for
a particular environment, pipe the environment-filtered names into `run-test`.

The standard `e2e-metal-ipi-ovn-ipv4` profile is not marked disconnected. The
[baremetalds runner](https://github.com/openshift/release/blob/main/ci-operator/step-registry/baremetalds/e2e/test/baremetalds-e2e-test-commands.sh#L154)
sets its provider to `{"type":"baremetal"}`, while the IPv6 profile adds
`"disconnected":true`. A local image mirror alone does not imply disconnected
connectivity. Connected and proxied jobs remain eligible, including IPv6-only;
the exclusion applies only when the runner advertises
`externalConnectivity == "Disconnected"`.

For IPv6-only validation, the release repo has two distinct examples:

- **Connected through a proxy:** the 5.1
  [`baremetal-ha-agent-ipv6-dynamic-f7` job](https://github.com/openshift/release/blob/main/ci-operator/config/openshift-eng/agent-qe-infra/openshift-eng-agent-qe-infra-release-5.1__amd64-nightly.yaml)
  uses the
  [`baremetal-lab-agent-install-network-dynamic-ipv6` workflow](https://github.com/openshift/release/blob/main/ci-operator/step-registry/baremetal/lab/agent/install/network/dynamic/ipv6/baremetal-lab-agent-install-network-dynamic-ipv6-workflow.yaml).
  It sets `ipv4_enabled: "false"`, `ipv6_enabled: "true"`,
  `DISCONNECTED: "false"`, and `CLUSTER_WIDE_PROXY: "true"`. The job provisions
  three masters and two workers, then runs cluster health checks. It does not
  currently execute the router extension; validating these tests requires a
  router test step or access to the provisioned cluster.
- **Disconnected:** the 5.1 `e2e-metal-ipi-ovn-ipv6` payload job uses
  [`baremetalds-e2e-ovn-ipv6`](https://github.com/openshift/release/blob/main/ci-operator/step-registry/baremetalds/e2e/ovn/ipv6/baremetalds-e2e-ovn-ipv6-workflow.yaml),
  which sets `IP_STACK=v6`. The baremetalds runner above marks this environment
  disconnected, so the router tests are excluded during discovery.

These configurations establish environment selection, not passing router-test
results. Connected IPv6 still needs live validation of source-registry access
and the route assertions.

## Fixture images

The manifests under `testdata/` pin their images by digest in
`quay.io/openshifttest/`. Case 17145 also replaces the client image with a pinned
`hello-sdn` image at runtime:

| Image | Used by |
|-------|---------|
| `nginx-alpine` | `web-server-deploy.yaml`, `web-server-signed-deploy.yaml`, `template-web-server-deploy.yaml`, `test-client-pod.yaml` |
| `httpbin` | `httpbin-deploy.yaml` (two digests) |
| `hello-websocket` | `websocket-deploy.yaml` |
| `hello-sdn` | `test-client-pod.yaml`, rewritten by case 17145 |

Those references are the source of truth, but **no pod ever pulls them
directly**.  `util.MirrorFixtureImages` rewrites every manifest before it is
applied: each distinct image is imported into an ImageStream in the test's own
namespace (tagged with a prefix of its digest, so two digests of the same
repository do not collide) and the manifest is rewritten to pull from
`image-registry.openshift-image-registry.svc:5000/<namespace>/<image>:<tag>`.
The import uses `--import-mode=PreserveOriginal` so a multi-architecture
manifest list stays intact, and is retried for up to 3 minutes.

This imports image metadata and configures registry pull-through; it does not
preload every image layer into the integrated registry. The importer must still
reach the source registry, and the integrated registry must be able to fetch
uncached image layers.

This matters for two independent CI failure modes:

- The **known-image-checker** monitor fails any job whose pods reference an
  image outside the mirrored registry.
- On a cluster with no route to quay.io the pod simply never becomes ready, so
  the test times out mid-assertion and reports a misleading failure. This is
  what happened to the ingress operator tests on disconnected IPv6 metal in
  OCPBUGS-132070.

A mirrored manifest pulls from the integrated registry, so that registry has to
be serving.  It is an optional capability and its operator is set to `Removed`
wherever the installer finds no supported object storage, and the capability
can be present while the operator is `Removed` -- which an environment selector
cannot express. `util.MirrorFixtureImages` therefore skips only when the
operator is explicitly `Removed`, or when a missing registry configuration is
accompanied by ClusterVersion status confirming the `ImageRegistry` capability
is disabled. A missing configuration without that confirmation fails the test.
Registry lookups use structured Kubernetes API errors: permission,
authentication, and unexpected errors fail instead of becoming skips.
Transient API or transport errors are retried within a 30-second deadline;
exhausting that deadline fails with the last lookup error.

Adding a fixture image means adding it to `quay.io/openshifttest/` and pinning
it by digest; `fixtureImageRe` in `test/util.go` matches that registry path.

### Temporary disconnected exclusion

The metal IPv6 Prow run fails during the shared fixture setup, before any route
assertions. For example, case 38672 repeatedly fails to import `nginx-alpine`
with `Get "https://quay.io/v2/": context deadline exceeded`, leaving the
ImageStream with `Unique Images: 0`. That job is marked disconnected. Every
spec is temporarily excluded during environment-filtered discovery on
disconnected clusters to avoid repeating this setup failure across the
extension, regardless of the network stack. This is a temporary test-image
dependency restriction; it does not indicate a router IPv6 defect. There is no
blanket IPv6 exclusion: connected and proxied IPv6-only clusters remain eligible.

The exclusion lives in `applyEnvironmentSelectors` in `cmd/main.go`. Test names
and unfiltered suite membership are unchanged. To verify the current selection:

```bash
# No router tests should be selected on disconnected clusters of any network stack.
./bin/router-tests-ext list tests --api-group config.openshift.io --network-stack ipv4 --external-connectivity Disconnected
./bin/router-tests-ext list tests --api-group config.openshift.io --network-stack ipv6 --external-connectivity Disconnected
./bin/router-tests-ext list tests --api-group config.openshift.io --network-stack dual --external-connectivity Disconnected

# All 119 remain eligible on connected or proxied clusters, before other filters.
./bin/router-tests-ext list tests --api-group config.openshift.io --network-stack ipv4 --external-connectivity Direct -o names
./bin/router-tests-ext list tests --api-group config.openshift.io --network-stack ipv6 --external-connectivity Direct -o names
./bin/router-tests-ext list tests --api-group config.openshift.io --network-stack ipv6 --external-connectivity Proxied -o names
./bin/router-tests-ext list tests --api-group config.openshift.io --network-stack dual --external-connectivity Direct -o names
```

Direct `run-test` and `run-suite` invocations bypass environment filtering, as
described above, and can still encounter the image import failure when the
source registry is unreachable.

Re-enable disconnected coverage in a separate follow-up PR:

1. Register all five pinned fixture image references through OTE so
   `router-tests-ext images` reports nginx-alpine, both httpbin versions,
   hello-websocket, and the hello-sdn image used by case 17145. It currently
   reports `null`.
2. Coordinate publication of the images to the approved OpenShift test-image
   mirror with its owners, verify the required architectures, and confirm that
   the metal IPv6 and disconnected jobs mirror the router extension's images
   before running tests.
3. Resolve fixture pull specs using the same image mapping as the CI mirror,
   honoring `KUBE_TEST_REPO` supplied by `openshift-tests --from-repository`.
   Use those mapped references directly in manifests, or import from the mapped
   registry. Remove the runtime dependency on the original `quay.io` registry.
4. Remove `specs.Exclude(et.ExternalConnectivityEquals("Disconnected"))` from
   `cmd/main.go` once disconnected operation is supported. Run
   `make update-metadata` and verify discovery selects the tests again while
   retaining the API-group and HyperShift restrictions.
5. Run cases 11036 and 38672, then the applicable router suites, on the metal
   IPv6 disconnected job and an IPv4 disconnected cluster before restoring
   their coverage.
   Verify fixture imports and backend pods use the configured mirror and
   execute the route assertions. Validate disconnected operation without access
   to `quay.io` or fallback to any external registry.
6. Remove this temporary-exclusion section and update the fixture-image and
   environment-selection documentation to describe the completed support.

## Test suites

| Suite | Parent | Tests |
|-------|--------|-------|
| `router/conformance/parallel` | `openshift/conformance/parallel` | 98 |
| `router/conformance/serial` | `openshift/conformance/serial` | 17 |
| `router/disruptive` | — | 4 |
| `router/non-disruptive` | — | 115 |
| `router/all` | — | 119 |

Conformance selects 98 parallel tests and 17 non-disruptive `[Serial]` tests.
The four disruptive cases carry only `[Disruptive]` and are excluded from both
conformance suites. They remain available through `router/disruptive` and
`router/all`, with isolation taints that prevent overlap with other tests.

Cases 53696 and 55895 change the default ingress controller's route selector;
53696 temporarily excludes platform routes, and 55895 also deletes the canary
route and waits for ingress to become degraded. These actions caused OAuth,
console, and image-registry availability and operator-state monitor failures
in the serial payload run. Cleanup restores service, but monitors retain the
outages. Informing lifecycle and execution isolation do not suppress those
independent monitor failures.

Run disruptive cases only on a disposable cluster or in a dedicated lane that
supports intentional platform outages. Current conformance payload jobs do not
run `router/disruptive`; dedicated automated coverage remains a follow-up.
The metadata uses the current execution tags, and the canonical OCP case
labels stay the same.

The counts above describe registered tests before environment filtering and
runtime skips.

### Disruptive tests

These cases are excluded from conformance suites and require explicit selection.

- 53696 - Route status should updates accordingly when ingress routes cleaned up [Disruptive]
- 55895 - Ingress should be in degraded status when canary route is not available [Disruptive]
- 56240 - Canary daemonset can schedule pods to both worker and infra nodes [Disruptive]

### Serial tests

- 40679 - The endpointPublishingStrategy parameter allows TCP, PROXY, and empty definitions for HostNetwork [Serial]
- 50819 - Routers with hostnetwork endpoint strategy with same http/https/stat port numbers cannot be deployed on the same worker node [Serial]
- 85274 - Route spec path that have specail characters should not cause HaProxy error and ingress degraded [Serial]
- 88075 - UnmanagedRoutes metric should filter ingress by both name and namespace [Serial]

## How to Run

Use Go 1.26 or newer (see `tests-extension/go.mod`), `make`, and an OpenShift
cluster with administrator access. Put `oc`, `curl`, and `bash` on `PATH`.
Some cases also invoke GNU `sed` with `-i`; on macOS, put GNU `sed` on `PATH`
for those cases. Fixture generation is a separate developer step described below.
Tests that use fixture images also require the integrated image registry to be
available and able to import the source images; see [Fixture images](#fixture-images).

```bash
# Start from the repository root. All subsequent commands use this directory.
cd tests-extension

export KUBECONFIG=/path/to/kubeconfig

# Build the binary
make build

# List all available suites
./bin/router-tests-ext list suites -o names

# List all tests, one full name per line
./bin/router-tests-ext list tests -o names
```

The extension build sets `CGO_ENABLED=0` and `GO_COMPLIANCE_POLICY=exempt_all`.
It compiles the committed QE `bindata.go` and vendored dependencies without
running or downloading `go-bindata`. The component Dockerfiles gzip the binary
and include it in the router image. `make clean` removes only the binary.

After changing `testdata/`, run `make update-bindata` explicitly and commit the
generated `test/qe/bindata.go`. `make verify-bindata` checks freshness without
rewriting the committed file. These developer commands use pinned go-bindata
v3.1.3 and need it in the module cache or access to the Go module proxy.
See the repository's [fixture build guidance](../../../HACKING.md#test-extension-fixtures).

Use `--help` on a subcommand to see its flags, for example
`./bin/router-tests-ext run-suite --help`. Listing tests does not execute them.

### Run All Tests

This includes the disruptive cases and can interrupt platform routes. Use a
disposable cluster or a lane that supports intentional outages. For normal
conformance coverage, run the parallel and serial conformance suites instead.

```bash
./bin/router-tests-ext run-suite router/all
```

### Run by Suite

```bash
# All tests except the disruptive ones.  Note this suite still contains the four
# [Serial] tests, which the scheduler isolates; see Scheduling below.
./bin/router-tests-ext run-suite router/non-disruptive

# Explicitly run cases that intentionally disrupt the cluster.
./bin/router-tests-ext run-suite router/disruptive

# Run the conformance suites separately
./bin/router-tests-ext run-suite router/conformance/parallel
./bin/router-tests-ext run-suite router/conformance/serial
```

### Run Only `route.go` Tests

```bash
# Run all 42 route tests by name filter
./bin/router-tests-ext run-test \
  -n "[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Route Route could NOT be updated after created" \
  -n "[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Route Set balance leastconn for passthrough routes" \
  -n "[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Route NetworkEdge Should use the same cookies for secure and insecure access when insecureEdgeTerminationPolicy set to allow for edge/reencrypt route" \
  -n "[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Route Service endpoint can be work well if the mapping pod ip is updated" \
  -n "[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Route NetworkEdge Set insecureEdgeTerminationPolicy to Redirect for passthrough/edge/reencrypt route" \
  -n "[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Route NetworkEdge oc help information should contain option wildcard-policy" \
  -n "[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Route NetworkEdge Enable/Disable haproxy cookies based sticky session for edge termination routes" \
  -n "[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Route Limit the number of TCP connection per IP in specified time period" \
  -n "[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Route NetworkEdge Set timeout server for passthough route" \
  -n "[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Route haproxy hash based sticky session for tcp mode passthrough routes" \
  -n "[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Route NetworkEdge Set timeout server for http route" \
  -n "[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Route haproxy config information should be clean when changing the service to another route" \
  -n "[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Route reencrypt route with no cert if a router is configured with a default wildcard cert" \
  -n "[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Route The path specified in route can work well for edge/unsecure termination" \
  -n "[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Route The path specified in route can work well for reencrypt terminated" \
  -n "[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Route The later route should be HostAlreadyClaimed when there is a same host exist" \
  -n "[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Route NetworkEdge Check the cookie if using secure mode when insecureEdgeTerminationPolicy to Redirect for edge/reencrypt route" \
  -n "[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Route NetworkEdge Set insecureEdgeTerminationPolicy to Allow for reencrypt/edge route" \
  -n "[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Route NetworkEdge Only the host in whitelist could access unsecure/edge/reencrypt/passthrough routes" \
  -n "[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Route router can do a case-insensitive match of a hostname for unsecure/edge/passthrough/reencrypt route" \
  -n "[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Route NetworkEdge can set cookie name for edge/reen routes by annotation" \
  -n "[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Route NetworkEdge Check haproxy.config when overwriting 'timeout server' which was already specified" \
  -n "[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Route haproxy router support websocket via unsecure route" \
  -n "[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Route NetworkEdge limits backend pod max concurrent connections for unsecure, edge, reen, passthrough route" \
  -n "[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Route NetworkEdge limits multiple backend pods max concurrent connections" \
  -n "[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Route Unsecure route with path and another tls route with same hostname can work at the same time" \
  -n "[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Route NetworkEdge Routes annotated with 'haproxy.router.openshift.io/rewrite-target=/path' will replace and rewrite http request with specified '/path'" \
  -n "[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Route NetworkEdge 'haproxy.router.openshift.io/timeout-tunnel' annotation gets applied alongside 'haproxy.router.openshift.io/timeout' for clear/edge/reencrypt routes" \
  -n "[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Route NetworkEdge 'haproxy.router.openshift.io/timeout-tunnel' annotation takes precedence over 'haproxy.router.openshift.io/timeout' values for passthrough routes" \
  -n "[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Route route can be configured to whitelist more than 61 ips/CIDRs" \
  -n "[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Route ingress controller continue to function normally with unexpected high timeout value" \
  -n "[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Route ingress canary route redirects http to https" \
  -n "[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Route HTTPS redirect happens even if there is a more specific http-only" \
  -n "[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Route Ipv6 addresses are also acceptable for whitelisting" \
  -n "[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Route NetworkEdge Only host in allowlist can access unsecure/edge/reencrypt/passthrough routes" \
  -n "[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Route NetworkEdge Route gives allowlist precedence when whitelist and allowlist annotations are both present" \
  -n "[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Route NetworkEdge Route does not enable allowlist with than 61 CIDRs and if invalid IP annotation is given" \
  -n "[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Route Route status should updates accordingly when ingress routes cleaned up [Disruptive]" \
  -n "[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Route Ingress should be in degraded status when canary route is not available [Disruptive]" \
  -n "[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Route Canary daemonset can schedule pods to both worker and infra nodes [Disruptive]" \
  -n "[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Route Route spec path that have specail characters should not cause HaProxy error and ingress degraded [Serial]" \
  -n "[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Route UnmanagedRoutes metric should filter ingress by both name and namespace [Serial]" \
  --max-concurrency=1
```

### Run Only `dynamic_config_manager.go` Tests

```bash
# Run all eight DCM tests by name filter
./bin/router-tests-ext run-test \
  -n "[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Route-DCM Check whether required ENV variables are configured after enabling Dynamic Configuration Manager [Serial]" \
  -n "[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Route-DCM Dynamic Configuration Manager for plain HTTP route [Serial]" \
  -n "[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Route-DCM Enable ALPN for reencrypt routes when DCM is enabled [Serial]" \
  -n "[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Route-DCM Dynamic Configuration Manager for edge route [Serial]" \
  -n "[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Route-DCM Dynamic Configuration Manager for passthrough route [Serial]" \
  -n "[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Route-DCM Dynamic Configuration Manager for reencrypt route [Serial]" \
  -n "[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Route-DCM Traffic test for dynamic servers [Serial]" \
  -n "[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Route-DCM Map entries added in the end when DCM is enabled [Serial]" \
  --max-concurrency=1
```

### Run Only `route-admission.go` Tests

```bash
# Run all five route-admission tests by name filter
./bin/router-tests-ext run-test \
  -n "[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Route-Admission Set namespaceOwnership of routeAdmission to InterNamespaceAllowed" \
  -n "[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Route-Admission Set namespaceOwnership of routeAdmission to Strict" \
  -n "[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Route-Admission Update the namespaceOwnership of routeAdmission" \
  -n "[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Route-Admission Set wildcardPolicy of routeAdmission to WildcardsAllowed" \
  -n "[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Route-Admission Set wildcardPolicy of routeAdmission to WildcardsDisallowed" \
  --max-concurrency=1
```

### Run Only `headers.go` Tests

```bash
# Run all 14 route-headers tests by name filter
./bin/router-tests-ext run-test \
  -n "[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Route-Headers Configure Ingresscontroller to preserve existing header with forwardedHeaderPolicy set to Append" \
  -n "[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Route-Headers Configure Ingresscontroller to replace any existing Forwarded header with forwardedHeaderPolicy set to Replace" \
  -n "[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Route-Headers Configure Ingresscontroller to set the headers if they are not already set with forwardedHeaderPolicy set to Ifnone" \
  -n "[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Route-Headers Forwarded header includes empty quoted proto-version parameter" \
  -n "[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Route-Headers adding/deleting http headers to an edge route by a router owner" \
  -n "[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Route-Headers adding/deleting http headers to a http route by a router owner" \
  -n "[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Route-Headers supported max http headers, max length of a http header name, max length value of a http header" \
  -n "[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Route-Headers negative test of adding/deleting http headers" \
  -n "[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Route-Headers set different type of values for a http header name and its value" \
  -n "[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Route-Headers adding/deleting http headers to a http route by an ingress-controller as a cluster administrator" \
  -n "[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Route-Headers adding/deleting http headers to a reen route by a router owner" \
  -n "[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Route-Headers adding/deleting http headers to an edge route by an ingress-controller as a cluster administrator" \
  -n "[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Route-Headers adding/deleting http headers to a reen route by an ingress-controller as a cluster administrator" \
  -n "[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Route-Headers http request with duplicated headers should not cause disruption to a router pod" \
  --max-concurrency=1
```

### Run Only `route-hsts.go` Tests

```bash
# Run all seven route-HSTS tests by name filter
./bin/router-tests-ext run-test \
  -n "[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Route-HSTS The edge/reencrypt route supports HSTS but unsecure/passthrough route does not" \
  -n "[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Route-HSTS The includeSubDomainsPolicy parameter can configure subdomain policy to inherit the HSTS policy of parent domain [Serial]" \
  -n "[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Route-HSTS The includeSubDomainsPolicy and PreloadPolicy option can be configured to be permissive with NoOpinion flag [Serial]" \
  -n "[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Route-HSTS The PreloadPolicy option can be set to be enforced strictly to be present or absent in HSTS preload header checks [Serial]" \
  -n "[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Route-HSTS The Maxage HSTS policy strictly adheres to validation of route based on largestMaxAge and smallestMaxAge parameter [Serial]" \
  -n "[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Route-HSTS The HSTS domain policy can be configured with multiple domainPatterns options [Disruptive]" \
  -n "[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Route-HSTS Global HSTS policy can be enforced strictly on a specific namespace using namespaceSelector for given domain pattern filtering [Serial]" \
  --max-concurrency=1
```

### Run Only `logging.go` Tests

```bash
# Run all three route-logging tests by name filter
./bin/router-tests-ext run-test \
  -n "[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Route-Logging Capture and log http cookies with specific prefixes via httpCaptureCookies option" \
  -n "[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Route-Logging Capture and log http requests using UniqueID with custom logging format defined via httpHeader option" \
  -n "[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Route-Logging The httpCaptureHeaders option strictly adheres to the maxlength parameter" \
  --max-concurrency=1
```

### Run Only `route-weight.go` Tests

```bash
# Run all six route-weight tests by name filter
./bin/router-tests-ext run-test \
  -n "[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Route-Weight Sticky session could work normally after set weight for route" \
  -n "[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Route-Weight Set backends weight for edge/passthrough/reencrypt/unsecure route" \
  -n "[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Route-Weight Set multiple backends weight for route" \
  -n "[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Route-Weight Endpoint will end up weight 1 when scaled weight per endpoint is less than 1" \
  -n "[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Route-Weight Each endpoint gets weight/numberOfEndpoints portion of the requests" \
  -n "[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Route-Weight Alternate Backends and Weights for a route work well" \
  --max-concurrency=1
```

### Run Only `cookies.go` Tests

```bash
# Run all five cookie tests by name filter
./bin/router-tests-ext run-test \
  -n "[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Route-Cookies Enable/Disable haproxy cookies based sticky session for unsecure routes" \
  -n "[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Route-Cookies Cookie name should not use openshift prefix" \
  -n "[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Route-Cookies Can set cookie name for unsecure routes by annotation" \
  -n "[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Route-Cookies router.openshift.io/cookie-same-site route annotation accepts None Lax or Strict attribute for edge routes" \
  -n "[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Route-Cookies router.openshift.io/cookie-same-site route annotation accepts None Lax or Strict attribute for Reencrypt routes" \
  --max-concurrency=1
```

### Run Only `haproxy.go` Tests

```bash
# Run all 29 HAProxy tests by name filter
./bin/router-tests-ext run-test \
  -n "[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Haproxy Configmap mounted on router volume after ingresscontroller has spec field HttpErrorCodePage populated with configmap name" \
  -n "[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Haproxy Errorfile stanzas and dummy default html files have been added to the router" \
  -n "[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Haproxy HAProxyDown message only for pods and No reaper messages for zombie processes" \
  -n "[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Haproxy Health check when there are multi service and each service has one backend" \
  -n "[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Haproxy Idling StatefulSet is not supported" \
  -n "[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Haproxy Ingress controller stats port is not set according to endpointPublishingStrategy" \
  -n "[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Haproxy IngressController option to use PROXY protocol with IBM Cloud load-balancers - TCP, PROXY and omitted" \
  -n "[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Haproxy Ingresscontroller routes traffic only to ready pods/backends" \
  -n "[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Haproxy Ingresscontroller with private endpoint publishing strategy supports PROXY protocol" \
  -n "[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Haproxy No health check when there is only one endpoint for a route" \
  -n "[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Haproxy Power-of-two feature allows unsupportedConfigOverrides ingress operator option to enable leastconn balancing algorithm" \
  -n "[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Haproxy Routers with hostnetwork endpoint strategy with same http/https/stat port numbers cannot be deployed on the same worker node [Serial]" \
  -n "[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Haproxy Set Cookie2 by an application in a route should not kill all router pods" \
  -n "[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Haproxy Setting ROUTER_ENABLE_COMPRESSION and ROUTER_COMPRESSION_MIME in HAProxy" \
  -n "[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Haproxy The Power of two balancing  honours the per route balancing algorithm defined via haproxy.router.openshift.io/balance annotation" \
  -n "[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Haproxy The Power-of-two balancing features defaults to random LB algorithm instead of leastconn for REEN/Edge/insecure routes" \
  -n "[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Haproxy The Power-of-two balancing features switches to roundrobin mode for REEN/Edge/insecure/passthrough routes with multiple backends configured with weights" \
  -n "[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Haproxy The Power-of-two balancing features switches to source algorithm for passthrough routes" \
  -n "[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Haproxy The backend health check interval of unsecure/edge/passthrough/reencrypt route can be set by annotation" \
  -n "[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Haproxy The endpointPublishingStrategy parameter allows TCP, PROXY, and empty definitions for HostNetwork [Serial]" \
  -n "[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Haproxy The logEmptyRequests ingresscontroller parameter set to Ignore add the dontlognull option in the haproxy configuration" \
  -n "[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Haproxy The logEmptyRequests option only gets applied when the access logging is configured for the ingresscontroller" \
  -n "[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Haproxy User can customize HAProxy error pages" \
  -n "[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Haproxy User can delete configmap and update configmap with new custom error page" \
  -n "[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Haproxy httpEmptyRequestsPolicy ingresscontroller parameter set to ignore adds the http-ignore-probes option in the haproxy configuration" \
  -n "[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Haproxy misc tests for haproxy router" \
  -n "[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Haproxy router continues to function normally with the service selector of exposed route gets removed/deleted" \
  -n "[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Haproxy router support HTTP2 for passthrough route and reencrypt route with custom certs" \
  -n "[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Haproxy the 503 Error page should not contain license for a vulnerable release of Bootstrap" \
  --max-concurrency=1
```

Both lists select tests by their full names. If a test title changes, refresh
the list with `list tests -o names`. The `[Serial]` and `[Disruptive]` cases
are included on purpose; `--max-concurrency=1` keeps them apart, but the
three disruptive cases still change platform routes, so run them only on a
disposable cluster.

### Run a single test

```bash
./bin/router-tests-ext run-test "[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Route NetworkEdge Set insecureEdgeTerminationPolicy to Redirect for passthrough/edge/reencrypt route"
```

`run-test` requires the exact full name. To select by case ID instead:

```bash
./bin/router-tests-ext list tests -o json \
  | jq -r '.[] | select(.labels | has("OCP-11036")) | .name' \
  | ./bin/router-tests-ext run-test
```

For an environment-filtered suite, use the same pipe with the relevant flags:

```bash
./bin/router-tests-ext list tests --suite router/non-disruptive \
  --api-group config.openshift.io --topology External -o names \
  | grep '^\[sig-network-edge\]\[Feature:Router\]' \
  | ./bin/router-tests-ext run-test
```

The name filters also remove startup log lines before passing input to
`run-test`.

### Save results

`run-suite` supports JUnit and HTML report files. Create their directory first:

```bash
mkdir -p artifacts
./bin/router-tests-ext run-suite router/all \
  --junit-path artifacts/junit-router-all.xml \
  --html-path artifacts/router-all.html
```

The suite command also saves an `extension_test_result_e2e_*.json` file beside
the JUnit file. Both run commands print results as JSON by default, or JSON Lines
with `-o jsonl`, alongside test logs. All specs currently have the informing
lifecycle, so review each test's `result` and the reports even when the command
exits successfully. `run-test` does not support `--junit-path` or the private
runner's `--junit-dir` and `-f` flags; it accepts piped test names directly.

### HTTP proxy behavior

Local curl requests honor `http_proxy`, `https_proxy`, and `no_proxy`. Some CI
lanes require the proxy to reach application routes. Case 11036 parses redirect
headers and accepts capitalization changes such as `location` to `Location`
while still checking the `302` status and the exact HTTPS destination.

In case 11036, the API errors for the passthrough policy `Allow` and the edge
policy `Abc` are expected negative checks. They do not indicate a test failure.

### Migrated-suite safety and validation

The cookie, logging, header, DCM, and admission specs use the router pod's
primary address and `CurlResolve` to bracket IPv6 addresses in `--resolve`.
Formatting addresses for either family does not replace an IPv6-only or
disconnected live-cluster run. Those runs remain required before claiming
compatibility for the migrated suites.

Global HSTS specs append test-owned policies without disabling existing cluster
policies. Updates retry resource-version conflicts, and cleanup removes only the
entries owned by the spec, preserving policies added by other clients. Test
domains are unique per invocation. The HTTPBin fixture does not request
privileged containers, and header specs do not relax namespace Pod Security
labels. The fixture and admission behavior still require live validation.

### Scheduling

`--max-concurrency=1` is **not** required, including for suites such as
`router/non-disruptive` that mix parallel and `[Serial]` tests.  `cmd/main.go`
gives every spec an isolation taint instead: `[Serial]` and `[Disruptive]`
specs are tainted `disruptive` and tolerate nothing, while parallel specs are
tainted `parallel-running` and tolerate only that.  The scheduler refuses to
start a spec that cannot tolerate every currently active taint, so a serial
spec waits for all parallel specs to drain and vice versa, and serial specs
never overlap.

That makes the taints, not the flag, the thing to protect.  If you change the
isolation in `cmd/main.go`, re-check that serial tests still run alone.  Note
also that running at concurrency 1 does not by itself prove isolation -- it
only hides the absence of it -- so verify suite membership and counts with
`list tests` as well.

The default maximum concurrency is 10. Isolation applies within one runner
invocation; separate test processes do not coordinate their shared cluster
changes. `[Disruptive]` alone preserves isolation, so removing `[Serial]` from
the disruptive cases does not make them run concurrently in `router/all` or
`router/disruptive`.

### Validate local changes

From `tests-extension/`:

```bash
# Unit tests and compilation of the test module; no cluster is needed
go test ./...

# Regenerate QE fixtures only after changing testdata/, then commit the output
# make update-bindata

# Rebuild the binary and check committed OTE metadata and fixture freshness
make verify

# Check handwritten Go sources; bindata.go is generated
gofmt -l cmd test/*.go test/qe/cookies.go test/qe/logging.go test/qe/route-weight.go test/qe/headers.go test/qe/route-hsts.go test/qe/dynamic_config_manager.go test/qe/route-admission.go test/qe/route.go test/qe/fixtures.go
```

Run `make verify check` from the repository root as well. The root unit-test
target covers the router module; the extension is a separate Go module and
needs its own `go test ./...` command. Commit regenerated fixtures or metadata
when they change.

## Pre-merge validation

Local runs prove neither platform applicability nor payload discovery.  Before
merging a change that adds or renames tests, run the payload jobs that most
often surface new-test problems — in particular the metal IPv6 job, which is
where the original migration broke:

```text
/payload-job periodic-ci-openshift-hypershift-release-<release>-periodics-e2e-aws-ovn-conformance
/payload-job periodic-ci-openshift-release-master-nightly-<release>-e2e-metal-ipi-ovn-ipv6
/payload-job periodic-ci-openshift-release-master-ci-<release>-e2e-aws-upgrade-ovn-single-node
/payload-job periodic-ci-openshift-hypershift-release-<release>-periodics-e2e-aws-ovn-conformance-serial
/payload-job periodic-ci-openshift-release-master-nightly-<release>-e2e-aws-ovn-single-node-serial
```

While the temporary disconnected exclusion is present, the metal IPv6 job and
other disconnected jobs should exclude all router extension tests during
discovery. A passing job therefore validates the exclusion, not router test
coverage in those environments. Connected and proxied IPv6 clusters remain
eligible and need live validation using an appropriate test step or cluster.
The follow-up PR must restore and exercise disconnected coverage using the
checklist above.

If the origin registration PR is not yet merged, test them together:

```text
/payload-job-with-prs <job_name> openshift/origin#<PR_NUMBER>
```

## Test execution time

These are estimates; image imports, cluster capacity, and network latency affect
the duration:

- Single test: 2-10 minutes
- Non-disruptive suite: 15-30 minutes
- Disruptive suite: 15-30 minutes
- All tests: 45-90 minutes

## Files

```text
tests-extension/
├── cmd/
│   └── main.go                         # OTE entry point, suites, environment selectors
├── test/
│   ├── util.go                         # Shared helpers, fixture image mirroring
│   └── qe/
│       ├── route.go                    # 42 Route test implementations
│       ├── haproxy.go                  # 29 HAProxy test implementations
│       ├── cookies.go                  # 5 cookie test implementations
│       ├── logging.go                  # 3 route-logging test implementations
│       ├── route-weight.go             # 6 route-weight test implementations
│       ├── headers.go                  # 14 route-headers test implementations
│       ├── route-hsts.go               # 7 route-HSTS test implementations
│       ├── dynamic_config_manager.go   # 8 DCM test implementations
│       ├── route-admission.go          # 5 route-admission test implementations
│       ├── bindata.go                  # Embedded test fixtures (auto-generated)
│       ├── fixtures.go                 # TestdataDir/FixturePath helpers
│       ├── bindata.mk                  # Bindata generation makefile
│       ├── README.md                   # This file
│       └── testdata/
│           ├── 49802-route.yaml        # Edge+HTTP route template (test 49802)
│           ├── ca-bundle.pem           # CA certificate for reencrypt routes
│           ├── httpbin-deploy.yaml     # Httpbin deployment for timeout tests
│           ├── error-page-404.http     # Custom 404 error page fixture
│           ├── error-page-503.http     # Custom 503 error page fixture
│           ├── error-page2-404.http    # Alternate 404 error page fixture
│           ├── error-page2-503.http    # Alternate 503 error page fixture
│           ├── ingresscontroller-IBMproxy.yaml # IBM proxy protocol fixture
│           ├── ingresscontroller-hn-PROXY.yaml # Host-network proxy protocol fixture
│           ├── ingresscontroller-hostnetwork-only.yaml # Host-network controller fixture
│           ├── ingresscontroller-private.yaml # Private endpoint fixture
│           ├── ocp57404-stateful-set.yaml # StatefulSet idling fixture
│           ├── svc-additional-backend.yaml # Additional backend service fixture
│           ├── ingress-with-class.yaml # Ingress with class template (test 88075)
│           ├── ingresscontroller-np.yaml  # Custom ingresscontroller template
│           ├── template-web-server-deploy.yaml  # Templated web server (test 12091)
│           ├── test-client-pod.yaml    # Client pod for internal curl tests
│           ├── web-server-deploy.yaml  # Standard web server deployment
│           ├── web-server-signed-deploy.yaml  # Web server with TLS certificates
│           ├── websocket-deploy.yaml   # WebSocket server (test 17145)
│           └── subdomain-routes/
│               └── route.yaml          # Subdomain route template (test 10024)
├── .openshift-tests-extension/         # Generated test metadata (make update-metadata)
├── Makefile                            # Build targets
├── go.mod                              # Dependencies
├── go.sum                              # Dependency checksums
└── vendor/                             # Vendored dependencies
```
