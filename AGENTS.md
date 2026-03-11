# Shell commands

- `make`, `make all`, or `make build`: Build the `openshift-router` executable.
- `make check`: Run unit tests for `openshift-router`.
- `make verify`: Verify that source code is properly formatted and that Go mod dependencies are correct.

Note that there is no make target for generated files.  Also, this repository
does not have any integration or end-to-end tests; see the [openshift/origin](https://github.com/openshift/origin/tree/master/test/extended/router/)
or [openshift/cluster-ingress-operator](https://github.com/openshift/cluster-ingress-operator/tree/master/test/e2e/) repositories for those.

See also [the `HACKING.md` file](./HACKING.md) in this repository for some more advanced
commands for building and testing images.

# Code style

- Format code using gofmt.
- Add godoc for all package-level identifiers.  Use your discretion, but when in doubt, add godoc.
- Write unit tests for new functionality.  Use the standard Go testing package; we don't use Ginkgo or other testing frameworks in this repository.
- Add code comments as appropriate, using complete English sentences.  Be kind to your fellow developers, including future you.
- In the absence of OpenShift-specific conventions, refer to the [Kubernetes Coding Conventions](https://www.kubernetes.dev/docs/guide/coding-convention/).  Use your discretion; when in doubt, ask.

# Workflow

- Use commits to group changes logically.  For example, sometimes it makes sense to do some refactoring in one commit, and then implement your defect fix or feature in a separate commit.
- Ensure each commit passes `make verify check`.
- Follow the [OpenShift](https://github.com/openshift/enhancements/blob/master/guidelines/commit_and_pr_text.md) and [Kubernetes commit message guidelines](https://www.kubernetes.dev/docs/guide/pull-requests/#commit-message-guidelines).
- If your code change requires `go mod` updates, commit the `go mod` updates (including `vendor/`) and document them in your commit message.  Put your code changes and `go mod` updates in the same commit so that they can be reviewed and tested (using `make verify`) as a unit.
- Rebase in case of merge conflicts.  Please don't have merge commits in your pull request.
- Please be sure to respond to PR review comments.  If you accept a suggestion, say so.  If you need clarification, ask.  If you reject a suggestion, explain why; don't be shy, but do be respectful and forthright.
