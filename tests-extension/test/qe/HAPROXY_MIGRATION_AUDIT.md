# HAProxy router test migration audit

This audit compares the 42 cases in
`openshift-tests-private/test/extended/router/haproxy-router.go` with tests in
`origin/test/extended/router`, `origin/test/extended/cli`, and
`cluster-ingress-operator/test/e2e` and `pkg/operator/controller/ingress`.
It describes the existing test assertions, not just matching test titles.
The local source revisions examined were private `4246aa6e1`, origin
`655e427e38`, and cluster-ingress-operator `dd718f260`.

Six cases have strong existing coverage, 21 have some relevant coverage but
retain distinct assertions, and 15 have no matching test scenario. This is a
static source comparison; it does not establish that any migrated test passes
on a live cluster.

| Private case | Coverage | Closest existing test and remaining assertion |
| --- | --- | --- |
| 15044 | None | CIO `TestHealthCheckIntervalIngressController` tests an ingress-controller tuning option, not the per-route annotation across four termination types. |
| 16870 | None | No test verifies the generated health checks when a route has only one endpoint and after scaling to two. |
| 16872 | None | No test verifies health checks for multiple services with one endpoint each. |
| 21766 | Partial | Origin `metrics.go` checks that Prometheus scrapes router targets; the private case also checks router monitoring objects and default resources. |
| 27628 | Partial | Origin `http2.go` sends HTTP/2 traffic with custom certificates. The private case also configures required client TLS on a custom ingress controller and checks `cert_config.map`/ALPN. |
| 37714 | None | No test exercises readiness transitions by removing and restoring backend pods while checking route responses. |
| 40675 | Partial | CIO `TestProxyProtocolAPI` covers NodePort and unit tests cover HostNetwork; no HostNetwork end-to-end protocol check was found. |
| 40677 | Strong | CIO `TestProxyProtocolAPI` verifies NodePort PROXY deployment configuration. |
| 40679 | Partial | CIO controller unit tests cover TCP, PROXY, and omitted protocol values. The private implementation only exercises HostNetwork transitions. |
| 41042 | Partial | CIO `TestLoadBalancingAlgorithmUnsupportedConfigOverride` checks default algorithm environment variables; it does not inspect generated backends for HTTP, edge, and reencrypt routes. |
| 41186 | Partial | Origin `weighted.go` checks weighted HTTP traffic; the private case inspects round-robin configuration for all four route types. |
| 41187 | Partial | Origin `config_manager_ingress.go` checks round-robin behavior; the private case checks a leastconn route annotation in generated configuration. |
| 41206 | Partial | CIO `TestLoadBalancingAlgorithmUnsupportedConfigOverride` checks the override environment variable; the private case also checks an edge backend's generated configuration. |
| 41929 | None | No test checks router reload logs after removing a routed service's selector. |
| 42878 | None | No test checks default HAProxy `errorfile` stanzas and dummy HTML files. |
| 42940 | Partial | CIO `TestCustomErrorpages` checks generated configuration and propagation; the private case also sends live requests and checks 404/503 responses. |
| 43115 | Partial | CIO error-page tests check generated configuration and volume declarations; the private case checks mounted files inside router pods. |
| 43292 | Partial | CIO `TestCustomErrorpages` checks config-map recreation and updates; the private case uses a different deletion/recreation sequence. Its final response-content assertion is commented out. |
| 43414 | Partial | CIO `TestDesiredRouterDeploymentSpecAndNetwork` checks `ROUTER_DONT_LOG_NULL`; the private case also checks `dontlognull` in HAProxy configuration. |
| 43416 | Partial | CIO `TestDesiredRouterDeploymentSpecAndNetwork` checks `ROUTER_HTTP_IGNORE_PROBES`; the private case also checks `http-ignore-probes` in HAProxy configuration. |
| 43454 | None | No test checks the access-logging prerequisite and default `Log` value. The private assertion against an empty logging field is ineffective. |
| 46571 | Partial | CIO compression tests cover MIME parsing, environment variables, and real gzip traffic; the private case checks an additional quoted MIME list and generated HAProxy directives. |
| 46898 | Partial | CIO `TestRouterCompressionParsing` rejects invalid MIME values; the private case also checks the absence of compression configuration. |
| 47344 | Partial | CIO deployment unit tests cover IPv4, IPv6, and dual-stack environment values; Origin `dualstack.go` tests AWS dual-stack traffic. The private case inspects live router pod values across all three modes. |
| 49131 | None | Origin `multi-haproxy.go` checks running HAProxy version behavior, but not a fixed patch version, runtime socket version, or RHEL base image. |
| 50074 | Partial | CIO `TestTunableRouterKubeletProbesForCustomIngressController` checks probe reconciliation. The private case also checks the maximum integer timeout and running pod values. |
| 50075 | None | No test rejects negative or string probe timeout values through the API. |
| 50405 | Strong | CIO `TestHostNetworkPortBinding` checks two HostNetwork routers with different ports on one node. |
| 50406 | None | No test rejects negative HTTP, HTTPS, and stats port values. The private implementation patches HTTP port three times. |
| 50819 | None | No test checks scheduling failure when two HostNetwork routers reserve the same ports on one node. |
| 52738 | Partial | CIO load-balancing tests check the TCP default environment value; the private case checks the generated passthrough backend's `source` algorithm. |
| 53048 | Partial | CIO controller unit tests cover Private protocol transitions; no Private ingress controller end-to-end PROXY binding check was found. |
| 54998 | None | No test sends a `Set-Cookie2` response through a route. |
| 55825 | None | No test checks the Bootstrap license content of a router 503 page. The private case currently makes a HEAD request, which does not inspect the body. |
| 56898 | Strong | Origin `idle.go` checks route access waking an idled deployment and restoring service/endpoints. |
| 57001 | Strong | Origin `grpc-interop.go` tests real gRPC over an edge route with an h2c service port. |
| 57404 | None | Origin CLI idling tests cover deployments, not rejection of StatefulSet idling while the route remains reachable. |
| 57406 | None | No test checks the precise `HAProxyDown` alert message and absence of reaper/zombie process logs. The Prometheus rule itself is not test coverage. |
| 59951 | Partial | CIO controller unit tests cover IBM status transitions; no IBM load-balancer end-to-end service annotation and router environment check was found. |
| 62926 | Partial | CIO `TestHostNetworkPortBinding` checks custom initial ports. The private case changes an existing controller's ports and checks pods, service target ports, and endpoints. |
| 79514 | Strong | CIO idle-connection tests check Immediate and Deferred behavior with live connections; deployment unit tests check the environment variable. |
| 88325 | Strong | CIO `TestMaxConnectionsMetric` checks the default and tuned Prometheus `maxconn` metric. |

This migration carries all 42 cases for a separate overlap review. The six
cases marked Strong are likely candidates for removal: 40677, 50405, 56898,
57001, 79514, and 88325. Case 43292 still needs a functional repair: its
final response assertion is commented out. The migration corrects the
HostNetwork namespace and rollout checks in 40679, the empty logging assertion
in 43454, the port fields in 50406, and the response-body check in 55825.
