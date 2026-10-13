package version

import "fmt"

var (
	// commitFromGit is a constant representing the source version that
	// generated this build. It should be set during build via -ldflags.
	commitFromGit string
	// versionFromGit is a constant representing the version tag that
	// generated this build. It should be set during build via -ldflags.
	versionFromGit = "unknown"
	// major version
	majorFromGit string
	// minor version
	minorFromGit string
	// build date in ISO8601 format, output of $(date -u +'%Y-%m-%dT%H:%M:%SZ')
	buildDate string
	// state of git tree, either "clean" or "dirty"
	gitTreeState string
)

func String() string {
	return fmt.Sprintf("majorFromGit: %s\nminorFromGit: %s\ncommitFromGit: %s\nversionFromGit: %s\ngitTreeState: %s\nbuildDate: %s\n",
		majorFromGit, minorFromGit, commitFromGit, versionFromGit, gitTreeState, buildDate)
}
