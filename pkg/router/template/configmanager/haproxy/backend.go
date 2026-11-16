package haproxy

import (
	"errors"
	"fmt"
	"os"
	"path"
	"strconv"
	"strings"

	routev1 "github.com/openshift/api/route/v1"
	templaterouter "github.com/openshift/router/pkg/router/template"
)

// Backend sends dynamic configuration API commands to a specific haproxy backend. It does not hold
// any state from haproxy, so it is cheap to create, and it is safe to discard after use.
type Backend struct {
	client HAProxyClient
	name   templaterouter.ServiceAliasConfigKey
}

// newBackend returns a Backend that sends commands to the named haproxy backend using the provided client.
func newBackend(client HAProxyClient, backendName templaterouter.ServiceAliasConfigKey) *Backend {
	return &Backend{
		client: client,
		name:   backendName,
	}
}

// AddServer dynamically adds a new backend server. It detects if the server already exists, and if so tries to remove it.
// It returns a failure in case HAProxy refuses to dynamically add the server for any reason, or if the existing server
// cannot be removed, e.g., it still has active or steady and established connection(s) to its backend server endpoint.
func (b *Backend) AddServer(cfg *templaterouter.ServiceAliasConfig, svc *templaterouter.ServiceUnit, ep templaterouter.Endpoint, workingDir, defaultDestinationCA string) error {
	if err := b.innerAddServer(cfg, svc, ep, workingDir, defaultDestinationCA); err != nil {
		if !errors.Is(err, ErrServerAlreadyExists) {
			return err
		}
		// Failed due to already existing server left behind, in maintenance mode, due to in-flight connections.
		// Let's just give it another chance to be deleted.
		if err := b.innerDeleteServer(ep); err != nil {
			// No way, need to fail which will ask for a fork-and-reload. This will leave the existing connections in the old process.
			return err
		}
		if err := b.innerAddServer(cfg, svc, ep, workingDir, defaultDestinationCA); err != nil {
			return err
		}
	}
	if err := b.innerSetServerState(ep, true); err != nil {
		return err
	}

	// health check is disabled by default on new backend servers, its enablement is handled via cm.ReplaceRouteEndpoints(),
	// since that method has a better view of former and current active backend servers.
	return nil
}

// UpdateServer dynamically updates the backend server with new address and weight.
func (b *Backend) UpdateServer(cfg *templaterouter.ServiceAliasConfig, svc *templaterouter.ServiceUnit, oldEP, newEP templaterouter.Endpoint, isPassthrough bool, workingDir, defaultDestinationCA string) (added bool, err error) {
	oldIsH2 := isH2C(oldEP.AppProtocol)
	newIsH2 := isH2C(newEP.AppProtocol)
	if oldIsH2 != newIsH2 || oldEP.VerifyHostname != newEP.VerifyHostname {
		// changes require to remove+add endpoints, an error is returned in case this cannot be done, e.g., existing connections
		return true, b.ReplaceServer(cfg, svc, oldEP, newEP, workingDir, defaultDestinationCA)
	}

	// changes that can be applied in the running server
	if oldEP.IP != newEP.IP || oldEP.Port != newEP.Port {
		if err := b.innerUpdateServerAddr(newEP); err != nil {
			return false, err
		}
	}

	if oldEP.Weight != newEP.Weight {
		return false, b.UpdateServerWeight(oldEP, newEP, isPassthrough)
	}

	return false, nil
}

// UpdateServerWeight updates the weight of the backend server represented by the new endpoint.
// It also updates the state of the endpoint to `drain` or `ready` if the weight going to, or coming from `0`.
func (b *Backend) UpdateServerWeight(oldEP, newEP templaterouter.Endpoint, isPassthrough bool) error {
	if err := b.innerUpdateServerWeight(newEP, isPassthrough); err != nil {
		return err
	}
	if (oldEP.Weight <= 0) != (newEP.Weight <= 0) {
		if err := b.innerSetServerState(newEP, true); err != nil {
			return err
		}
	}
	return nil
}

