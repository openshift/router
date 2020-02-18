# How to build the haproxy RPM for OpenShift 4.4+

In OpenShift 4.4 we started building our own haproxy RPM, named
haproxy20.

## Prerequisites

- You need to be a member of the devel group:

```
[amcdermo@file01 ~]$ groups
amcdermo devel aos-qe-installer aos-devaccess aos-tier3-limited
```

- You'll need to install rhpkg(1) and that comes from the rcm-tools repo:

```
$ cat /etc/yum.repos.d/rcm-tools-fedora.repo
[rcm-tools-fedora-rpms]
name=RCM Tools for Fedora $releasever (RPMs)
baseurl=https://download.devel.redhat.com/rel-eng/RCMTOOLS/latest-RCMTOOLS-2-F-$releasever/compose/Everything/$basearch/os/
enabled=0
gpgcheck=1
gpgkey=https://download.devel.redhat.com/rel-eng/RCMTOOLS/RPM-GPG-KEY-rcminternal
```

Further information on rhpkg can be found in the Mojo page:

  https://mojo.redhat.com/docs/DOC-999615des

## rhpkg Mojo documentation / tutorials

- Dist-git/rhpkg tutorial (https://mojo.redhat.com/docs/DOC-1045187)
- rhpkg documentation (https://mojo.redhat.com/docs/DOC-999615)

## Where is the source repository?

The source repository is here:

  https://pkgs.devel.redhat.com/cgit/rpms/haproxy/

We have the following branches setup for OpenShift 4.4:

- https://pkgs.devel.redhat.com/cgit/rpms/haproxy/log/?h=rhaos-4.4-rhel-7
- https://pkgs.devel.redhat.com/cgit/rpms/haproxy/log/?h=rhaos-4.4-rhel-8

I have been committing to both repos every time I have changed any
aspect of the packaging and that means we have RPMs built for RHEL-7
and RHEL-8.

## Cloning the repository

Ensure you have active kerberos credentials:

```
$ kinit amcdermo
Password for amcdermo@IPA.REDHAT.COM:

$ klist
Ticket cache: KEYRING:persistent:1000:1000
Default principal: amcdermo@IPA.REDHAT.COM

Valid starting     Expires            Service principal
18/02/20 10:08:00  18/02/20 20:08:00  krbtgt/IPA.REDHAT.COM@IPA.REDHAT.COM
	renew until 19/02/20 10:07:22
```

Use the `rhpkg` utility to clone:

```
$ rhpkg clone haproxy
Cloning into 'haproxy'...
amcdermo@pkgs.devel.redhat.com's password: <kerberos-password>
remote: Counting objects: 1068, done.
remote: Compressing objects: 100% (780/780), done.
remote: Total 1068 (delta 547), reused 502 (delta 256)
Receiving objects: 100% (1068/1068), 184.53 KiB | 755.00 KiB/s, done.
Resolving deltas: 100% (547/547), done.
```

At this point I think it is easier to follow the tutorial for
building/pushing/etc already documented here:

  https://mojo.redhat.com/docs/DOC-1045187.

## Finding existing pre-built RPMs

https://brewweb.engineering.redhat.com/brew/packageinfo?packageID=73301

That link contains builds for haproxy20 -- our package name for
OpenShift 4.x. Follow the links for RHEL 7 & 8 for RPM packages.

## RPMs end up in an OpenShift-consumable puddle:

http://download.eng.bos.redhat.com/rcm-guest/puddles/RHAOS/AtomicOpenShift/4.4/latest/x86_64/os/Packages/

And in there we can see the build I did yesterday:

  haproxy20-2.0.13-2.el7.x86_64.rpm 17-Feb-2020 12:31  1.5M

We consume this RPM by name as `haproxy20` in the openshift-router
Dockerfile:

  https://github.com/openshift/router/blob/master/images/router/haproxy/Dockerfile

## How to produce and consume a scratch build

Let's say you have made a change and you want to consume and test that
before committing to the change:

```
$ cd ~/haproxy

$ git remote update
Fetching origin
amcdermo@pkgs.devel.redhat.com's password: <kerberos-password>

$ git checkout rhaos-4.4-rhel-7

$ rpmdev-bumpspec -c 'Add optimisation patch' haproxy.spec

$ git add 0001-OPTIM-startup-fast-unique_id-allocation-for-acl.patch
```

$EDIT `haproxy.spec` and these hunks:

```diff
@@ -18,6 +18,8 @@ License:        GPLv2+
 URL:            http://www.haproxy.org/
 Source0:        http://www.haproxy.org/download/2.0/src/haproxy-%{version}.tar.gz

+Patch0:                0001-OPTIM-startup-fast-unique_id-allocation-for-acl.patch
+
 BuildRequires:  openssl-devel
 BuildRequires:  pcre-devel
 BuildRequires:  zlib-devel
@@ -48,6 +50,7 @@ availability environments. Indeed, it can:

 %prep
 %setup -q
+%patch0 -p1

 %build
 regparm_opts=

Patch0:		bz1664533-fix-handling-priority-flag-HTTP2-decoder.patch
```

```
$ git status
On branch rhaos-4.4-rhel-7
Your branch is up to date with 'origin/rhaos-4.4-rhel-7'.

Changes to be committed:
  (use "git reset HEAD <file>..." to unstage)

	new file:   0001-OPTIM-startup-fast-unique_id-allocation-for-acl.patch

Changes not staged for commit:
  (use "git add <file>..." to update what will be committed)
  (use "git checkout -- <file>..." to discard changes in working directory)

	modified:   haproxy.spec

$ git commit -am 'Add optimisation patch'
```

Create a new source RPM for our scratch build; this will also contain
our new patch file.

```
$ rhpkg srpm
Wrote: /home/aim/haproxy/haproxy-2.0.13-3.el7.src.rpm
```

Start a scatch build:

```
$ rhpkg scratch-build --srpm haproxy-2.0.13-3.el7.src.rpm
[====================================] 100% 00:00:03   2.53 MiB 684.64 KiB/sec
Building haproxy-2.0.13-3.el7.src.rpm for rhaos-4.4-rhel-7-candidate
Created task: 26584597
Task info: https://brewweb.engineering.redhat.com/brew/taskinfo?taskID=26584597
Watching tasks (this may be safely interrupted)...
```

Once that completes you can follow the "Task info" URL to find the RPM
files. Copy this scratch build RPM somewhere public so that CI can
consume it. Now change the openshift/router Dockerfile(s) to
explicitly consume that RPM.

For example, see this existing PR:

  https://github.com/openshift/router/pull/89

The difference is that we consume the RPM from somewhere explicit.

```diff
commit e9ace411eeb68d8fe410cc8db2f3940eac51fc7b
Author: Andrew McDermott <amcdermo@redhat.com>
Date:   Fri Feb 14 15:06:49 2020 +0000

    [WIP] DO NOT MERGE - 4.4: test/validate haproxy20-optimized-2.0.13-1.el7.x86_64.rpm
    
    This is 2.0.13 plus this WIP patch:
    
      https://github.com/chlunde/haproxy/commit/2fccb7d8d7b31ee795725fa883f2167707705490
    
    which speeds up parsing the configuration file.

diff --git a/images/router/haproxy/Dockerfile b/images/router/haproxy/Dockerfile
index 1a646b1..c184558 100644
--- a/images/router/haproxy/Dockerfile
+++ b/images/router/haproxy/Dockerfile
@@ -1,5 +1,9 @@
 FROM registry.svc.ci.openshift.org/openshift/origin-v4.0:base-router
-RUN INSTALL_PKGS="haproxy20 rsyslog sysvinit-tools" && \
+
+RUN yum install -y https://github.com/frobware/haproxy-hacks/raw/master/RPMs/haproxy20-optimized-2.0.13-1.el7.x86_64.rpm
+RUN haproxy -vv
+
+RUN INSTALL_PKGS="rsyslog sysvinit-tools" && \
     yum install -y $INSTALL_PKGS && \
     rpm -V $INSTALL_PKGS && \
     yum clean all && \
diff --git a/images/router/haproxy/Dockerfile.rhel b/images/router/haproxy/Dockerfile.rhel
index 7d34ba4..ca1ae4c 100644
--- a/images/router/haproxy/Dockerfile.rhel
+++ b/images/router/haproxy/Dockerfile.rhel
@@ -1,5 +1,9 @@
 FROM registry.svc.ci.openshift.org/ocp/4.0:base-router
-RUN INSTALL_PKGS="haproxy20 rsyslog sysvinit-tools" && \
+
+RUN yum install -y https://github.com/frobware/haproxy-hacks/raw/master/RPMs/haproxy20-optimized-2.0.13-1.el7.x86_64.rpm
+RUN haproxy -vv
+
+RUN INSTALL_PKGS="rsyslog sysvinit-tools" && \
     yum install -y $INSTALL_PKGS && \
     rpm -V $INSTALL_PKGS && \
     yum clean all && \
```

If you're happy with the RPM and want to commit to it then:

```
$ rhpkg push
$ rhpkg build
```
