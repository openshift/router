package util

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"syscall"
	"testing"
	"time"
)

func TestRunStartedProcessWithContextCancelsAndReapsStalledCommand(t *testing.T) {
	cmd := exec.Command("sh", "-c", "sleep 30")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start stalled command: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, _, err := runStartedProcessWithContextOutputs(ctx, cmd, &stdout, &stderr)
	elapsed := time.Since(start)

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected context deadline exceeded, got %v", err)
	}
	if elapsed > 5*time.Second {
		t.Fatalf("stalled command returned after %s, expected prompt cancellation", elapsed)
	}
	if cmd.ProcessState == nil {
		t.Fatalf("stalled child was not reaped; process state is nil")
	}
	status, ok := cmd.ProcessState.Sys().(syscall.WaitStatus)
	if !ok {
		t.Fatalf("unexpected process state type: %T", cmd.ProcessState.Sys())
	}
	if !status.Signaled() && !cmd.ProcessState.Exited() {
		t.Fatalf("stalled child did not exit or get signaled; process state: %#v", cmd.ProcessState)
	}
}
