#!/bin/bash
set -e

if [[ "$1" == "" ]]; then
  echo "ERROR: You must provide a task ID"
  exit 1
fi
taskID=$1

# Make sure we are at the top level dir in the git repo.
cd $(git rev-parse --show-toplevel)

# Download x86_64 and src RPMs
mkdir -p $taskID
cd $taskID
brew download-task $taskID --skip="debug[source|info]-*" --arch=x86_64 #--arch=src
cd -

rpmPath=./*.x86_64.rpm
rpmName=$(basename ${rpmPath})

echo "Building scratch RPM image"
podman build --build-arg=RPM=${rpmName} -t quay.io/openshift/network-edge-testing:router-scratch-rpm-$rpmName -f- ./${taskID} <<EOF
FROM scratch
ARG RPM
COPY haproxy26-2.6.13-4.rhaos4.14.el8.x86_64.rpm /
EOF

echo "Pushing scratch RPM image"
podman push quay.io/openshift/network-edge-testing:router-scratch-rpm-$rpmName

# Clean up.
rm ${taskID} -fr

# Make a file for review with the PR link in it, just for the reviewer's convenience.
echo "https://brewweb.engineering.redhat.com/brew/taskinfo?taskID=${taskID}" > brewtask.txt

echo "Don't forget to include the git commit patch diff in the distgit HAProxy repo via `git format-patch -1 --subject-prefix=""`"