// ReplaceServer dynamically replaces the backend server by removing it and adding again with new configuration.
// Note that a failure adding the backend server should result in the server being missed in the configuration,
// which would cause an outage until HAProxy is reloaded, because of that this method returns an error if trying
// to replace the only server from a backend.
func (b *Backend) ReplaceServer(cfg *templaterouter.ServiceAliasConfig, svc *templaterouter.ServiceUnit, oldEP, newEP templaterouter.Endpoint, workingDir, defaultDestinationCA string) error {
	count, err := b.innerServerCount()
	if err != nil {
		return err
	}
	if count < 2 {
		return fmt.Errorf("cannot replace server: backend %q has fewer than two servers", b.name)
	}
	if err := b.innerSetServerState(oldEP, false); err != nil {
		return err
	}
	if err := b.innerDeleteServer(oldEP); err != nil {
		if rollbackErr := b.innerSetServerState(oldEP, true); rollbackErr != nil {
			return fmt.Errorf("deleting old server: %v; restoring old server state: %v", err, rollbackErr)
		}
		return err
	}
	if err := b.innerAddServer(cfg, svc, newEP, workingDir, defaultDestinationCA); err != nil {
		return err
	}
	return b.innerSetServerState(newEP, true)
}

// EnableHealthCheck dynamically enables health check on a backend server that already declares the health check interval.
func (b *Backend) EnableHealthCheck(ep templaterouter.Endpoint) error {
	return b.innerSetHealthCheck(ep, true)
}

// DisableHealthCheck dynamically disables health check on a backend server.
func (b *Backend) DisableHealthCheck(ep templaterouter.Endpoint) error {
	if err := b.innerSetHealthCheck(ep, false); err != nil {
		return err
	}
	// manually set the new health state after disabling the automatic check
	return b.innerSetServerHealth(ep, true)
}

// DeleteServer dynamically removes the backend server from the load balance. The backend server is put in maintenance mode
// and returns `removed` as false in case it has active or steady and established connections, so these connections continue
// to be handled and new ones are directed to other servers. An error only happens if the server cannot be put in maintenance
// mode, any failure trying to remove the server is logged and just return removed as false. A server that does not exist
// is reported as removed, since the desired state is already in place.
func (b *Backend) DeleteServer(ep templaterouter.Endpoint) (removed bool, err error) {
	// put in maintenance mode first, this is a pre-requisite to remove a backend server.
	if err := b.innerSetServerState(ep, false); err != nil {
		if errors.Is(err, ErrServerNotFound) {
			// Nothing to delete, e.g. the router state is out of sync with haproxy and the server was already removed.
			return true, nil
		}
		return false, err
	}
	if err := b.innerDeleteServer(ep); err != nil {
		log.Info("disabling backend server instead of deleting due to a delete failure", "server", ep.ID, "error", err.Error())
		return false, nil
	}
	return true, nil
}

