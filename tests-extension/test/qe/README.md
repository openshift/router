# Router OTE Test Extension

Route test cases migrated from `openshift-tests-private` to the OTE
(openshift-tests-extension) framework, under the NE-2126 migration epic.

| File | Tests | Description |
|------|-------|-------------|
| `route.go` | 42 | Router/Route lifecycle tests (route types, timeouts, cookies, annotations, whitelisting, allowlisting) |

## Test naming conventions

Every test name has the shape:

```text
[sig-network-edge][OTP][Level0] Network_Edge Component_Router <description> [<execution tags>]
```

| Token | Meaning |
|-------|---------|
| `[OTP]` | Ported from openshift-tests-private; used to track migration volume. |
| `[sig-network-edge]` | Broad-area grouping required for component mapping. |
| `[Level0]` | Tracking tag that selects the test into the `router/conformance/*` suites. |
| `OCP-<case ID>` label | Canonical case identity stored with `g.Label`, independent of the title. |
| `[Serial]` / `[Disruptive]` | Scheduling isolation; see [Test suites](#test-suites). |

Case IDs are Ginkgo labels, and environment selectors use those labels. Priority
prefixes are omitted. The prefix and execution-tag spacing match the DNS extension.
These descriptive titles form the initial OTE registration metadata, matching DNS.

`[Level0]` should reflect the QE case classification. The current OTP source has
no explicit Level0 markers for these 42 migrated cases, so the existing tags are
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
Check the names emitted by the payload runner: the existing `Network_Edge`
suite matcher covers the private runner's suite name, while the router SIG
matcher also requires `[Feature:Router]`. If the payload names do not match,
add a router ownership matcher in `pkg/components/networking/router/component.go`
in ci-test-mapping and verify the resulting mappings before merge.

### Renaming or removing a test

`make update-metadata` compares the binary against the committed
`.openshift-tests-extension/openshift_payload_router.json` and **refuses to
regenerate it** when a test has disappeared from the binary without being
declared:

```text
Missing Tests:
  * [sig-network-edge][OTP][Level0] Network_Edge Component_Router <previous description>
Error: missing tests, if you've renamed tests you must add their names to
       OriginalName, or mark them obsolete
```

A rename counts as a disappearance, so declare it *before* regenerating. For
the first rename, record the previously registered name as `OriginalName` on
the spec in [`cmd/main.go`](../../cmd/main.go). For later renames, retain that
first recorded name; update the current-name match without overwriting an
existing `OriginalName`:

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
- All 42 tests are temporarily excluded on disconnected clusters
  (`externalConnectivity == "Disconnected"`), including IPv4, IPv6-only, and
  dual-stack, because fixture setup still requires the source registry.
  Connected and proxied clusters remain eligible on every network stack,
  provided the source registry is reachable. See
  [Temporary disconnected exclusion](#temporary-disconnected-exclusion).
- Cases 11036, 13753, 13839, 14678, 17145, 55895, and 56240 are excluded when
  the control plane topology is `External` (HyperShift).
- Case 56240 requires worker and infra Machines and is also excluded on
  `SingleReplica` (single-node) clusters.

`list tests` without environment flags lists all registered tests. To inspect
environment selection, supply the supported API group together with the other
environment flags. For example, this lists the 35 tests selected for HyperShift:

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
cannot express.  `util.MirrorFixtureImages` therefore reads the operator's
management state and **skips** the spec when there is no registry to mirror
through, so such a cluster reports a skip rather than a pull failure that looks
like a routing bug.

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

# All 42 remain eligible on connected or proxied clusters, before other filters.
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
| `router/conformance/parallel` | `openshift/conformance/parallel` | 37 |
| `router/conformance/serial` | `openshift/conformance/serial` | 5 |
| `router/disruptive` | — | 3 |
| `router/non-disruptive` | — | 39 |
| `router/all` | — | 42 |

Every test that mutates shared cluster state is tagged `[Serial]`, and the
three that are also destructive carry `[Serial][Disruptive]`.  Tagging the
disruptive ones `[Serial]` as well is what puts them in
`openshift/conformance/serial`; `openshift/disruptive` is not run by any
payload job, so without it they would never run in CI.  These 5 tests mutate
shared cluster state (ingress controllers, canary routes) and are scheduled
with isolation taints so they never run concurrently with other tests.

The counts above describe registered tests before environment filtering and
runtime skips.

### Disruptive tests

- 53696 - Route status should updates accordingly when ingress routes cleaned up [Disruptive] [Serial]
- 55895 - Ingress should be in degraded status when canary route is not available [Disruptive] [Serial]
- 56240 - Canary daemonset can schedule pods to both worker and infra nodes [Disruptive] [Serial]

### Serial tests

- 85274 - Route spec path that have specail characters should not cause HaProxy error and ingress degraded [Serial]
- 88075 - UnmanagedRoutes metric should filter ingress by both name and namespace [Serial]

## How to Run

Use Go 1.26 or newer (see `tests-extension/go.mod`), `make`, and an OpenShift
cluster with administrator access. Put `oc`, `curl`, and `bash` on `PATH`.
Some cases also invoke GNU `sed` with `-i`; on macOS, put GNU `sed` on `PATH`
for those cases. The build installs `go-bindata` v3.1.3 into `$(go env GOPATH)/bin`
if it is missing, so the first build needs access to the Go module proxy.
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
The component Dockerfiles gzip the binary and include it in the router image.

Use `--help` on a subcommand to see its flags, for example
`./bin/router-tests-ext run-suite --help`. Listing tests does not execute them.

### Run all tests

```bash
./bin/router-tests-ext run-suite router/all
```

### Run by suite

```bash
# All tests except the disruptive ones.  Note this suite still contains the two
# [Serial] tests, which the scheduler isolates; see Scheduling below.
./bin/router-tests-ext run-suite router/non-disruptive

./bin/router-tests-ext run-suite router/disruptive

# Run the conformance suites separately
./bin/router-tests-ext run-suite router/conformance/parallel
./bin/router-tests-ext run-suite router/conformance/serial
```

### Run a single test

```bash
./bin/router-tests-ext run-test "[sig-network-edge][OTP][Level0] Network_Edge Component_Router NetworkEdge Set insecureEdgeTerminationPolicy to Redirect for passthrough/edge/reencrypt route"
```

`run-test` requires the exact full name. To select by case ID instead:

```bash
./bin/router-tests-ext list tests -o names \
  | grep -- '-11036-' \
  | ./bin/router-tests-ext run-test
```

For an environment-filtered suite, use the same pipe with the relevant flags:

```bash
./bin/router-tests-ext list tests --suite router/non-disruptive \
  --api-group config.openshift.io --topology External -o names \
  | grep '^\[OTP\]' \
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

### Scheduling

`--max-concurrency=1` is **not** required, including for suites such as
`router/non-disruptive` that mix parallel and `[Serial]` tests.  `cmd/main.go`
gives every spec an isolation taint instead: `[Serial]` and `[Disruptive]`
specs are tainted `disruptive` and tolerate nothing, while parallel specs are
tainted `parallel-running` and tolerate only that.  The scheduler refuses to
start a spec that cannot tolerate every currently active taint, so a serial
spec waits for all parallel specs to drain and vice versa, and two serial specs
never overlap.

That makes the taints, not the flag, the thing to protect.  If you change the
isolation in `cmd/main.go`, re-check that serial tests still run alone.  Note
also that running at concurrency 1 does not by itself prove isolation -- it
only hides the absence of it -- so verify suite membership and counts with
`list tests` as well.

The default maximum concurrency is 10. Isolation applies within one runner
invocation; separate test processes do not coordinate their shared cluster
changes.

### Validate local changes

From `tests-extension/`:

```bash
# Unit tests and compilation of the test module; no cluster is needed
go test ./...

# Rebuild the binary and check the committed OTE metadata and embedded fixtures
make verify

# Check handwritten Go sources; bindata.go is generated
gofmt -l cmd test/*.go test/qe/route.go test/qe/fixtures.go
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
│       ├── route.go                    # Route test implementations
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
├── .openshift-tests-extension/         # Generated test metadata (make update-metadata)
├── Makefile                            # Build targets
├── go.mod                              # Dependencies
├── go.sum                              # Dependency checksums
└── vendor/                             # Vendored dependencies
```

## All test names

```text
[sig-network-edge][OTP][Level0] Network_Edge Component_Router Route could NOT be updated after created
[sig-network-edge][OTP][Level0] Network_Edge Component_Router Set balance leastconn for passthrough routes
[sig-network-edge][OTP][Level0] Network_Edge Component_Router NetworkEdge Should use the same cookies for secure and insecure access when insecureEdgeTerminationPolicy set to allow for edge/reencrypt route
[sig-network-edge][OTP][Level0] Network_Edge Component_Router Service endpoint can be work well if the mapping pod ip is updated
[sig-network-edge][OTP][Level0] Network_Edge Component_Router NetworkEdge Set insecureEdgeTerminationPolicy to Redirect for passthrough/edge/reencrypt route
[sig-network-edge][OTP][Level0] Network_Edge Component_Router NetworkEdge oc help information should contain option wildcard-policy
[sig-network-edge][OTP][Level0] Network_Edge Component_Router NetworkEdge Enable/Disable haproxy cookies based sticky session for edge termination routes
[sig-network-edge][OTP][Level0] Network_Edge Component_Router Limit the number of TCP connection per IP in specified time period
[sig-network-edge][OTP][Level0] Network_Edge Component_Router NetworkEdge Set timeout server for passthough route
[sig-network-edge][OTP][Level0] Network_Edge Component_Router haproxy hash based sticky session for tcp mode passthrough routes
[sig-network-edge][OTP][Level0] Network_Edge Component_Router NetworkEdge Set timeout server for http route
[sig-network-edge][OTP][Level0] Network_Edge Component_Router haproxy config information should be clean when changing the service to another route
[sig-network-edge][OTP][Level0] Network_Edge Component_Router reencrypt route with no cert if a router is configured with a default wildcard cert
[sig-network-edge][OTP][Level0] Network_Edge Component_Router The path specified in route can work well for edge/unsecure termination
[sig-network-edge][OTP][Level0] Network_Edge Component_Router The path specified in route can work well for reencrypt terminated
[sig-network-edge][OTP][Level0] Network_Edge Component_Router The later route should be HostAlreadyClaimed when there is a same host exist
[sig-network-edge][OTP][Level0] Network_Edge Component_Router NetworkEdge Check the cookie if using secure mode when insecureEdgeTerminationPolicy to Redirect for edge/reencrypt route
[sig-network-edge][OTP][Level0] Network_Edge Component_Router NetworkEdge Set insecureEdgeTerminationPolicy to Allow for reencrypt/edge route
[sig-network-edge][OTP][Level0] Network_Edge Component_Router NetworkEdge Only the host in whitelist could access unsecure/edge/reencrypt/passthrough routes
[sig-network-edge][OTP][Level0] Network_Edge Component_Router router can do a case-insensitive match of a hostname for unsecure/edge/passthrough/reencrypt route
[sig-network-edge][OTP][Level0] Network_Edge Component_Router NetworkEdge can set cookie name for edge/reen routes by annotation
[sig-network-edge][OTP][Level0] Network_Edge Component_Router NetworkEdge Check haproxy.config when overwriting 'timeout server' which was already specified
[sig-network-edge][OTP][Level0] Network_Edge Component_Router haproxy router support websocket via unsecure route
[sig-network-edge][OTP][Level0] Network_Edge Component_Router NetworkEdge limits backend pod max concurrent connections for unsecure, edge, reen, passthrough route
[sig-network-edge][OTP][Level0] Network_Edge Component_Router NetworkEdge limits multiple backend pods max concurrent connections
[sig-network-edge][OTP][Level0] Network_Edge Component_Router Unsecure route with path and another tls route with same hostname can work at the same time
[sig-network-edge][OTP][Level0] Network_Edge Component_Router NetworkEdge Routes annotated with 'haproxy.router.openshift.io/rewrite-target=/path' will replace and rewrite http request with specified '/path'
[sig-network-edge][OTP][Level0] Network_Edge Component_Router NetworkEdge 'haproxy.router.openshift.io/timeout-tunnel' annotation gets applied alongside 'haproxy.router.openshift.io/timeout' for clear/edge/reencrypt routes
[sig-network-edge][OTP][Level0] Network_Edge Component_Router NetworkEdge 'haproxy.router.openshift.io/timeout-tunnel' annotation takes precedence over 'haproxy.router.openshift.io/timeout' values for passthrough routes
[sig-network-edge][OTP][Level0] Network_Edge Component_Router route can be configured to whitelist more than 61 ips/CIDRs
[sig-network-edge][OTP][Level0] Network_Edge Component_Router ingress controller continue to function normally with unexpected high timeout value
[sig-network-edge][OTP][Level0] Network_Edge Component_Router ingress canary route redirects http to https
[sig-network-edge][OTP][Level0] Network_Edge Component_Router HTTPS redirect happens even if there is a more specific http-only
[sig-network-edge][OTP][Level0] Network_Edge Component_Router Route status should updates accordingly when ingress routes cleaned up [Disruptive] [Serial]
[sig-network-edge][OTP][Level0] Network_Edge Component_Router Ingress should be in degraded status when canary route is not available [Disruptive] [Serial]
[sig-network-edge][OTP][Level0] Network_Edge Component_Router Canary daemonset can schedule pods to both worker and infra nodes [Disruptive] [Serial]
[sig-network-edge][OTP][Level0] Network_Edge Component_Router Ipv6 addresses are also acceptable for whitelisting
[sig-network-edge][OTP][Level0] Network_Edge Component_Router NetworkEdge Only host in allowlist can access unsecure/edge/reencrypt/passthrough routes
[sig-network-edge][OTP][Level0] Network_Edge Component_Router NetworkEdge Route gives allowlist precedence when whitelist and allowlist annotations are both present
[sig-network-edge][OTP][Level0] Network_Edge Component_Router NetworkEdge Route does not enable allowlist with than 61 CIDRs and if invalid IP annotation is given
[sig-network-edge][OTP][Level0] Network_Edge Component_Router Route spec path that have specail characters should not cause HaProxy error and ingress degraded [Serial]
[sig-network-edge][OTP][Level0] Network_Edge Component_Router UnmanagedRoutes metric should filter ingress by both name and namespace [Serial]
```
