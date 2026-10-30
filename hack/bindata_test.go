package hack

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestExtensionBindata(t *testing.T) {
	for _, scenario := range []string{"build-clean", "fresh", "stale", "missing", "untracked", "generator-failure", "update"} {
		t.Run(scenario, func(t *testing.T) {
			directory := t.TempDir()
			write := func(name, content string, mode os.FileMode) {
				t.Helper()
				path := filepath.Join(directory, name)
				if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(content), mode); err != nil {
					t.Fatal(err)
				}
			}
			for _, name := range []string{"Makefile", "test/qe/bindata.mk"} {
				data, err := os.ReadFile(filepath.Join("..", "tests-extension", name))
				if err != nil {
					t.Fatal(err)
				}
				write(name, string(data), 0644)
			}
			write("test/qe/bindata.go", "package qe\n", 0644)
			write("tools/go", `#!/bin/sh
set -eu
echo "$*" >> "$CALL_LOG"
case "$1" in
build) mkdir -p bin; touch bin/router-tests-ext ;;
run)
  test "$SCENARIO" != build-clean
  test "$SCENARIO" != generator-failure
  test "$2" = github.com/go-bindata/go-bindata/v3/go-bindata@v3.1.3
  test "$3" = -nocompress
  test "$4" = -nometadata
  test "$5" = -pkg
  test "$6" = qe
  test "$7" = -o
  test "${10}" = testdata
  test "${11}" = testdata/...
  while [ "$1" != -o ]; do shift; done
  printf 'package qe\n' > "$2"
  ;;
*) exit 1 ;;
esac
`, 0755)
			run := func(name string, args ...string) ([]byte, error) {
				command := exec.Command(name, args...)
				command.Dir = directory
				command.Env = append(os.Environ(), "PATH="+filepath.Join(directory, "tools")+":"+os.Getenv("PATH"), "CALL_LOG="+filepath.Join(directory, "calls"), "SCENARIO="+scenario)
				return command.CombinedOutput()
			}
			for _, args := range [][]string{{"init", "-q"}, {"add", "test/qe/bindata.go"}} {
				if output, err := run("git", args...); err != nil {
					t.Fatalf("git %v: %v\n%s", args, err, output)
				}
			}
			switch scenario {
			case "stale", "update":
				write("test/qe/bindata.go", "package stale\n", 0644)
			case "missing":
				if err := os.Remove(filepath.Join(directory, "test/qe/bindata.go")); err != nil {
					t.Fatal(err)
				}
			case "untracked":
				if output, err := run("git", "rm", "--cached", "test/qe/bindata.go"); err != nil {
					t.Fatalf("git rm: %v\n%s", err, output)
				}
			}
			before, beforeErr := os.ReadFile(filepath.Join(directory, "test/qe/bindata.go"))
			targets := []string{"verify-bindata"}
			if scenario == "build-clean" {
				targets = []string{"clean", "build", "clean"}
			} else if scenario == "update" {
				targets = []string{"update-bindata", "verify-bindata"}
			}
			for _, target := range targets {
				output, err := run("make", target)
				wantFailure := scenario != "fresh" && scenario != "build-clean" && scenario != "update"
				if (err != nil) != wantFailure {
					t.Fatalf("make %s: %v (want failure %t)\n%s", target, err, wantFailure, output)
				}
				if scenario == "build-clean" && target == "build" {
					if _, err := os.Stat(filepath.Join(directory, "bin/router-tests-ext")); err != nil {
						t.Fatalf("build did not create binary: %v", err)
					}
				}
			}
			after, afterErr := os.ReadFile(filepath.Join(directory, "test/qe/bindata.go"))
			if scenario == "update" {
				before = []byte("package qe\n")
			}
			if string(before) != string(after) || os.IsNotExist(beforeErr) != os.IsNotExist(afterErr) {
				t.Fatal("build/clean/verify changed generated source")
			}
			if scenario == "build-clean" {
				calls, err := os.ReadFile(filepath.Join(directory, "calls"))
				if err != nil || !strings.HasPrefix(string(calls), "build -o bin/router-tests-ext ./cmd\n") || strings.Count(string(calls), "\n") != 1 {
					t.Fatalf("unexpected Go commands: %q (%v)", calls, err)
				}
				if _, err := os.Stat(filepath.Join(directory, "bin/router-tests-ext")); !os.IsNotExist(err) {
					t.Fatalf("clean did not remove binary: %v", err)
				}
			}
		})
	}
}

func TestVerifyDependencies(t *testing.T) {
	for _, scenario := range []string{"consistent", "nested-drift", "other-drift", "command-failure", "disabled"} {
		t.Run(scenario, func(t *testing.T) {
			directory := t.TempDir()
			tools := t.TempDir()
			if err := os.Mkdir(filepath.Join(directory, "tests-extension"), 0755); err != nil {
				t.Fatal(err)
			}
			stub := `#!/bin/sh
set -eu
echo "${PWD##*/}:$*" >> "$CALL_LOG"
test "$SCENARIO" != command-failure
if [ "$SCENARIO" = nested-drift ] && [ "${PWD##*/}" = tests-extension ]; then
  mkdir -p vendor
  echo drift > vendor/drift.go
fi
if [ "$SCENARIO" = other-drift ]; then
  echo drift > unexpected.txt
fi
`
			if err := os.WriteFile(filepath.Join(tools, "go"), []byte(stub), 0755); err != nil {
				t.Fatal(err)
			}
			command := exec.Command("git", "init", "-q", directory)
			if output, err := command.CombinedOutput(); err != nil {
				t.Fatalf("git init: %v\n%s", err, output)
			}
			script, err := filepath.Abs("verify-deps.sh")
			if err != nil {
				t.Fatal(err)
			}
			command = exec.Command("bash", script)
			command.Dir = directory
			ci := "true"
			if scenario == "disabled" {
				ci = "false"
			}
			command.Env = append(os.Environ(), "PATH="+tools+":"+os.Getenv("PATH"), "CALL_LOG="+filepath.Join(tools, "calls"), "SCENARIO="+scenario, "OPENSHIFT_CI="+ci)
			output, err := command.CombinedOutput()
			wantFailure := scenario == "nested-drift" || scenario == "other-drift" || scenario == "command-failure"
			if (err != nil) != wantFailure {
				t.Fatalf("verify-deps: %v (want failure %t)\n%s", err, wantFailure, output)
			}
			calls, err := os.ReadFile(filepath.Join(tools, "calls"))
			if scenario == "disabled" {
				if !os.IsNotExist(err) {
					t.Fatalf("disabled verification invoked go: %q (%v)", calls, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if scenario == "consistent" && (strings.Count(string(calls), "\n") != 4 || !strings.Contains(string(calls), "tests-extension:mod vendor\ntests-extension:mod tidy\n")) {
				t.Fatalf("expected vendor and tidy in both modules: %q", calls)
			}
		})
	}
}
