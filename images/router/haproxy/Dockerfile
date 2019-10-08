FROM registry.svc.ci.openshift.org/openshift/origin-v4.0:base AS builder
RUN INSTALL_PKGS="git ca-certificates gcc libc6-dev liblua5.3-dev libpcre3-dev libssl-dev libsystemd-dev make wget zlib1g-dev" && \
      yum install -y $INSTALL_PKGS && \
      rpm -V INSTALL_PKGS && \
      yum clean all && \
      git clone --single-branch --branch v2.0.0 https://github.com/haproxy/haproxy.git && \
      cd haproxy && \
      make TARGET=linux-glibc USE_LUA=1 USE_OPENSSL=1 USE_PCRE=1 USE_ZLIB=1 USE_SYSTEMD=1 EXTRA_OBJS="contrib/prometheus-exporter/service-prometheus.o" && \

FROM registry.svc.ci.openshift.org/openshift/origin-v4.0:base-router
COPY --from=builder /haproxy/haproxy /usr/sbin/
RUN INSTALL_PKGS="rsyslog sysvinit-tools" && \
    yum install -y $INSTALL_PKGS && \
    rpm -V $INSTALL_PKGS && \
    yum clean all && \
    mkdir -p /var/lib/haproxy/router/{certs,cacerts,whitelists} && \
    mkdir -p /var/lib/haproxy/{conf/.tmp,run,bin,log} && \
    touch /var/lib/haproxy/conf/{{os_http_be,os_edge_reencrypt_be,os_tcp_be,os_sni_passthrough,os_route_http_redirect,cert_config,os_wildcard_domain}.map,haproxy.config} && \
    setcap 'cap_net_bind_service=ep' /usr/sbin/haproxy && \
    chown -R :0 /var/lib/haproxy && \
    chmod -R g+w /var/lib/haproxy
COPY images/router/haproxy/* /var/lib/haproxy/
LABEL io.k8s.display-name="OpenShift HAProxy Router" \
      io.k8s.description="This component offers ingress to an OpenShift cluster via Ingress and Route rules." \
      io.openshift.tags="openshift,router,haproxy"
USER 1001
EXPOSE 80 443
WORKDIR /var/lib/haproxy/conf
ENV TEMPLATE_FILE=/var/lib/haproxy/conf/haproxy-config.template \
    RELOAD_SCRIPT=/var/lib/haproxy/reload-haproxy
ENTRYPOINT ["/usr/bin/openshift-router"]
