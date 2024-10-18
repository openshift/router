# Debug Development Environment

This directory provides a container-based development environment
designed to streamline the workflow for debugging the
`openshift-router` process. It primarily contains a Dockerfile and
some helper scripts and utilities. The Dockerfile produces an image
that allows you to rapidly redeploy the `openshift-router` binary
without needing to build a new image each time you want to debug an
edit/compile iteration.

## Rationale

Traditionally, iterating on edit/compile/debug is slow because it
involves building a new image, pushing the image, and waiting for the
roll out to complete. This new environment provides a one-time deploy
of the debug image. Subsequent iterations only require editing and
compiling as before. The debugging step just copies a new binary to
the pod and starts executing immediately, eliminating the need to wait
for container builds, tagging an image, pushing an image, setting a
new image on the deployment, and waiting for new pods to roll out.

## Overview

The debug image runs `supervisord` as the init system. `supervisord`
launches `remote-debug-helper`, which performs the following tasks:

1. Discovers all environment variables associated with the
   `router-default` deployment.

2. Writes a file named `/etc/profile.d/router-default.sh` that contains all of
   `router-default`'s environment variables, ensuring arbitrary SSH
   login shells get an identical environment as with `oc rsh`.

3. Starts `sshd` for SSH access into the pod for development and
   debugging.

This setup allows you to run the `openshift-router` binary without
specifying command line flags or environment variables, ensuring a
friction-free invocation that yields an identical environment as a
non-debug-based Dockerfile.

## Preparatory Steps

Before you begin, there are several preparatory steps you need to
follow to set up your development environment.

### 1. Obtain the `dlv` Binary

    The `dlv` (Delve debugger) executable needs to be located in the
    top-level directory as it is copied into the image during the
    container build process. Use the provided helper script to find and
    copy the `dlv` binary from a GoLand installation:

    ```sh
    $ ./hack/remote-debug/find-and-copy-dlv-from-goland-installation
    ```

    If you don't have GoLand installed, you can install `dlv` via:

    ```sh
    $ sudo dnf install -y /usr/bin/dlv
    ```

    Ensure the `dlv` debugger version installed by `dnf` matches the one
    used by your $IDE to avoid compatibility issues.

### 2. Set Environment Variables for image build and SSH access

    You need to specify several environment variables for the build and
    deployment processes. These variables can be set in a `.envrc` file in
    the top-level directory.

    ```sh
    export REGISTRY=quay.io
    export IMAGE=amcdermo/openshift-router
    export DOCKERFILE_REMOTE_DEBUG=hack/remote-debug/Dockerfile
    ```

