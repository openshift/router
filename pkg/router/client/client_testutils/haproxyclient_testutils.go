package client_testutils

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// clientSockID makes it simple to create concurrent unix socket files
var clientSockID atomic.Int32

// CreateServerMock creates a server mock and returns its listening unix socket (without the unix:// prefix).
// Responses is a cmd->output hashmap, and cmd should not add the last trailing line break the client adds
// when calling HAProxy. defer stopServer() just after creating the mock server to ensure it is properly stopped.
func CreateServerMock(t *testing.T, responses map[string]string) (unixSocket string, stopServer func()) {
	unixSocket = path.Join(os.TempDir(), fmt.Sprintf("haproxy-client-%02d.sock", clientSockID.Add(1)))
	// clean up from old tests that crashed
	_ = os.RemoveAll(unixSocket)
	listener, err := net.Listen("unix", unixSocket)
	require.NoError(t, err)

	t.Cleanup(func() {
		err := os.RemoveAll(unixSocket)
		require.NoError(t, err)
	})

	go func() {
		buf := make([]byte, 4096)
		for {
			conn, err := listener.Accept()
			if errors.Is(err, net.ErrClosed) || !assert.NoError(t, err) {
				return
			}
			r, err := conn.Read(buf)
			if !assert.NoError(t, err) {
				return
			}
			cmd := strings.TrimSuffix(string(buf[:r]), "\n")
			response, found := responses[cmd]
			if !found {
				response = "command not found: " + cmd
			}
			_, err = conn.Write([]byte(response))
			if !assert.NoError(t, err) {
				return
			}
			err = conn.Close()
			if !assert.NoError(t, err) {
				return
			}
		}
	}()

	return unixSocket, func() { listener.Close() }
}