// innerAddServer adds a new server to the backend, configured the same way the template configures it.
// The server starts in maintenance mode and with health check disabled, which is the haproxy default
// for dynamically added servers.
func (b *Backend) innerAddServer(cfg *templaterouter.ServiceAliasConfig, svc *templaterouter.ServiceUnit, ep templaterouter.Endpoint, workingDir, defaultDestinationCA string) error {
	// This should always follow the template, changes here should be reflected there, both regular and passthrough backends
	//
	// TODO: either read this configuration from the template, or instead make the template read from here.
	// For the former, note that creating a new template definition should conflict with the for-loop in templateRouter.writeConfig()
	// that assumes that all the definitions should be written to disk.
	//
	// https://redhat.atlassian.net/browse/NE-2646

	cmd := fmt.Sprintf("add server %s/%s %s:%s weight %d", b.name, ep.ID, ep.IP, ep.Port, ep.Weight)

	switch cfg.TLSTermination {
	case routev1.TLSTerminationReencrypt:
		cmd += " ssl"
		if disableHTTP2, _ := strconv.ParseBool(os.Getenv("ROUTER_DISABLE_HTTP2")); !disableHTTP2 {
			cmd += " alpn h2,http/1.1"
		}
		if ep.VerifyHostname {
			cmd += " verifyhost " + svc.Hostname
		}
		if cert := cfg.Certificates[cfg.Host+"_pod"]; len(cert.Contents) > 0 {
			cmd += " verify required ca-file " + path.Join(workingDir, "router/cacerts", cert.ID+".pem")
		} else if len(defaultDestinationCA) > 0 {
			cmd += " verify required ca-file " + defaultDestinationCA
		} else {
			cmd += " verify none"
		}
		cmd += " check-ssl"
	case "", routev1.TLSTerminationEdge:
		if isH2C(ep.AppProtocol) {
			cmd += " proto h2"
		}
	case routev1.TLSTerminationPassthrough:
		// passthrough is a TCP listener and does not use ssl or proto related config
	}

	// health check is always configured and defaults as disabled, being enabled later
	// on DCM's manager depending on `cfg.ActiveEndpoints > 1` and `!ep.NoHealthCheck`.
	inter := templaterouter.FirstMatch(`[1-9][0-9]*(us|ms|s|m|h|d)?`,
		cfg.Annotations["router.openshift.io/haproxy.health.check.interval"],
		os.Getenv("ROUTER_BACKEND_CHECK_INTERVAL"),
		"5000ms")
	cmd += " check inter " + inter

	podMaxConn := cfg.Annotations["haproxy.router.openshift.io/pod-concurrent-connections"]
	if _, err := strconv.Atoi(podMaxConn); err == nil {
		cmd += " maxconn " + podMaxConn
	}

	return execCommand(b.client, apiAddServer, cmd)
}

// innerUpdateServerAddr updates the address and port of an existing server.
func (b *Backend) innerUpdateServerAddr(ep templaterouter.Endpoint) error {
	cmd := fmt.Sprintf("set server %s/%s addr %s port %s", b.name, ep.ID, ep.IP, ep.Port)
	return execCommand(b.client, apiSetServerAddr, cmd)
}

// innerUpdateServerWeight updates the weight of an existing server.
func (b *Backend) innerUpdateServerWeight(ep templaterouter.Endpoint, isPassthrough bool) error {
	// https://github.com/openshift/router/blob/896390778ebe15f57f87e6ca78f11c96e64c2652/pkg/router/template/configmanager/haproxy/manager.go#L446-L454
	weight := "100%" // hardcoded for passthrough
	if !isPassthrough {
		weight = strconv.Itoa(int(ep.Weight))
	}
	cmd := fmt.Sprintf("set server %s/%s weight %s", b.name, ep.ID, weight)
	return execCommand(b.client, apiSetServerWeight, cmd)
}

// innerSetHealthCheck enables or disables the health check of an existing server.
func (b *Backend) innerSetHealthCheck(ep templaterouter.Endpoint, enable bool) error {
	enableStr := "enable"
	if !enable {
		enableStr = "disable"
	}
	cmd := fmt.Sprintf("%s health %s/%s", enableStr, b.name, ep.ID)
	return execCommand(b.client, apiSetHealth, cmd)
}

// innerSetServerHealth forces the health state of an existing server to up or down.
func (b *Backend) innerSetServerHealth(ep templaterouter.Endpoint, up bool) error {
	upStr := "up"
	if !up {
		upStr = "down"
	}
	cmd := fmt.Sprintf("set server %s/%s health %s", b.name, ep.ID, upStr)
	return execCommand(b.client, apiSetServerHealth, cmd)
}