You can either source the `.envrc` file manually with:
```sh
$ source .envrc
```
or, if using [direnv](https://direnv.net/), the environment variables
will automatically load when you navigate to the directory.

# Building and deploying the debug image

1. **Build `openshift-router` and `remote-debug-helper`**:

    To build the image, ensure you have at least the HAProxy binary
    RPMs in the current directory (which is the Docker/Podman build
    context). These RPMs can be obtained via brew (RH internal build
    system). The image build process will automatically copy all
    haproxy*.rpm files, both source and binary RPMs, and install them
    appropriately.

   ```sh
   % ls -l *.rpm
   -rw-r--r-- 1 aim users 4402838 Oct 18 10:34 haproxy-2.8.10-1.rhaos4.17.el9.src.rpm
   -rw-r--r-- 1 aim users 2460520 Oct 18 10:34 haproxy28-2.8.10-1.rhaos4.17.el9.x86_64.rpm
   -rw-r--r-- 1 aim users 3967020 Oct 18 10:34 haproxy28-debuginfo-2.8.10-1.rhaos4.17.el9.x86_64.rpm
   -rw-r--r-- 1 aim users 1418293 Oct 18 10:34 haproxy-debugsource-2.8.10-1.rhaos4.17.el9.x86_64.rpm
   ```

   Navigate to the top-level directory of your
   [openshift-router](https://github.com/openshift/router) clone and
   run the following command to compile the `openshift-router` and
   `remote-debug-helper` binaries:

    ```sh
    $ make -f hack/Makefile.debug build-image
    ```

2. **Push the image**:

    ```sh
    $ make -f hack/Makefile.debug push-image
    ```

3. **Set the image**:

    Once the debug image is built and pushed to the registry, you need
    to update the router deployment to use this new image. This
    indirectly involves several steps to ensure that the custom debug
    image is used correctly and that the router deployment can be
    easily debugged.

    ```sh
    $ make -f hack/Makefile.debug set-image
    ```

# SSH & Debugger pod Access

After deploying the debug image, you can access the container via SSH.
This can be particularly useful for debugging and inspecting the
running environment.

## Preparatory Steps

1. Install Port Forward Services:

    ```sh
    $ make -f hack/Makefile.debug install-port-forward-services
    ```

    You can later uninstall the port forwarding services with:

    ```sh
    $ make -f hack/Makefile.debug uninstall-port-forward-services
    ```

2. Start or Restart Port Forward Services:

    This command will start or restart the systemd services that
    forward local ports to the container's ports. By default, the port
    forward services map local port 2222 to the container's SSH port.
    Port 7000 is used for the Delve debugger. You may have to adjust
    the port numbers defined in the service files if you are already
    using port 2222 and/or 7000.

    ```sh
    $ make -f hack/Makefile.debug restart-port-forward-services
    oc-port-forward-SSH.service is running.
    oc-port-forward-delve.service is running.
    ```

    For more detail, inspect the status using systemctl:

    ```sh
    $ systemctl status --no-pager --user oc-port-forward-SSH.service
    ● oc-port-forward-SSH.service - Persistent oc port-forward service for port 2222 (SSH)
    Loaded: loaded (/home/aim/.config/systemd/user/oc-port-forward-SSH.service; enabled; preset: enabled)
         Active: active (running) since Thu 2024-06-06 09:49:02 BST; 1min 5s ago
       Main PID: 1010750 (oc)
          Tasks: 19 (limit: 153094)
         Memory: 25.0M
            CPU: 107ms
         CGroup: /user.slice/user-1000.slice/user@1000.service/app.slice/oc-port-forward-SSH.service
                 └─1010750 /home/aim/bin/oc port-forward -n openshift-ingress deployment/router-default --address 127.0.0.1 2…

    Jun 06 09:49:02 spicy systemd[9432]: Started Persistent oc port-forward service for port 2222 (SSH).
    Jun 06 09:49:02 spicy oc[1010750]: Forwarding from 127.0.0.1:2222 -> 2222
    Jun 06 09:49:05 spicy oc[1010750]: Handling connection for 2222
    ```

    We can now ssh to the container:

    ```sh
    $ ssh -F hack/remote-debug/ssh_config container
    Last login: Thu Jun  6 08:49:05 2024 from ::1
    [root@worker-0 ~]# whoami
    root

    [root@worker-0 ~]# type openshift-router
    openshift-router is /usr/bin/openshift-router

    [root@worker-0 ~]# type start-debugging
    start-debugging is /usr/bin/start-debugging
    ```

# Debugging Workflows (the Ultimate Goal)

The primary objective of this setup is to streamline the debugging
workflow for the `openshift-router` process. By utilising the custom
debug image and SSH access, you can rapidly iterate on your code
changes without the need for rebuilding and redeploying the entire
image each time.

However, if you prefer not to attach a debugger, you can run the
`openshift-router` directly without any ceremony, as the image and the
SSH session will have all the environment variables defined up-front.
A friction-free invocation is:

```sh
[root@worker-0 ~]# /usr/bin/openshift-router
I0606 09:02:29.108050      75 template.go:560] "msg"="starting router" "logger"="router" "version"="majorFromGit: \nminorFromGit: \ncommitFromGit: \nversionFromGit: unknown\ngitTreeState: \nbuildDate: \n"
I0606 09:02:29.112747      75 metrics.go:156] "msg"="router health and metrics port listening on HTTP and HTTPS" "address"="0.0.0.0:1936" "logger"="metrics"
I0606 09:02:29.117731      75 router.go:372] "msg"="watching for changes" "logger"="template" "path"="/etc/pki/tls/private"
I0606 09:02:29.117791      75 router.go:293] "msg"="initializing dynamic config manager ... " "logger"="template"
I0606 09:02:29.117870      75 manager.go:719] "msg"="provisioning blueprint route pool" "logger"="manager" "name"="_blueprint-http-route" "namespace"="_hapcm_blueprint_pool" "size"=10
I0606 09:02:29.118044      75 manager.go:719] "msg"="provisioning blueprint route pool" "logger"="manager" "name"="_blueprint-edge-route" "namespace"="_hapcm_blueprint_pool" "size"=10
I0606 09:02:29.118152      75 manager.go:719] "msg"="provisioning blueprint route pool" "logger"="manager" "name"="_blueprint-passthrough-route" "namespace"="_hapcm_blueprint_pool" "size"=10
I0606 09:02:29.118254      75 router.go:282] "msg"="router is including routes in all namespaces" "logger"="router"
E0606 09:02:29.239263      75 haproxy.go:418] can't scrape HAProxy: dial unix /var/lib/haproxy/run/haproxy.sock: connect: no such file or directory
I0606 09:02:29.277656      75 router.go:669] "msg"="router reloaded" "logger"="template" "output"=" - Checking http://localhost:80 ...\n - Health check ok : 0 retry attempt(s).\n"
...
```

Alternatively, you can invoke /usr/bin/start-debugging, which will run
the `openshift-router` process under the auspices of dlv (Delve):

```sh
[root@worker-0 ~]# bash -x /usr/bin/start-debugging
+ set -o errexit
+ set -o nounset
+ dlv --listen=:7000 --api-version=2 --headless=true --accept-multiclient exec /usr/bin/openshift-router
+ tee /proc/1/fd/1
API server listening at: [::]:7000
2024-06-06T09:09:39Z warning layer=rpc Listening for remote connections (connections are not authenticated nor encrypted)
```

The debugger is now running and waiting for a remote connection. You
can connect to this debugging session using a compatible IDE, such as
GoLand, by configuring it to connect to port 7000. For more details on
how to attach to running Go processes with the GoLand debugger, refer
to this
[guide](https://www.jetbrains.com/help/go/attach-to-running-go-processes-with-debugger.html).

You can pass the `--continue` flag to `start-debugging` which starts
the process immediately, and without halting for a debugger to attach.
This setup ensures that the router is operational from the get-go, yet
still allows for remote debugging. You can attach from your IDE at any
time after launch to begin debugging.

## Iterative Development

With the preparatory steps completed and the debug environment set up,
you can now enter the iterative cycle of editing, compiling, and
debugging. This workflow allows you to rapidly test changes without
the overhead of rebuilding and redeploying container images each time.
Welcome to the Happy Path!

### Happy Path Workflow

1. **Edit Your Code**:

   Make the necessary changes to your `openshift-router` code. This
   could involve fixing bugs, adding features, or making improvements.

2. **Compile the openshift-router Binary**:

   After editing your code, compile the `openshift-router` binary
   locally:

    ```sh
    $ make build
    ```

3. **Sync the Binary to the Container**:

   Use rsync to copy the newly compiled binary to the running
   container. This ensures that the latest version of your code is
   being executed without the need to rebuild the container image:

    ```sh
    $ make -f hack/Makefile.debug rsync-openshift-router
    ```

4. **Start Debugging your changes**:

    You can now start debugging again with:

    ```sh
    $ ssh -F hack/remote-debug/ssh_config container
    Last login: Thu Jun  6 08:53:17 2024 from ::1

    [root@worker-0 ~]# /usr/bin/start-debugging --continue
    API server listening at: [::]:7000

    # Or...

    [root@worker-0 ~]# /usr/bin/start-debugging
    API server listening at: [::]:7000
    ```

You can simplify the previous steps using the `debug` Makefile target.
This target combines steps 1, 2, and 3, running the build, syncing the
new binary, and starting the debugging session in one step:

```sh
$ make -f hack/Makefile.debug debug
GO111MODULE=on CGO_ENABLED=0 GOFLAGS=-mod=vendor go build -o openshift-router -gcflags=all="-N -l" ./cmd/openshift-router
GO111MODULE=on CGO_ENABLED=0 GOFLAGS=-mod=vendor go build -o hack/remote-debug/remote-debug-helper -gcflags=all="-N -l" ./hack/remote-debug/remote-debug-helper.go
rsync --progress -z -e 'ssh -F hack/remote-debug/ssh/config' ./openshift-router container:/usr/bin/openshift-router
openshift-router
     82,512,838 100%  203.29MB/s    0:00:00 (xfr#1, to-chk=0/1)
ssh -t -F hack/remote-debug/ssh/config container /usr/bin/start-debugging --continue
API server listening at: [::]:7000
2024-06-06T09:33:44Z warning layer=rpc Listening for remote connections (connections are not authenticated nor encrypted)
I0606 09:33:45.873085     234 template.go:560] "msg"="starting router" "logger"="router" "version"="majorFromGit: \nminorFromGit: \ncommitFromGit: \nversionFromGit: unknown\ngitTreeState: \nbuildDate: \n"
I0606 09:33:45.876269     234 metrics.go:156] "msg"="router health and metrics port listening on HTTP and HTTPS" "address"="0.0.0.0:1936" "logger"="metrics"
I0606 09:33:45.882477     234 router.go:372] "msg"="watching for changes" "logger"="template" "path"="/etc/pki/tls/private"
I0606 09:33:45.882543     234 router.go:293] "msg"="initializing dynamic config manager ... " "logger"="template"
...
```

**Note:** When you start the debugging session with the `debug`
Makefile target, it includes Delve's `--continue` flag so that the
debuggee (i.e., the router) starts immediately.

If you want to start debugging in `main()` or any other function
before the router starts, you should suppress the `--continue` flag
with:

```sh
$ make -f hack/Makefile.debug debug DLV_CONTINUE=
GO111MODULE=on CGO_ENABLED=0 GOFLAGS=-mod=vendor go build -o openshift-router -gcflags=all="-N -l" ./cmd/openshift-router
GO111MODULE=on CGO_ENABLED=0 GOFLAGS=-mod=vendor go build -o hack/remote-debug/remote-debug-helper -gcflags=all="-N -l" ./hack/remote-debug/remote-debug-helper.go
rsync --progress -z -e 'ssh -F hack/remote-debug/ssh/config' ./openshift-router container:/usr/bin/openshift-router
openshift-router
     82,512,838 100%    4.27GB/s    0:00:00 (xfr#1, to-chk=0/1)
ssh -t -F hack/remote-debug/ssh/config container /usr/bin/start-debug
```

Here, dlv is waiting for a connection from the debugger, which allows
you to add breakpoints before the `openshift-router` process starts.

By following the `make debug` happy path, you can achieve faster
iteration, making the development and debugging process smoother and
more enjoyable.

## Warts!

If you interrupt either `dlv` or the `make debug` invocation with
Ctrl-C (i.e., SIGINT) in the terminal, the `dlv` process does not
terminate. As a workaround, you can run the following command in
another shell to kill the `dlv` process so that you can continue down
the happy path:

```sh
$ ssh -F hack/remote-debug/ssh_config container pkill dlv
```

# Router Logs

When the `openshift-router` is not PID 1 in the container, you won't
see any output from the router using `oc logs -n openshift-ingress
<pod>` if you just run `openshift-router` on its own.

The issue is that the output from any process started directly will
only go to the terminal. This can be inconvenient if you want to use
`oc logs`. To address this, you can redirect the output to
`/proc/1/fd/1`, which will allow you to use `oc logs` to view the
logs.

The `start-debugging` script already includes this redirection for
convenience, allowing you to view the router's logs via `oc logs`.

### Viewing Logs

To view the router's logs, you can also use the
`hack/remote-debug/tail-router-default-logs` script. This script
continuously tails the logs of the router pod and will restart the
tail process if the pod gets deleted (e.g., due to a new image
update).

This script repeatedly tails the logs and restarts the tail process if
the pod gets deleted, ensuring you always have access to the latest
logs.

You can leave this script running in another terminal, which could be
a terminal view in your IDE. That way, when you're debugging in the
IDE, you can see the code, the debugger, and any output from the
router simultaneously.

# Redeploying a New Debug Image

If you need to change the Dockerfile for the debug image, there’s an
express path for building, pushing the image, and setting the new
image.

Make your changes (e.g., add new packages, change the debug helper
scripts, etc.), then run:

```sh
$ make -f hack/Makefile.debug deploy-image
```

This will combine the steps outlined earlier, which are:
- **`build`**: Compile the openshift-router and remote-debug-helper binaries
- **`build-image`**: Build the new image
- **`push-image`**: Push the new image to the specified container registry
- **`install-port-forward-services`**: Install the port-forward systemd services
- **`set-image`**: Update the router deployment to use the new image

By using the `deploy-image` target, you can quickly redeploy a new
debug image with all necessary changes, streamlining the development
and debugging process.

# Reverting a Debug Image Deployment

```sh
$ make -f hack/Makefile.debug reset
```
