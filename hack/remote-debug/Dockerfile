FROM quay.io/fedora/fedora:40

# This is a build-time variable and must be present in the
# environment.
ARG GITHUB_USER

ARG CLEAN_DNF_CACHE=false
ARG DNF_UPDATE=false

# For example: https://github.com/frobware/haproxy-builds/raw/master/rhaos-4.17-rhel-9/haproxy28-2.8.10-1.rhaos4.17.el9.x86_64.rpm
ARG HAPROXY_RPMS

RUN INSTALL_PKGS="rsyslog procps-ng util-linux socat lsof strace rsync wget supervisor openssh-server" && \
    [ "$DNF_UPDATE" = "true" ] && dnf update -y || echo "Skipping dnf update" && \
    dnf install -y $INSTALL_PKGS $HAPROXY_RPMS && \
    mkdir -p /var/lib/haproxy/router/{certs,cacerts,whitelists} && \
    mkdir -p /var/lib/haproxy/{conf/.tmp,run,bin,log,mtls} && \
    touch /var/lib/haproxy/conf/{{os_http_be,os_edge_reencrypt_be,os_tcp_be,os_sni_passthrough,os_route_http_redirect,cert_config,os_wildcard_domain}.map,haproxy.config} && \
    setcap 'cap_net_bind_service=ep' /usr/sbin/haproxy && \
    chown -R :0 /var/lib/haproxy && \
    chmod -R g+w /var/lib/haproxy && \
    [ "$CLEAN_DNF_CACHE" = "true" ] && dnf clean all || echo "Skipping dnf clean all"

RUN mkdir -p /var/log/supervisor /etc/supervisor
COPY hack/remote-debug/supervisord.conf /etc/supervisor/supervisord.conf

RUN echo "AllowAgentForwarding no" >> /etc/ssh/sshd_config && \
    echo "PasswordAuthentication no" >> /etc/ssh/sshd_config && \
    echo "PermitRootLogin yes" >> /etc/ssh/sshd_config && \
    echo "Port 2222" >> /etc/ssh/sshd_config && \
    echo "Match User root" >> /etc/ssh/sshd_config && \
    echo "  AuthorizedKeysFile /etc/ssh/authorized_keys.d/%u" >> /etc/ssh/sshd_config && \
    ssh-keygen -A && \
    mkdir -p /etc/ssh/authorized_keys.d && \
    curl -s https://github.com/${GITHUB_USER}.keys -o /etc/ssh/authorized_keys.d/root && \
    chmod 755 /etc/ssh/authorized_keys.d && \
    chmod 444 /etc/ssh/authorized_keys.d/root

# Modify root's home directory and preserve bash login setups. When we
# ssh into the container this step ensures that the initial directory
# matches `oc rsh <pod>`.
RUN sed -i 's|^\(root:x:0:0:Super User:\)/root|\1/var/lib/haproxy/conf|' /etc/passwd && \
    ln -s /root/.bash* /var/lib/haproxy/conf

COPY images/router/haproxy/ /var/lib/haproxy/

# Ensure the Delve debugger version matches the one used by your IDE
# to avoid compatibility issues.
COPY dlv \
     hack/remote-debug/start-debugging \
     hack/remote-debug/remote-debug-helper \
     openshift-router /usr/bin/

LABEL io.k8s.display-name="OpenShift HAProxy Router" \
      io.k8s.description="This component offers ingress to an OpenShift cluster via Ingress and Route rules." \
      io.openshift.tags="openshift,router,haproxy"

USER 1001
EXPOSE 80 443
WORKDIR /var/lib/haproxy/conf
ENV XDG_CONFIG_HOME=/tmp \
    TEMPLATE_FILE=/var/lib/haproxy/conf/haproxy-config.template \
    RELOAD_SCRIPT=/var/lib/haproxy/reload-haproxy
ENTRYPOINT ["/usr/bin/supervisord", "-c", "/etc/supervisor/supervisord.conf"]