// innerServerCount returns the number of servers of the backend, including the ones in maintenance mode.
func (b *Backend) innerServerCount() (int, error) {
	// The dump has the following format:
	//   - first line contains the format version (1 in this specification);
	//   - second line contains the column headers, prefixed by a sharp ('#');
	//   - third line and next ones contain data;
	//   - each line starting by a sharp ('#') is considered as a comment.
	//
	// https://docs.haproxy.org/3.2/management.html#9.3-show%20servers%20state

	cmd := fmt.Sprintf("show servers state %s", b.name)
	responseStr, err := execCommandOutput(b.client, cmd)
	if err != nil {
		return 0, err
	}

	response := strings.Split(responseStr, "\n")
	if len(response) < 2 {
		return 0, fmt.Errorf("invalid HAProxy response for 'show servers state': %s", responseStr)
	}
	if response[0] != "1" {
		return 0, fmt.Errorf("unsupported format version for 'show servers state': %s", response[0])
	}

	return len(response) - 2, nil
}

// innerSetServerState changes the administrative state of an existing server: `maint` if not ready,
// or else `drain` if its weight is zero, or else `ready`.
func (b *Backend) innerSetServerState(ep templaterouter.Endpoint, ready bool) error {
	state := "ready"
	if !ready {
		state = "maint"
	} else if ep.Weight <= 0 {
		state = "drain"
	}
	cmd := fmt.Sprintf("set server %s/%s state %s", b.name, ep.ID, state)
	return execCommand(b.client, apiSetServerState, cmd)
}

// innerDeleteServer removes an existing server, which only succeeds if the server is in maintenance mode
// and has no connections.
func (b *Backend) innerDeleteServer(ep templaterouter.Endpoint) error {
	cmd := fmt.Sprintf("del server %s/%s", b.name, ep.ID)
	return execCommand(b.client, apiDelServer, cmd)
}

// isH2C reports whether appProtocol refers to HTTP/2 over cleartext.
func isH2C(appProtocol string) bool {
	return appProtocol == "h2c" || appProtocol == "kubernetes.io/h2c"
}

// apiType identifies a haproxy dynamic configuration API command, so its response can be properly validated.
type apiType int

// The haproxy dynamic configuration API commands sent by Backend.
const (
	apiAddServer apiType = iota
	apiDelServer
	apiSetHealth
	apiSetServerAddr
	apiSetServerHealth
	apiSetServerWeight
	apiSetServerState
)

// execCommandOutput executes cmd and returns its response without leading and trailing white spaces.
func execCommandOutput(client HAProxyClient, cmd string) (string, error) {
	responseRaw, err := client.Execute(cmd)
	if err != nil {
		return "", err
	}
	response := strings.TrimSpace(string(responseRaw))
	return response, nil
}

// execCommand executes cmd and validates its response according to api. An empty response is
// always valid. Known failures are returned as errors wrapping ErrServerAlreadyExists,
// ErrServerNotFound or ErrHealthCheckNotConfigured, so callers can handle them.
func execCommand(client HAProxyClient, api apiType, cmd string) error {
	response, err := execCommandOutput(client, cmd)
	if len(response) == 0 || err != nil {
		return err
	}

	var valid bool
	switch api {
	case apiAddServer:
		valid = response == "New server registered."
	case apiDelServer:
		valid = response == "Server deleted."
	case apiSetServerAddr:
		valid = response == "nothing changed" || strings.HasPrefix(response, "IP changed from ") || strings.HasPrefix(response, "port changed from ") || strings.HasPrefix(response, "no need to change ")
	case apiSetHealth, apiSetServerHealth, apiSetServerWeight, apiSetServerState:
		valid = false // any response from these api calls mean there is a failure
	default:
		// fail fast in case of a dev error
		panic(fmt.Errorf("invalid cmd ID: %d", api))
	}

	if !valid {
		if strings.Contains(response, "Already exists a server") {
			return NewError(ErrServerAlreadyExists, response)
		}
		if strings.Contains(response, "No such server") {
			return NewError(ErrServerNotFound, response)
		}
		if strings.Contains(response, "Health check was not configured") {
			return NewError(ErrHealthCheckNotConfigured, response)
		}
		return fmt.Errorf("unexpected response from haproxy: %s", response)
	}
	return nil
}
