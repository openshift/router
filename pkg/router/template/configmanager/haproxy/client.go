package haproxy

import (
	"bytes"
	"fmt"
	"strings"
	"time"

	templaterouter "github.com/openshift/router/pkg/router/template"

	haproxy "github.com/bcicen/go-haproxy"

	utilwait "k8s.io/apimachinery/pkg/util/wait"
)

const (
	// Prefix for the socket file used for haproxy dynamic API commands.
	afUnixSocketPrefix = "unix://"

	// Prefix if TCP is used to communicate with haproxy.
	tcpSocketPrefix = "tcp://"

	// maxRetries is the number of times a command is retried.
	maxRetries = 3
)

// Client is a client used to dynamically configure haproxy.
type Client struct {
	socketAddress string
	timeout       int

	backends []*Backend
	maps     map[string]*HAProxyMap
}

// NewClient returns a client used to dynamically change the haproxy config.
func NewClient(socketName string, timeout int) *Client {
	sockAddr := socketName
	if !strings.HasPrefix(sockAddr, afUnixSocketPrefix) && !strings.HasPrefix(sockAddr, tcpSocketPrefix) {
		sockAddr = fmt.Sprintf("%s%s", afUnixSocketPrefix, sockAddr)
	}

	return &Client{
		socketAddress: sockAddr,
		timeout:       timeout,
		backends:      make([]*Backend, 0),
		maps:          make(map[string]*HAProxyMap),
	}
}

// RunCommand executes a haproxy dynamic config API command and if present
// converts the response as desired.
func (c *Client) RunCommand(cmd string, converter Converter) ([]byte, error) {
	log.V(2).Info("running haproxy command", "command", cmd)
	buffer, err := c.runCommandWithRetries(cmd, maxRetries)
	if err != nil {
		log.V(0).Info("haproxy dynamic config API command failed", "command", cmd, "error", err)
		return nil, err
	}

	response := buffer.Bytes()
	log.V(2).Info("haproxy command returned", "response", string(response))
	if converter == nil {
		return response, nil
	}

	return converter.Convert(response)
}

// Execute runs a haproxy dynamic config API command.
func (c *Client) Execute(cmd string) ([]byte, error) {
	return c.RunCommand(cmd, nil)
}

// Reset resets any changes and clears the backends and maps.
func (c *Client) Reset() {
	c.backends = make([]*Backend, 0)
	c.maps = make(map[string]*HAProxyMap)
}

// Commit flushes out any pending changes on all the backends and maps.
func (c *Client) Commit() error {
	for _, b := range c.backends {
		if err := b.Commit(); err != nil {
			return err
		}
	}

	for _, m := range c.maps {
		if err := m.Commit(); err != nil {
			return err
		}
	}

	return nil
}

// Backends returns the list of configured haproxy backends.
func (c *Client) Backends() ([]*Backend, error) {
	if len(c.backends) == 0 {
		if backends, err := buildHAProxyBackends(c); err != nil {
			return nil, err
		} else {
			c.backends = backends
		}
	}

	return c.backends, nil
}

// FindBackend returns a specific haproxy backend if it is configured.
func (c *Client) FindBackend(id templaterouter.ServiceAliasConfigKey) (*Backend, error) {
	if _, err := c.Backends(); err != nil {
		return nil, err
	}

	for _, b := range c.backends {
		if b.Name() == id {
			return b, nil
		}
	}

	return nil, fmt.Errorf("no backend found for id: %s", id)
}

// Maps returns the list of configured haproxy maps.
func (c *Client) Maps() ([]*HAProxyMap, error) {
	if len(c.maps) == 0 {
		hapMaps, err := buildHAProxyMaps(c)
		if err != nil {
			return nil, err
		}

		for _, v := range hapMaps {
			c.maps[v.Name()] = v
		}

		return hapMaps, nil
	}

	mapList := make([]*HAProxyMap, len(c.maps))
	i := 0
	for _, v := range c.maps {
		mapList[i] = v
		i++
	}

	return mapList, nil
}

// FindMap returns a populated haproxy map.
func (c *Client) FindMap(name string) (*HAProxyMap, error) {
	if _, err := c.Maps(); err != nil {
		return nil, err
	}

	if m, ok := c.maps[name]; ok {
		return m, m.Refresh()
	}

	return nil, fmt.Errorf("no map found for name: %s", name)
}

// runCommandWithRetries retries a haproxy command upto the retry limit
// if the error for the command is a retryable error.
func (c *Client) runCommandWithRetries(cmd string, limit int) (*bytes.Buffer, error) {
	var buffer *bytes.Buffer
	var cmdErr error

	cmdWaitBackoff := utilwait.Backoff{
		Duration: 10 * time.Millisecond,
		Factor:   2,
		Steps:    limit,
	}

	n := 0
	utilwait.ExponentialBackoff(cmdWaitBackoff, func() (bool, error) {
		n++
		client := &haproxy.HAProxyClient{
			Addr:    c.socketAddress,
			Timeout: c.timeout,
		}
		buffer, cmdErr = client.RunCommand(cmd)
		if cmdErr == nil {
			return true, nil
		}
		if !isRetriable(cmdErr, cmd) {
			return false, cmdErr
		}
		return false, nil
	})

	if cmdErr != nil {
		log.V(2).Info("failed attempt to run haproxy command", "command", cmd, "attempts", n, "error", cmdErr)
	}

	return buffer, cmdErr
}

// isRetriable checks if a haproxy command can be retried.
func isRetriable(err error, cmd string) bool {
	retryableErrors := []string{
		"connection reset by peer",
		"connection refused",
	}

	s := err.Error()
	for _, v := range retryableErrors {
		if strings.HasSuffix(s, v) {
			return true
		}
	}

	return false
}
