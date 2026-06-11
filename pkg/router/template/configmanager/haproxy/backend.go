package haproxy

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path"
	"strconv"
	"strings"

	v1 "github.com/openshift/api/route/v1"
	templaterouter "github.com/openshift/router/pkg/router/template"
)

// BackendServerState indicates the state for a haproxy backend server.
type BackendServerState string

const (
	// BackendServerStateReady indicates a server is ready.
	BackendServerStateReady BackendServerState = "ready"

	// BackendServerStateDrain indicates a server is ready but draining.
	BackendServerStateDrain BackendServerState = "drain"

	// BackendServerStateMaint indicates a server is under maintainence.
	BackendServerStateMaint BackendServerState = "maint"

	// ListBackendsCommand is the command to get a list of all backends.
	ListBackendsCommand = "show backend"

	// GetServersStateCommand gets the state of all servers. This can be
	// optionally filtered by backends by passing a backend name.
	GetServersStateCommand = "show servers state"

	// SetServerCommand sets server specific information and state.
	SetServerCommand = "set server"

	// showBackendHeader is the haproxy backend list csv output header.
	showBackendHeader = "name"

	// serverStateHeader is the haproxy server state csv output header.
	serversStateHeader = "be_id be_name srv_id srv_name srv_addr srv_op_state srv_admin_state srv_uweight srv_iweight srv_time_since_last_change srv_check_status srv_check_result srv_check_health srv_check_state srv_agent_state bk_f_forced_id srv_f_forced_id srv_fqdn srv_port"
)

// backendEntry is an entry in the list of backends returned from haproxy.
type backendEntry struct {
	Name string `csv:"name"`
}

// serverStateInfo represents the state of a specific backend server.
type serverStateInfo struct {
	BackendID   string `csv:"be_id"`
	BackendName string `csv:"be_name"`
	ID          string `csv:"srv_id"`
	Name        string `csv:"srv_name"`
	IPAddress   string `csv:"srv_addr"`

	OperationalState    int32 `csv:"srv_op_state"`
	AdministrativeState int32 `csv:"srv_admin_state"`
	UserVisibleWeight   int32 `csv:"srv_uweight"`
	InitialWeight       int32 `csv:"srv_iweight"`

	TimeSinceLastChange   int `csv:"srv_time_since_last_change"`
	LastHealthCheckStatus int `csv:"srv_check_status"`
	LastHealthCheckResult int `csv:"srv_check_result"`
	CheckHealth           int `csv:"srv_check_health"`
	CheckHealthState      int `csv:"srv_check_state"`
	AgentCheckState       int `csv:"srv_agent_state"`

	BackendIDForced int `csv:"bk_f_forced_id"`
	IDForced        int `csv:"srv_f_forced_id"`

	FQDN string `csv:"srv_fqdn"`
	Port int    `csv:"srv_port"`
}

// BackendServerInfo represents a server [endpoint] for a haproxy backend.
type BackendServerInfo struct {
	Name          string
	IPAddress     string
	Port          int
	CurrentWeight int32
}

// Backend represents a specific haproxy backend.
type Backend struct {
	name    templaterouter.ServiceAliasConfigKey
	servers map[string]*backendServer

	client HAProxyClient
}

// backendServer is internally used for managing a haproxy backend server.
type backendServer struct {
	BackendServerInfo

	updatedIPAddress string
	updatedPort      int
	updatedWeight    string // as it can be a percentage.
	updatedState     BackendServerState
}

// buildHAProxyBackends builds and returns a list of haproxy backends.
func buildHAProxyBackends(c *Client) ([]*Backend, error) {
	entries := []*backendEntry{}
	converter := NewCSVConverter(showBackendHeader, &entries, nil)
	_, err := c.RunCommand(ListBackendsCommand, converter)
	if err != nil {
		return []*Backend{}, err
	}

	backends := make([]*Backend, len(entries))
	for k, v := range entries {
		backends[k] = newBackend(templaterouter.ServiceAliasConfigKey(v.Name), c)
	}

	return backends, nil
}

