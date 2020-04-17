FROM registry.svc.ci.openshift.org/openshift/release:golang-1.13 AS builder
WORKDIR /go/src/github.com/openshift/router
COPY . .
RUN make

FROM registry.svc.ci.openshift.org/openshift/origin-v4.0:base
COPY --from=builder /go/src/github.com/openshift/router/openshift-router /usr/bin/
LABEL io.k8s.display-name="OpenShift Router" \
      io.k8s.description="This is the base image from which all template based routers inherit." \
      io.openshift.tags="openshift,router"
