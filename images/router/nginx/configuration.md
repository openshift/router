# The NGINX Router Configuration

The NGINX Router can be customized using environment variables or annotations.

## Environment Variables

The NGINX Router supports the majority of the standard Router environment variables, described in [this document](https://docs.okd.io/latest/architecture/networking/routes.html#router-environment-variables).

The NGINX Router supports the following standard Router environment variables:
* `DEFAULT_CERTIFICATE`
* `EXTENDED_VALIDATION`
* `NAMESPACE_LABELS`
* `RELOAD_SCRIPT`
* `ROUTER_ALLOWED_DOMAINS`
* `ROUTER_DEFAULT_CLIENT_TIMEOUT`
* `ROUTER_DEFAULT_CONNECT_TIMEOUT`
* `ROUTER_DEFAULT_SERVER_TIMEOUT`
* `ROUTER_DEFAULT_TUNNEL_TIMEOUT`
* `ROUTER_DENIED_DOMAINS`
* `ROUTER_ENABLE_INGRESS`
* `ROUTER_LISTEN_ADDR`
* `ROUTER_LOG_LEVEL`
* `ROUTER_MAX_CONNECTIONS`
* `ROUTER_OVERRIDE_HOSTNAME`
* `ROUTER_SERVICE_HTTPS_PORT`
* `ROUTER_SERVICE_HTTP_PORT`
* `ROUTER_SERVICE_NAME`
* `ROUTER_CANONICAL_HOSTNAME`
* `ROUTER_SERVICE_NAMESPACE`
* `ROUTER_SERVICE_SNI_PORT`
* `ROUTER_SLOWLORIS_HTTP_KEEPALIVE`
* `ROUTER_SUBDOMAIN`
* `ROUTER_SYSLOG_ADDRESS`
* `ROUTE_LABELS`
* `RELOAD_INTERVAL`
* `ROUTER_USE_PROXY_PROTOCOL`
* `ROUTER_ALLOW_WILDCARD_ROUTES`
* `ROUTER_DISABLE_NAMESPACE_OWNERSHIP_CHECK`

The following standard environment variables are supported, but have some differences comparing with the default Router:
* `DEFAULT_CERTIFICATE_DIR`. The default value is `/etc/pki/tls/private`.
* `ROUTER_TCP_BALANCE_SCHEME`. Specifies the load balancing algorithm for TCP/UDP and passthrough upstreams. Supported values are `round_robin`, `random`, `random_two`, `least_conn`, `ip_hash` and `least_time` (NGINX Plus). The default is `random_two`.
* `ROUTER_LOAD_BALANCE_ALGORITHM`. Specifies the load balancing algorithm for HTTP upstreams. Supported values are `round_robin`, `random`, `random_two`, `least_conn`, `ip_hash` and `least_time` (NGINX Plus). The default is `random_two`.
* `ROUTER_METRICS_TYPE`. The value must be empty.
* `ROUTER_SYSLOG_FORMAT`. Specifies the log format for a server that routes HTTP(s) traffic.
* `TEMPLATE_FILE`. The default value is `/var/lib/nginx/conf/nginx-config.template`.


The following standard environment variables are not available:
* `DEFAULT_CERTIFICATE_PATH`
* `DROP_SYN_DURING_RESTART`
* `ROUTER_BACKEND_CHECK_INTERVAL`
* `ROUTER_BACKEND_PROCESS_ENDPOINTS`
* `ROUTER_CLIENT_FIN_TIMEOUT`
* `ROUTER_COOKIE_NAME`
* `ROUTER_COMPRESSION_MIME`
* `ROUTER_DEFAULT_SERVER_FIN_TIMEOUT`
* `ROUTER_ENABLE_COMPRESSION`
* `ROUTER_SERVICE_NO_SNI_PORT`
* `ROUTER_SLOWLORIS_TIMEOUT`
* `STATS_PASSWORD`
* `STATS_USERNAME`
* `ROUTER_STRICT_SNI`


The NGINX Router includes the following additional environment variables:
* `ROUTER_SERVICE_UNREACHABLE_PORT`. For Routes with zero endpoints, the Router sends passthrough connections to `127.0.0.1:<ROUTER_SERVICE_UNREACHABLE_PORT>`. The Router assumes that nothing is running on that port. The default is `10446`.
* `ROUTER_PROXY_PROTOCOL_TRUSTED_SOURCE`. Configures trusted sources of client connections when PROXY PROTOCOL is enabled (using `ROUTER_USE_PROXY_PROTOCOL` variable). The default is `0.0.0.0/0`, which means any source is trusted. Configure this variable with the value of the IP address or the subnet of outgoing connections of your external load balancer that uses PROXY PROTOCOL. When the load balancer is not trusted, the client IP address is not extracted using the PROXY PROTOCOL and the IP address of the load balancer is used as the client IP.
* `ROUTER_SERVICE_INTERNAL_PASSTHROUGH_PORT`. Specifies the port of the helper server that routes passthrough connections.  The default is `10447`.
* `ROUTER_SYSLOG_FORMAT_FOR_PASSTHROUGH`. Specifies the log format for the passthrough server -- the server, which handles both HTTPS and passthrough connections, before forwarding them to the Router internal helper servers.
* `ROUTER_SYSLOG_FORMAT_FOR_INTERNAL_PASSTHROUGH`. Specifies the log format for the internal server that routes passthrough connections.
* `ROUTER_SERVICE_503_SERVER_PORT`. Specifies the port of the helper server, which serves 503 error pages.
* `ROUTER_SERVICE_PASSTHROUGH_PORT`. Specifies the port of the passthrough server -- the server, which handles both HTTPS and passthrough connections, before forwarding them to the Router internal helper servers. The default is `443` and equal to `ROUTER_SERVICE_HTTPS_PORT`. However, `ROUTER_SERVICE_PASSTHROUGH_PORT` and `ROUTER_SERVICE_HTTPS_PORT` can be different. In that case the server on the passthrough port will only handle passthrough connections and the server on the HTTPS port will only handle HTTPS connections.
* `KEEPALIVE_REQUESTS`. Specifies the maximum number of requests NGINX can serve through a single keep-alive connection. The default value is `100`.
* `WORKER_PROCESSES`. Specifies the number of worker processes, and is used to help tune NGINX performance. To see the list of factors that influences the optimal value, see [our documentation](http://nginx.org/en/docs/ngx_core_module.html#worker_processes). It is considered a good start to set this value to the number of CPU cores of the node in which the router is running. Leaving this value to default will allow NGINX to try and autodetect it.
* `WORKER_CPU_AFFINITY`. Tied closely with `WORKER_PROCESSES`, this value binds worker processes to the sets of CPUs. Each CPU set is represented by a bitmask of allowed CPUs. See the [documentation](http://nginx.org/en/docs/ngx_core_module.html#worker_cpu_affinity) for examples of how to use it.
* `WORKER_RLIMIT_NOFILE`. Specifies the maximum number of open files for worker processes. The default value is `8192`, but it can be useful to increase this number when NGINX is handling a large number of connections.
* `ROUTER_ENABLE_UNSAFE_ANNOTATIONS` Enables the unsafe annotations. The unsafe annotations are not validated by the Router and could lead to invalid NGINX configuration. The default is `false`.

The NGINX Router with NGINX Plus offers the additional following environment variables:

* `ROUTER_HTTP_BALANCE_PARAMETERS`. Modifies the `ROUTER_LOAD_BALANCE_ALGORITHM` default environment variable and the `nginx.router.openshift.io/balance` annotation to specify load balancing functionality.
* `ROUTER_TCP_BALANCE_PARAMETERS`. Modifies the `ROUTER_TCP_BALANCE_SCHEME` default environment variable and the `nginx.router.openshift.io/balance` annotation to specify load balancing functionality.

For more information on specifying load balancing with `ROUTER_HTTP_BALANCE_PARAMETERS` and `ROUTER_TCP_BALANCE_PARAMETERS`, see the [Fine-Tuning Load Balancing with NGINX Plus](#fine-tuning-load-balancing-methods-with-nginx-plus) section.

## Annotations

The NGINX Router supports the following annotations:
* `nginx.router.openshift.io/balance`. Configures a load balancing method. The supported values are `round_robin`, `least_conn`, `random`, `random_two`, `ip_hash` and `least_time` (NGINX Plus). The default value is configured using `ROUTER_LOAD_BALANCE_ALGORITHM` or `ROUTER_TCP_BALANCE_SCHEME` environment variables. `random_two` defaults to `random two least_conn` in the NGINX configuration.
* `nginx.router.openshift.io/balance-parameters`. Specific to NGINX Plus. Modifies the load balancing algorithm to specify more precise functionality. See [Fine-Tuning Load Balancing with NGINX Plus](#fine-tuning-load-balancing-methods-with-nginx-plus) for more information on tuning NGINX load balancing.
* `nginx.router.openshift.io/keepalive`. Activates the cache for connections between NGINX and upstream servers. The default is `0`, which means the cache is not activated. Not applicable for passthrough routes. See http://nginx.org/en/docs/http/ngx_http_upstream_module.html#keepalive for more details.
* `nginx.router.openshift.io/websocket`. Enables Websocket. The default is `false`.
* `nginx.router.openshift.io/grpc`. Enables gRPC. The default is `false`.
* `nginx.router.openshift.io/proxy_ssl_name`. Specifies the server name for verifying the proxied server certificate. Only used when re-encryption is enabled. The default value is the host of the route.

The NGINX Router supports the following unsafe annotations:

* `unsafe.nginx.router.openshift.io/server-snippets`. Sets custom snippets in the server context of the generated NGINX config. If multiple routes are created for the same host and all have this annotation present, the annotation of the primary route will be used. The primary route is a route which name is the alphabetically last among all routes. If some routes have TLS termination enabled, the primary route is a route which name is the alphabetically last among all TLS-enabled routes. Not available for passthrough routes.
* `unsafe.nginx.router.openshift.io/location-snippets`. Sets custom snippets in the location context of the generated NGINX config. Not available for passthrough and TCP/UDP routes.

**Notes**: 
* The Router doesn't validate the unsafe annotations, which might lead to invalid NGINX configuration. Check the Router logs to makes sure that the annotations have been successfully applied.
* Requires the `ROUTER_ENABLE_UNSAFE_ANNOTATIONS` environment variable set to `true`.


Additional annotations are available in the [TCP/UDP Load Balancing Extension](#tcpudp-load-balancing-extension).

## Configuring Edge Termination

If edge termination is enabled for a host in one route, it will be enabled in all other routes that reference that host as well. Thus, you only need to configure edge termination in one route. This behavior is different from the default Router behavior, where you would have to enable edge termination in every route.

If you still want to configure edge termination in every route that reference a particular host, please note that for all those routes the Router will use edge termination settings, such as termination policy and the certificate, from a route with a name that is alphabetically last among those routes.

## TCP/UDP Load Balancing Extension

This section describes the configuration, features, and limitations of the TCP/UDP load balancing extension. The extension is specific to the NGINX Router and not supported by other routers. For a demonstration, see the [TCP/UDP Load Balancing](../examples/tcp-udp) example.

### Configuration

The NGINX Router supports TCP/UDP load balancing through the following annotations:
* `nginx.router.openshift.io/protocol`. Enables TCP or UDP load balancing. The accepted values are `tcp` or `udp`.
* `nginx.router.openshift.io/port`. Specifies the port for TCP and UDP load balancing for NGINX to listen on. Notes:
    *  The specified port is not checked against the ports specified in other routes. As a result, port conflicts between routes may occur. Make sure to use unique ports per route.
    *  You can use the same port in two routes, but only if one of them is a TCP route and the other one is a UDP route.
    *  Make sure to open the specified port in the firewall for the protocol specified in the nginx.router.openshift.io/protocol annotation on every node where the Router is running. You can use iptables or firewall-cmd. For example, to open port 5353 for TCP traffic, run:
        ```
        $ sudo iptables -I OS_FIREWALL_ALLOW -p tcp --dport 5353 -j ACCEPT
        ```
* `nginx.router.openshift.io/proxy_ssl_name`. Specifies the server name for verifying the proxied server certificate. Only used when re-encryption is enabled. The default value is the host of the route.
* `nginx.router.openshift.io/responses`. Specifies the number of datagrams expected by the proxied server in response to a client datagram. Only used in UDP load balancing.

For TCP/UDP load balancing, the host and path of the route are ignored. However, keep in mind that the router allows only a single hostname/path combination across all routes. This means that if two or more routes have the same hostname/path combination, the oldest route will “win” and the other ones will be ignored. To avoid such collisions for TCP/UDP load balancing, we suggest providing each route with a host following a specific format:

```
host: <port>.<protocol>.nginx.router.openshift.io
```
For example, `9003.tcp.nginx.router.openshift.io`. This will prevent hostname/path collisions as well as help to avoid port conflicts among routes.

Here is an example route for TCP load balancing from the [TCP/UDP Load Balancing](../examples/tcp-udp) example:
```
apiVersion: v1
kind: Route
metadata:
  name: tcp-route
  annotations:
    nginx.router.openshift.io/protocol: tcp
    nginx.router.openshift.io/port: "5353"
spec:
  host: 5353.tcp.nginx.router.openshift.io
  to:
    kind: Service
    name: coredns
  port:
    targetPort: 53
```

### Edge Termination and Re-encryption

Load balancing of secure TCP connections can be configured using the tls configuration option similar to HTTP routes:
* `edge`. Terminates SSL at the router and establishes an unencrypted TCP connection to the backend.
* `reencrypt`. Terminates SSL at the router and establishes a new encrypted TCP connection to the backend.

Here is an example route using the `reencrypt` SSL termination policy:
```
apiVersion: v1
kind: Route
metadata:
  name: tcp-reencrypt-route
  annotations:
    nginx.router.openshift.io/protocol: "tcp"
    nginx.router.openshift.io/port: "9003"
    nginx.router.openshift.io/proxy_ssl_name: "app.example.com"
spec:
  host: 9003.tcp.nginx.router.openshift.io
  to:
    kind: Service
    name: secure-app
  port:
    targetPort: 9003
  tls:
    termination: reencrypt
    key: |-
      -----BEGIN PRIVATE KEY-----
      [...]
      -----END PRIVATE KEY-----
    certificate: |-
      -----BEGIN CERTIFICATE-----
      [...]
      -----END CERTIFICATE-----
    destinationCACertificate: |-
      -----BEGIN CERTIFICATE-----
      [...]
      -----END CERTIFICATE-----
```


### Limitations

The NGINX Router doesn't allow load balancing TCP/UDP on standard HTTP ports, or any ports used for internal routing. The table below shows these ports along with the environment variables that could override them:

| Default Value | Environment Variable |
| ------------- | ------------- |
| 443 | ROUTER_SERVICE_HTTPS_PORT |
| 443 | ROUTER_SERVICE_PASSTHROUGH_PORT |
| 80 | ROUTER_SERVICE_HTTP_PORT |
| 10444 | ROUTER_SERVICE_SNI_PORT |
| 10445 | ROUTER_SERVICE_503_SERVER_PORT |
| 10446 | ROUTER_SERVICE_UNREACHABLE_PORT |
| 10447 | ROUTER_SERVICE_INTERNAL_PASSTHROUGH_PORT |
| 1936 | STATS_PORT |

## Fine-Tuning Load Balancing Methods with NGINX Plus

This section describes the allowed values for the HTTP, TCP/UDP, and passthrough upstream load balancing algorithms. These values are specific to the NGINX Plus Router and not supported by other routers.

When specifying the load balancing algorithm `least_time`, for example, the router will default to `least_time header` within the NGINX configuration. It is sometimes necessary, however, to specify exactly how an algorithm will act, perhaps with `least_time first_byte`. This section describes how that mechanism is configured within the NGINX Router with NGINX Plus.

### HTTP

HTTP load balancing is configured with the `nginx.router.openshift.io/balance` annotation but defaults to the `ROUTER_LOAD_BALANCE_ALGORITHM` environment variable. This algorithm can be adjusted to support more fine-tuned load balancing algorithms using the `nginx.router.openshift.io/balance-parameters`, which defaults to the `ROUTER_HTTP_BALANCE_PARAMETERS` environment variable.

* `random_two`: Supported values are `least_conn`, `least_time=header` and `least_time=last_byte`. Default value is `random two least_conn`.
* `least_time`: Supported values are `header`, `last_byte`, `header infight` and `last_byte inflight`. Default value is `least_time header`.

### TCP/UDP and Passthrough

TCP/UDP as well as passthrough load balancing is configured with the `nginx.router.openshift.io/balance` annotation but defaults to the `ROUTER_TCP_BALANCE_SCHEME` environment variable. This algorithm can be adjusted to support more fine-tuned load balancing algorithms using the `nginx.router.openshift.io/balance-parameters`, which defaults to the `ROUTER_TCP_BALANCE_PARAMETERS` environment variable.

* `random_two`: Supported values are `least_conn`, `least_time=connect`, `least_time=first_byte` and `least_time=last_byte`. Default value is `random two least_conn`.
* `least_time`: Supported values are `connect`, `first_byte`, `last_byte`, `connect inflight`, `first_byte inflight` and `last_byte inflight`. Default value is `least_time connect`.