// newBackend returns a new Backend representing a haproxy backend.
func newBackend(name templaterouter.ServiceAliasConfigKey, c HAProxyClient) *Backend {
	return &Backend{
		name:    name,
		servers: make(map[string]*backendServer),
		client:  c,
	}
}

// Name returns the name of this haproxy backend.
func (b *Backend) Name() templaterouter.ServiceAliasConfigKey {
	return b.name
}

// Reset resets the cached server info in this haproxy backend.
func (b *Backend) Reset() {
	b.servers = make(map[string]*backendServer)
}

// Refresh refreshs our internal state for this haproxy backend.
func (b *Backend) Refresh() error {
	entries := []*serverStateInfo{}
	converter := NewCSVConverter(serversStateHeader, &entries, stripVersionNumber)
	cmd := fmt.Sprintf("%s %s", GetServersStateCommand, b.Name())
	_, err := b.client.RunCommand(cmd, converter)
	if err != nil {
		return err
	}

	b.servers = make(map[string]*backendServer)
	for _, v := range entries {
		info := BackendServerInfo{
			Name:          v.Name,
			IPAddress:     v.IPAddress,
			Port:          v.Port,
			CurrentWeight: v.UserVisibleWeight,
		}

		b.servers[v.Name] = newBackendServer(info)
	}

	return nil
}

// SetRoutingKey sets the cookie routing key for the haproxy backend.
func (b *Backend) SetRoutingKey(k string) error {
	log.V(4).Info("setting routing key", "backend", b.name)

	cmd := fmt.Sprintf("set dynamic-cookie-key backend %s %s", b.name, k)
	if err := b.executeCommand(cmd); err != nil {
		return fmt.Errorf("setting routing key for backend %s: %v", b.name, err)
	}

	cmd = fmt.Sprintf("enable dynamic-cookie backend %s", b.name)
	if err := b.executeCommand(cmd); err != nil {
		return fmt.Errorf("enabling routing key for backend %s: %v", b.name, err)
	}

	return nil
}

// executeCommand runs a command using the haproxy dynamic config api client.
func (b *Backend) executeCommand(cmd string) error {
	responseBytes, err := b.client.Execute(cmd)
	if err != nil {
		return err
	}

	response := strings.TrimSpace(string(responseBytes))
	if len(response) > 0 {
		return errors.New(response)
	}

	return nil
}

// Disable stops serving traffic for all servers for a haproxy backend.
func (b *Backend) Disable() error {
	if _, err := b.Servers(); err != nil {
		return err
	}

	for _, s := range b.servers {
		if err := b.DisableServer(s.Name); err != nil {
			return err
		}
	}

	return nil
}

// EnableServer enables serving traffic with a haproxy backend server.
func (b *Backend) EnableServer(name string) error {
	log.V(4).Info("enabling server with ready state", "server", name)
	return b.UpdateServerState(name, BackendServerStateReady)
}

// DisableServer stops serving traffic for a haproxy backend server.
func (b *Backend) DisableServer(name string) error {
	log.V(4).Info("disabling server with maint state", "server", name)
	return b.UpdateServerState(name, BackendServerStateMaint)
}

// Commit commits all the pending changes made to a haproxy backend.
func (b *Backend) Commit() error {
	for _, s := range b.servers {
		if err := s.ApplyChanges(b.name, b.client); err != nil {
			return err
		}
	}

	b.Reset()
	return nil
}

// Servers returns the servers for this haproxy backend.
func (b *Backend) Servers() ([]BackendServerInfo, error) {
	if len(b.servers) == 0 {
		if err := b.Refresh(); err != nil {
			return []BackendServerInfo{}, err
		}
	}

	serverInfo := make([]BackendServerInfo, len(b.servers))
	i := 0
	for _, s := range b.servers {
		serverInfo[i] = s.BackendServerInfo
		i++
	}

	return serverInfo, nil
}

