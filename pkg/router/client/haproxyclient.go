package client

import (
	"context"
	"fmt"
	"io"
	"net"
	"regexp"
	"strings"
	"time"
)

// ClientOpts has options for the HAProxy client. All options have a proper
// default value, so the zero value is also a valid configuration.
type ClientOpts struct {
	// Timeout of the API call. The default value is 5s if not declared.
	Timeout time.Duration
}

var haproxyEndpointRegex = regexp.MustCompile(`^(tcp|unix)://(.*)$`)

// RunCommand sends the specified `cmd` to the HAProxy endpoint, which could be either
// the API or the admin/master one. `endpoint` has the "<schema>://<address>" syntax
// which could be either "tcp://ip-or-name:port-number" or "unix:///path/to/unix-socket".
func RunCommand(ctx context.Context, endpoint, cmd string, opts ClientOpts) (string, error) {

	// TODO: this is the starting point of a new HAProxy client, managed by the NI&D team.
	// Further improvements already tracked via https://redhat.atlassian.net/browse/NE-2746

	if opts.Timeout == 0 {
		opts.Timeout = 5 * time.Second
	}

	var d net.Dialer
	endpointParts := haproxyEndpointRegex.FindStringSubmatch(endpoint)
	if len(endpointParts) != 3 {
		return "", fmt.Errorf("invalid endpoint: %s", endpoint)
	}
	conn, err := d.DialContext(ctx, endpointParts[1], endpointParts[2])
	if err != nil {
		return "", err
	}
	defer conn.Close()
	rawConn, ok := conn.(interface{ CloseWrite() error })
	if !ok {
		// Just a safeguard, since validation enforces TCP and Unix only, and both implements CloseWrite().
		return "", fmt.Errorf("connection type %T does not implement CloseWrite(), cannot half close the connection", conn)
	}

	err = conn.SetDeadline(time.Now().Add(opts.Timeout))
	if err != nil {
		return "", err
	}
	// Immediately cancel the connection in case the context is cancelled.
	stop := context.AfterFunc(ctx, func() {
		conn.SetDeadline(time.Now())
	})
	defer stop()

	// Adding a line-break if missing, HAProxy expects it to run the command.
	if !strings.HasSuffix(cmd, "\n") {
		cmd += "\n"
	}
	_, err = conn.Write([]byte(cmd))
	if err != nil {
		return "", err
	}

	// Half close the connection, this tells HAProxy to close its write counterpart
	// (our read) as soon as it finishes to respond.
	err = rawConn.CloseWrite()
	if err != nil {
		return "", err
	}

	// Reading response into a loop and populating a buffer with the response chunks.
	// io.EOF response means the read socket was closed by haproxy after all the data being transmitted.
	buf := make([]byte, 1024)
	output := strings.Builder{}
	for {
		count, err := conn.Read(buf)
		output.Write(buf[:count])
		if err == io.EOF {
			return strings.TrimSpace(output.String()), nil
		}
		if err != nil {
			return "", err
		}
	}
}
