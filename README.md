openshift-router
================

This repository contains the OpenShift routers for NGINX, HAProxy, and F5. They read `Route` objects out of the
OpenShift API and allow ingress to services. HAProxy is currently the reference implementation. See the details
in each router image.

These images are managed by the `cluster-ingress-operator` in an OpenShift 4.0+ cluster.

The template router code (`openshift-router`) is generic and creates config files on disk based on the state
of the cluster. The process launches proxies as children and triggers reloads as necessary after new config
has been written. The standard logic for handling conflicting routes, supporting wildcards, reporting status
back to the Route object, and metrics live in the standard process.


Deploying to Kubernetes
-----------------------

The OpenShift router can be run against a vanilla Kubernetes cluster, although some of the security protections
present in the API are not possible with CRDs.

To deploy, clone this repository and then run:

    $ kubectl create -f deploy/

You will then be able to create a `Route` that points to a service on your cluster and the router pod will
forward your traffic from port 80 to your service endpoints.  You can run the example like:

    $ kubectl create -f example/

And access the router via the node it is located on. If you're running locally on minikube or another solution,
just run:

    $ curl http://localhost -H "Host: example.local"

to see your route and:

    $ kubectl get routes

to see details of your routes.