// UpdateServerInfo updates the information for a haproxy backend server.
func (b *Backend) UpdateServerInfo(id, ipaddr, port, appProtocol string, weight int32, relativeWeight bool) error {
	server, err := b.FindServer(id)
	if err != nil {
		return err
	}

	if len(ipaddr) > 0 {
		server.updatedIPAddress = ipaddr
	}
	if n, err := strconv.Atoi(port); err == nil && n > 0 {
		server.updatedPort = n
	}
	if appProtocol == "h2c" || appProtocol == "kubernetes.io/h2c" {
		return errors.New("dynamically updating proto is unsupported")
	}
	if weight > -1 {
		suffix := ""
		if relativeWeight {
			suffix = "%"
		}
		server.updatedWeight = fmt.Sprintf("%v%s", weight, suffix)
	}

	return nil
}

// UpdateServerState specifies what should be the state of a haproxy backend
// server when all the changes made to the backend committed.
func (b *Backend) UpdateServerState(id string, state BackendServerState) error {
	server, err := b.FindServer(id)
	if err != nil {
		return err
	}

	server.updatedState = state
	return nil
}

// FindServer returns a specific haproxy backend server if found.
func (b *Backend) FindServer(id string) (*backendServer, error) {
	if _, err := b.Servers(); err != nil {
		return nil, err
	}

	if s, ok := b.servers[id]; ok {
		return s, nil
	}

	return nil, fmt.Errorf("no server found for id: %s", id)
}

// AddServer dynamically adds a new backend server. It detects if the server already exists, and if so tries to remove it.
// It returns a failure in case HAProxy refuses to dynamically add the server for any reason, or if the existing server
// cannot be removed, e.g., it still have active or steady and established connection(s) to its backend server endpoint.
func (b *Backend) AddServer(cfg *templaterouter.ServiceAliasConfig, svc *templaterouter.ServiceUnit, ep templaterouter.Endpoint, backendServerID, weight int32, workingDir, defaultDestinationCA string) error {
	if err := b.innerAddServer(cfg, svc, ep, backendServerID, weight, workingDir, defaultDestinationCA); err != nil {
		if !strings.Contains(err.Error(), "Already exists a server ") {
			return err
		}
		// Failed due to already existing server left behind, in maintenance mode, due to in-flight connections.
		// Let's just give it another chance to be deleted.
		if err := b.innerDeleteServer(ep); err != nil {
			// No way, need to fail which will ask for a fork-and-reload. This will leave the existing connections in the old process.
			return err
		}
		if err := b.innerAddServer(cfg, svc, ep, backendServerID, weight, workingDir, defaultDestinationCA); err != nil {
			return err
		}
	}
	if err := b.innerSetServerState(ep, true, weight); err != nil {
		return err
	}

	// health check is disabled by default on new backend servers, its enablement is handled via cm.ReplaceRouteEndpoints(),
	// since that method has a better view of former and current active backend servers.
	return nil
}

// UpdateServer dynamically updates the backend server with new address and weight.
func (b *Backend) UpdateServer(ep templaterouter.Endpoint, weight int32, isPassthrough bool) error {
	// missing to properly populate the current servers when created, should be done in the next phase.
	// After that we can update only changed attributes.
	// https://redhat.atlassian.net/browse/NE-2646
	if err := b.innerUpdateServerAddr(ep); err != nil {
		return err
	}
	return b.innerUpdateServerWeight(ep, weight, isPassthrough)
}

// EnableHealthCheck dynamically enables health check on a backend server that already declares the health check interval.
func (b *Backend) EnableHealthCheck(ep templaterouter.Endpoint) error {
	return b.innerSetHealthCheck(ep, true)
}

// DisableHealthCheck dynamically disables health check on a backend server.
func (b *Backend) DisableHealthCheck(ep templaterouter.Endpoint) error {
	return b.innerSetHealthCheck(ep, false)
}

