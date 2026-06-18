package client

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/openshift/router/pkg/router/client/client_testutils"
)

func TestRunCommand(t *testing.T) {

	// TODO: improve with new scenarios as part of https://redhat.atlassian.net/browse/NE-2746
	// Non exhaustive list of examples:
	// - half-closed connection
	// - timeouts
	// - read/write failures
	// - response bigger than the buffer (1024 bytes)

	ctx := context.Background()

	t.Run("should fail on unexpected protocol", func(t *testing.T) {
		_, err := RunCommand(ctx, "someproto://thecontent", "valid command", ClientOpts{})
		require.EqualError(t, err, `invalid endpoint: someproto://thecontent`)
	})

	t.Run("should fail on invalid tcp address", func(t *testing.T) {
		_, err := RunCommand(ctx, "tcp://missingport", "valid command", ClientOpts{})
		require.EqualError(t, err, `dial tcp: address missingport: missing port in address`)
	})

	t.Run("should fail on unix socket not found", func(t *testing.T) {
		_, err := RunCommand(ctx, "unix:///file/not/found", "valid command", ClientOpts{})
		require.EqualError(t, err, `dial unix /file/not/found: connect: no such file or directory`)
	})

	t.Run("should receive expected command and return expected output", func(t *testing.T) {
		socket, stopServer := client_testutils.CreateServerMock(t, map[string]string{"valid command": "output\n\n"})
		defer stopServer()
		output, err := RunCommand(ctx, "unix://"+socket, "valid command", ClientOpts{})
		require.NoError(t, err)
		require.Equal(t, "output", output)
	})
}
