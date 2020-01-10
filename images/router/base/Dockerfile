FROM registry.svc.ci.openshift.org/openshift/release:golang-1.12 AS builder
WORKDIR /go/src/github.com/openshift/router
COPY . .
RUN make
COPY catatonit-0.1.4.tar.gz .
RUN INSTALL_DEV_PKGS="automake libtool" && \
	yum install -y $INSTALL_DEV_PKGS && \
	tar xvzf catatonit-0.1.4.tar.gz && \
	cd catatonit-0.1.4 && \
	autoreconf -fvi && ./configure && make && \
	pwd && \
	ls -lR && \
	yum remove -y $INSTALL_DEV_PKGS
FROM registry.svc.ci.openshift.org/openshift/origin-v4.0:base
COPY --from=builder /go/src/github.com/openshift/router/openshift-router /usr/bin/
COPY --from=builder /go/src/github.com/openshift/router/catatonit-0.1.4/catatonit /usr/bin/
LABEL io.k8s.display-name="OpenShift Router" \
      io.k8s.description="This is the base image from which all template based routers inherit." \
      io.openshift.tags="openshift,router"