// DeleteServer dynamically removes the backend server from the load balance. The backend server is put in maintenance mode
// and returns `removed` as false in case it has active or steady and established connections, so these connections continue
// to be handled and new ones are directed to other servers. An error only happens if the server cannot be put in maintenance
// mode, any failure trying to remove the server is logged and just return removed as false.
func (b *Backend) DeleteServer(ep templaterouter.Endpoint) (removed bool, err error) {
	// put in maintenance mode first, this is a pre-requisite to remove a backend server.
	if err := b.innerSetServerState(ep, false, 0); err != nil {
		return false, err
	}
	if err := b.innerDeleteServer(ep); err != nil {
		log.Info("disabling backend server instead of deleting due to a delete failure", "server", ep.ID, "error", err.Error())
		return false, nil
	}
	return true, nil
}

func (b *Backend) innerAddServer(cfg *templaterouter.ServiceAliasConfig, svc *templaterouter.ServiceUnit, ep templaterouter.Endpoint, backendServerID, weight int32, workingDir, defaultDestinationCA string) error {
	// This should always follow the template, changes here should be reflected there, both regular and passthrough backends
	//
	// TODO: either read this configuration from the template, or instead make the template read from here.
	// For the former, note that creating a new template definition should conflict with the for-loop in templateRouter.writeConfig()
	// that assumes that all the definitions should be written to disk.
	//
	// https://redhat.atlassian.net/browse/NE-2646

	cmd := fmt.Sprintf("add server %s/%s %s:%s weight %d id %d", b.name, ep.ID, ep.IP, ep.Port, weight, backendServerID)

	switch cfg.TLSTermination {
	case v1.TLSTerminationReencrypt:
		cmd += " ssl"
		if disableHTTP2, _ := strconv.ParseBool(os.Getenv("ROUTER_DISABLE_HTTP2")); !disableHTTP2 {
			cmd += " alpn h2,http/1.1"
		}
		if cfg.VerifyServiceHostname {
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
	case "", v1.TLSTerminationEdge:
		if ep.AppProtocol == "h2c" || ep.AppProtocol == "kubernetes.io/h2c" {
			cmd += " proto h2"
		}
	case v1.TLSTerminationPassthrough:
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

func (b *Backend) innerUpdateServerAddr(ep templaterouter.Endpoint) error {
	cmd := fmt.Sprintf("set server %s/%s addr %s port %s", b.name, ep.ID, ep.IP, ep.Port)
	return execCommand(b.client, apiSetServerAddr, cmd)
}

func (b *Backend) innerUpdateServerWeight(ep templaterouter.Endpoint, weight int32, isPassthrough bool) error {
	cmd := fmt.Sprintf("set server %s/%s", b.name, ep.ID)
	if isPassthrough {
		// https://github.com/openshift/router/blob/896390778ebe15f57f87e6ca78f11c96e64c2652/pkg/router/template/configmanager/haproxy/manager.go#L446-L454
		cmd += " weight 100%"
	} else {
		cmd = fmt.Sprintf("%s weight %d", cmd, weight)
	}
	return execCommand(b.client, apiSetServerWeight, cmd)
}

func (b *Backend) innerSetHealthCheck(ep templaterouter.Endpoint, enable bool) error {
	enableStr := "enable"
	if !enable {
		enableStr = "disable"
	}
	cmd := fmt.Sprintf("%s health %s/%s", enableStr, b.name, ep.ID)
	return execCommand(b.client, apiSetHealth, cmd)
}

func (b *Backend) innerSetServerState(ep templaterouter.Endpoint, ready bool, weight int32) error {
	state := "ready"
	if !ready {
		state = "maint"
	} else if weight <= 0 {
		state = "drain"
	}
	cmd := fmt.Sprintf("set server %s/%s state %s", b.name, ep.ID, state)
	return execCommand(b.client, apiSetServerState, cmd)
}

func (b *Backend) innerDeleteServer(ep templaterouter.Endpoint) error {
	cmd := fmt.Sprintf("del server %s/%s", b.name, ep.ID)
	return execCommand(b.client, apiDelServer, cmd)
}

// newBackendServer returns a BackendServer representing a haproxy backend server.
func newBackendServer(info BackendServerInfo) *backendServer {
	return &backendServer{
		BackendServerInfo: info,

		updatedIPAddress: info.IPAddress,
		updatedPort:      info.Port,
		updatedWeight:    strconv.Itoa(int(info.CurrentWeight)),
		updatedState:     "", // empty means that we don't have changes missing to be applied
	}
}

// ApplyChanges applies all the local backend server changes.
func (s *backendServer) ApplyChanges(backendName templaterouter.ServiceAliasConfigKey, client HAProxyClient) error {
	// Build the haproxy dynamic config API commands.
	commands := []string{}

	cmdPrefix := fmt.Sprintf("%s %s/%s", SetServerCommand, string(backendName), s.Name)

	if s.updatedIPAddress != s.IPAddress || s.updatedPort != s.Port {
		cmd := fmt.Sprintf("%s addr %s", cmdPrefix, s.updatedIPAddress)
		if s.updatedPort != s.Port {
			cmd = fmt.Sprintf("%s port %v", cmd, s.updatedPort)
		}
		commands = append(commands, cmd)
	}

	if s.updatedWeight != strconv.Itoa(int(s.CurrentWeight)) {
		// Build and execute the haproxy dynamic config API command.
		cmd := fmt.Sprintf("%s weight %s", cmdPrefix, s.updatedWeight)
		commands = append(commands, cmd)
	}

	if s.updatedState != "" {
		cmd := fmt.Sprintf("%s state %s", cmdPrefix, s.updatedState)
		commands = append(commands, cmd)
	}

	// Execute all the commands.
	for _, cmd := range commands {
		if err := s.executeCommand(cmd, client); err != nil {
			return err
		}
	}

	return nil
}

// executeCommand runs a server change command and handles the response.
func (s *backendServer) executeCommand(cmd string, client HAProxyClient) error {
	responseBytes, err := client.Execute(cmd)
	if err != nil {
		return err
	}

	response := strings.TrimSpace(string(responseBytes))
	if len(response) == 0 {
		return nil
	}

	okPrefixes := []string{"IP changed from", "no need to change"}
	for _, prefix := range okPrefixes {
		if strings.HasPrefix(response, prefix) {
			return nil
		}
	}

	return fmt.Errorf("setting server info with %s : %s", cmd, response)
}

// stripVersionNumber strips off the first line if it is a version number.
func stripVersionNumber(data []byte) ([]byte, error) {
	// The first line contains the version number, so we need to strip
	// that off in order to use the CSV converter.
	//  Example:
	//  > show servers state be_sni
	//  1
	//  # be_id be_name srv_id srv_name ... srv_fqdn srv_port
	//  4 be_sni 1 fe_sni 127.0.0.1 2 0 1 1 46518 1 0 2 0 0 0 0 - 10444
	//
	idx := bytes.Index(data, []byte("\n"))
	if idx > -1 {
		version := string(data[:idx])
		if _, err := strconv.ParseInt(version, 10, 0); err == nil {
			if idx+1 < len(data) {
				return data[idx+1:], nil
			}
		}
	}

	return data, nil
}

type apiType int

const (
	apiAddServer apiType = iota
	apiDelServer
	apiSetHealth
	apiSetServerAddr
	apiSetServerWeight
	apiSetServerState
)

func execCommand(client HAProxyClient, api apiType, cmd string) error {
	responseRaw, err := client.Execute(cmd)
	if err != nil {
		return err
	}
	response := strings.TrimSpace(string(responseRaw))
	if len(response) == 0 {
		return nil
	}

	var valid bool
	switch api {
	case apiAddServer:
		valid = response == "New server registered."
	case apiDelServer:
		valid = response == "Server deleted."
	case apiSetServerAddr:
		valid = response == "nothing changed" || strings.HasPrefix(response, "IP changed from ") || strings.HasPrefix(response, "port changed from ") || strings.HasPrefix(response, "no need to change ")
	case apiSetHealth, apiSetServerWeight, apiSetServerState:
		valid = false // any response from these api calls mean there is a failure
	default:
		// fail fast in case of a dev error
		panic(fmt.Errorf("invalid cmd ID: %d", api))
	}

	if !valid {
		return fmt.Errorf("unexpected response from haproxy: %s", response)
	}
	return nil
}
