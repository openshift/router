package haproxy

import (
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	routev1 "github.com/openshift/api/route/v1"

	"github.com/openshift/router/pkg/router/routeapihelpers"
	templaterouter "github.com/openshift/router/pkg/router/template"
	templateutil "github.com/openshift/router/pkg/router/template/util"

	logf "github.com/openshift/router/log"
)

var log = logf.Logger.WithName("manager")

const (
	// haproxyManagerName is the name of this config manager.
	haproxyManagerName = "haproxy-manager"

	// haproxyRunDir is the name of run directory within the haproxy
	// router working directory heirarchy.
	haproxyRunDir = "run"

	// haproxySocketFile is the name of haproxy socket file.
	haproxySocketFile = "haproxy.sock"

	// haproxyConnectionTimeout is the timeout (in seconds) used for
	// preventing slow connections to the haproxy socket from blocking
	// the config manager from doing any work.
	haproxyConnectionTimeout = 30

	// blueprintRoutePoolNamePrefix is the prefix used for the managed
	// pool of blueprint routes.
	blueprintRoutePoolNamePrefix = "_hapcm_blueprint_pool"

	// dynamicServerPrefix is the prefix used in the haproxy template
	// for adding dynamic servers (pods) to a backend.
	dynamicServerPrefix = "_dynamic"

	// routePoolSizeAnnotation is the annotation on the blueprint route
	// overriding the default pool size.
	routePoolSizeAnnotation = "router.openshift.io/pool-size"

	// We can only manage endpoint changes (servers upto a limit) and
	// can't really dynamically add backends via the haproxy Dynamic
	// Configuration API. So what we need to do is pre-allocate backends
	// based on the different route blueprints. And we can then enable
	// those later when a route is actually added. These constants
	// control the pool namespace & service name to use.
	blueprintRoutePoolNamespace   = blueprintRoutePoolNamePrefix
	blueprintRoutePoolServiceName = blueprintRoutePoolNamePrefix + ".svc"
)

// endpointToDynamicServerMap is a map of endpoint to dynamic server names.
type endpointToDynamicServerMap map[string]string

// configEntryMap is a map containing name-value pairs representing the
// config entries to add to an haproxy map.
type configEntryMap map[string]templaterouter.ServiceAliasConfigKey

// haproxyMapAssociation is a map of haproxy maps and their config entries for a backend.
type haproxyMapAssociation map[string]configEntryMap

// routeBackendEntry is the entry for a route and its associated backend.
type routeBackendEntry struct {
	// id is the route id.
	id string

	// termination is the route termination.
	termination routev1.TLSTerminationType

	// wildcard indicates if the route is a wildcard route.
	wildcard bool

	// BackendName is the name of the associated haproxy backend.
	backendName templaterouter.ServiceAliasConfigKey

	// mapAssociations is the associated set of haproxy maps and their
	// config entries.
	mapAssociations haproxyMapAssociation

	// poolRouteBackendName is backend name for any associated route
	// from the pre-configured blueprint route pool.
	poolRouteBackendName templaterouter.ServiceAliasConfigKey

	// DynamicServerMap is a map of all the allocated dynamic servers.
	dynamicServerMap endpointToDynamicServerMap
}

// haproxyConfigManager is a template router config manager implementation
// that supports changing haproxy configuration dynamically via the haproxy
// dynamic configuration API.
type haproxyConfigManager struct {
	// connectionInfo specifies how to connect to haproxy.
	connectionInfo string

	// commitInterval controls how often we call commit to write out
	// (to the actual config) all the changes made via the haproxy
	// dynamic configuration API.
	commitInterval time.Duration

	// blueprintRoutes are the blueprint routes used for pre-allocation.
	blueprintRoutes []*routev1.Route

	// blueprintRoutePoolSize is the size of the pre-allocated pool of
	// backends for each route blueprint.
	blueprintRoutePoolSize int

	// maxDynamicServers is the maximum number of dynamic servers
	// allocated per backend in the haproxy template configuration.
	maxDynamicServers int

	// wildcardRoutesAllowed indicates if wildcard routes are allowed.
	wildcardRoutesAllowed bool

	// extendedValidation indicates if extended route validation is enabled.
	extendedValidation bool

	// router is the associated template router.
	router templaterouter.RouterInterface

	// defaultCertificate is the default certificate bytes.
	defaultCertificate string

	// client is the client used to dynamically manage haproxy.
	client *Client

	// reloadInProgress indicates if a router reload is in progress.
	reloadInProgress bool

	// backendEntries is a map of route id to the route backend entry.
	backendEntries map[templaterouter.ServiceAliasConfigKey]*routeBackendEntry

	// poolUsage is a mapping of blueprint route pool entries to their
	// corresponding routes.
	poolUsage map[templaterouter.ServiceAliasConfigKey]templaterouter.ServiceAliasConfigKey

	// lock is a mutex used to prevent concurrent config changes.
	lock sync.Mutex

	// commitTimer indicates if a router config commit is pending.
	commitTimer *time.Timer
}

// NewHAProxyConfigManager returns a new haproxyConfigManager.
func NewHAProxyConfigManager(options templaterouter.ConfigManagerOptions) *haproxyConfigManager {
	client := NewClient(options.ConnectionInfo, haproxyConnectionTimeout)

	log.V(2).Info("creating new manager", "manager", haproxyManagerName, "options", options)

	return &haproxyConfigManager{
		connectionInfo:         options.ConnectionInfo,
		commitInterval:         options.CommitInterval,
		blueprintRoutes:        buildBlueprintRoutes(options.BlueprintRoutes, options.ExtendedValidation),
		blueprintRoutePoolSize: options.BlueprintRoutePoolSize,
		maxDynamicServers:      options.MaxDynamicServers,
		wildcardRoutesAllowed:  options.WildcardRoutesAllowed,
		extendedValidation:     options.ExtendedValidation,
		defaultCertificate:     "",

		client:           client,
		reloadInProgress: false,
		backendEntries:   make(map[templaterouter.ServiceAliasConfigKey]*routeBackendEntry),
		poolUsage:        make(map[templaterouter.ServiceAliasConfigKey]templaterouter.ServiceAliasConfigKey),
	}
}

// Initialize initializes the haproxy config manager.
func (cm *haproxyConfigManager) Initialize(router templaterouter.RouterInterface, certPath string) {
	certBytes := []byte{}
	if len(certPath) > 0 {
		if b, err := ioutil.ReadFile(certPath); err != nil {
			log.Error(err, "loading router default certificate", "certPath", certPath)
		} else {
			certBytes = b
		}
	}

	cm.lock.Lock()
	cm.router = router
	cm.defaultCertificate = string(certBytes)
	blueprints := cm.blueprintRoutes
	cm.lock.Unlock()

	// Ensure this is done outside of the lock as the router will call
	// back into the manager code for all the routes we provision.
	for _, r := range blueprints {
		cm.provisionRoutePool(r)
	}

	log.V(2).Info("haproxy Config Manager router will flush out any dynamically configured changes within some interval of each other", "interval", cm.commitInterval.String())
}

// AddBlueprint adds a new (or replaces an existing) route blueprint.
func (cm *haproxyConfigManager) AddBlueprint(route *routev1.Route) error {
	newRoute := route.DeepCopy()
	newRoute.Namespace = blueprintRoutePoolNamespace
	newRoute.Spec.Host = ""

	if cm.extendedValidation {
		if err := routeapihelpers.ExtendedValidateRoute(newRoute).ToAggregate(); err != nil {
			return err
		}
	}

	cm.lock.Lock()
	existingBlueprints := cm.blueprintRoutes
	cm.lock.Unlock()

	routeExists := false
	updated := false
	blueprints := make([]*routev1.Route, 0)
	for _, r := range existingBlueprints {
		if r.Namespace == newRoute.Namespace && r.Name == newRoute.Name {
			// Existing route, check if if anything changed,
			// other than the host name.
			routeExists = true
			newRoute.Spec.Host = r.Spec.Host
			if !reflect.DeepEqual(r, newRoute) {
				updated = true
				blueprints = append(blueprints, newRoute)
				continue
			}
		}
		blueprints = append(blueprints, r)
	}

	if !routeExists {
		blueprints = append(blueprints, newRoute)
		updated = true
	}

	if !updated {
		return nil
	}

	cm.lock.Lock()
	cm.blueprintRoutes = blueprints
	cm.lock.Unlock()

	cm.provisionRoutePool(newRoute)
	return nil
}

// RemoveBlueprint removes a route blueprint.
func (cm *haproxyConfigManager) RemoveBlueprint(route *routev1.Route) {
	deletedRoute := route.DeepCopy()
	deletedRoute.Namespace = blueprintRoutePoolNamespace

	cm.lock.Lock()
	existingBlueprints := cm.blueprintRoutes
	cm.lock.Unlock()

	updated := false
	blueprints := make([]*routev1.Route, 0)
	for _, r := range existingBlueprints {
		if r.Namespace == deletedRoute.Namespace && r.Name == deletedRoute.Name {
			updated = true
		} else {
			blueprints = append(blueprints, r)
		}
	}

	if !updated {
		return
	}

	cm.lock.Lock()
	cm.blueprintRoutes = blueprints
	cm.lock.Unlock()

	cm.removeRoutePool(deletedRoute)
}

// Register registers an id with an expected haproxy backend for a route.
func (cm *haproxyConfigManager) Register(id templaterouter.ServiceAliasConfigKey, route *routev1.Route) {
	wildcard := cm.wildcardRoutesAllowed && (route.Spec.WildcardPolicy == routev1.WildcardPolicySubdomain)
	entry := &routeBackendEntry{
		id:               string(id),
		termination:      routeTerminationType(route),
		wildcard:         wildcard,
		backendName:      routeBackendName(id, route),
		dynamicServerMap: make(endpointToDynamicServerMap),
	}

	cm.lock.Lock()
	defer cm.lock.Unlock()

	entry.BuildMapAssociations(route)
	cm.backendEntries[id] = entry
}

// AddRoute adds a new route or updates an existing route.
func (cm *haproxyConfigManager) AddRoute(id templaterouter.ServiceAliasConfigKey, routingKey string, route *routev1.Route) error {
	if cm.isReloading() {
		return fmt.Errorf("Router reload in progress, cannot dynamically add route %s", id)
	}

	log.V(2).Info("adding route", "id", id)

	if cm.isManagedPoolRoute(route) {
		return fmt.Errorf("managed pool blueprint route %s ignored", id)
	}

	matchedBlueprint := cm.findMatchingBlueprint(route)
	if matchedBlueprint == nil {
		return fmt.Errorf("no blueprint found that would match route %s/%s", route.Namespace, route.Name)
	}

	cm.Register(id, route)

	cm.lock.Lock()
	defer func() {
		cm.lock.Unlock()
		cm.scheduleRouterReload()
	}()

	slotName, err := cm.findFreeBackendPoolSlot(matchedBlueprint)
	if err != nil {
		return fmt.Errorf("finding free backend pool slot for route %s: %v", id, err)
	}

	log.V(2).Info("adding route using blueprint pool", "id", id, "slot", slotName)
	entry, ok := cm.backendEntries[id]
	if !ok {
		// Should always find backend info but ...
		return fmt.Errorf("route id %s was not registered", id)
	}

	// Update mapping to use the free pool slot, set the pool entry
	// name and process the map associations.
	// Note here that we need to rebuild the map associations since
	// those depend on the backend name (or the free slot name now).
	cm.poolUsage[slotName] = id
	entry.poolRouteBackendName = slotName
	entry.BuildMapAssociations(route)

	if err := cm.addMapAssociations(entry.mapAssociations); err != nil {
		return fmt.Errorf("adding map associations for id %s: %v", id, err)
	}

	backendName := entry.BackendName()
	log.V(2).Info("finding backend", "name", backendName)
	backend, err := cm.client.FindBackend(backendName)
	if err != nil {
		return err
	}

	log.V(2).Info("setting routing key", "name", backendName)
	if err := backend.SetRoutingKey(routingKey); err != nil {
		return err
	}

	log.V(2).Info("route added using blueprint pool", "id", id, "slot", slotName)
	return nil
}

// RemoveRoute removes a route.
func (cm *haproxyConfigManager) RemoveRoute(id templaterouter.ServiceAliasConfigKey, route *routev1.Route) error {
	log.V(2).Info("removing route", "id", id)
	if cm.isReloading() {
		return fmt.Errorf("Router reload in progress, cannot dynamically remove route id %s", id)
	}

	if cm.isManagedPoolRoute(route) {
		return fmt.Errorf("managed pool blueprint route %s ignored", id)
	}

	cm.lock.Lock()
	defer func() {
		cm.lock.Unlock()
		cm.scheduleRouterReload()
	}()

	entry, ok := cm.backendEntries[id]
	if !ok {
		// Not registered - return error back.
		return fmt.Errorf("route id %s was not registered", id)
	}

	backendName := entry.BackendName()
	log.V(2).Info("removing backend", "id", id, "backend", backendName)

	// Remove the associated haproxy map entries.
	if err := cm.removeMapAssociations(entry.mapAssociations); err != nil {
		log.V(0).Info("continuing despite errors removing backend map associations", "backend", backendName, "error", err)
	}

	// Remove pool usage entry for a route added in.
	if len(entry.poolRouteBackendName) > 0 {
		delete(cm.poolUsage, entry.poolRouteBackendName)
	}

	// Delete entry for route id to backend info.
	delete(cm.backendEntries, id)

	// Finally, disable all the servers.
	log.V(2).Info("finding backend", "backend", backendName)
	backend, err := cm.client.FindBackend(backendName)
	if err != nil {
		return err
	}
	log.V(2).Info("disabling all servers for backend", "backend", backendName)
	if err := backend.Disable(); err != nil {
		return err
	}

	log.V(2).Info("committing changes made to backend", "backend", backendName)
	return backend.Commit()
}

// ReplaceRouteEndpoints dynamically replaces a subset of the endpoints for
// a route - modifies a subset of the servers on an haproxy backend.
func (cm *haproxyConfigManager) ReplaceRouteEndpoints(id templaterouter.ServiceAliasConfigKey, oldEndpoints, newEndpoints []templaterouter.Endpoint, weight int32) error {
	log.V(2).Info("replacing route endpoints", "id", id, "weight", weight)
	if cm.isReloading() {
		return fmt.Errorf("Router reload in progress, cannot dynamically add endpoints for %s", id)
	}

	configChanged := false
	cm.lock.Lock()
	defer func() {
		cm.lock.Unlock()
		if configChanged {
			cm.scheduleRouterReload()
		}
	}()

	entry, ok := cm.backendEntries[id]
	if !ok {
		// Not registered - return error back.
		return fmt.Errorf("route id %s was not registered", id)
	}

	weightIsRelative := false
	if entry.termination == routev1.TLSTerminationPassthrough {
		// Passthrough is a wee bit odd and is like a boolean on/off
		// switch. Setting actual weights, causing the haproxy
		// dynamic API to either hang or then haproxy dying off.
		// So 100% works for us today because we use a dynamic hash
		// balancing algorithm. Needs a follow up on this issue.
		weightIsRelative = true
		weight = 100
	}

	backendName := entry.BackendName()
	log.V(2).Info("finding backend", "backend", backendName)
	backend, err := cm.client.FindBackend(backendName)
	if err != nil {
		return err
	}

	modifiedEndpoints := make(map[string]templaterouter.Endpoint)
	for _, ep := range newEndpoints {
		modifiedEndpoints[ep.ID] = ep
	}

	deletedEndpoints := make(map[string]templaterouter.Endpoint)
	for _, ep := range oldEndpoints {
		if v2ep, ok := modifiedEndpoints[ep.ID]; ok {
			if reflect.DeepEqual(ep, v2ep) {
				// endpoint was unchanged.
				delete(modifiedEndpoints, v2ep.ID)
			}
			if ep.AppProtocol != v2ep.AppProtocol && (ep.AppProtocol == "h2c" || v2ep.AppProtocol == "h2c") {
				return fmt.Errorf("endpoint %s changed appProtocol from %q to %q, and dynamically updating proto is unsupported", ep.ID, ep.AppProtocol, v2ep.AppProtocol)
			}
		} else {
			configChanged = true
			deletedEndpoints[ep.ID] = ep
		}
	}

	log.V(2).Info("getting servers for backend", "backend", backendName)
	servers, err := backend.Servers()
	if err != nil {
		return err
	}

	log.V(2).Info("processing endpoint changes", "deleted", deletedEndpoints, "modified", modifiedEndpoints)

	// First process the deleted endpoints and update the servers we
	// have already used - these would be the ones where the name
	// matches the endpoint name or is a dynamic server already in use.
	// Also keep track of the unused dynamic servers.
	unusedServerNames := []string{}
	for _, s := range servers {
		relatedEndpointID := s.Name
		if isDynamicBackendServer(s) {
			if epid, ok := entry.dynamicServerMap[s.Name]; ok {
				relatedEndpointID = epid
			} else {
				unusedServerNames = append(unusedServerNames, s.Name)
				continue
			}
		}

		if _, ok := deletedEndpoints[relatedEndpointID]; ok {
			configChanged = true
			log.V(2).Info("disabling server for deleted endpoint", "endpoint", relatedEndpointID, "server", s.Name)
			backend.DisableServer(s.Name)
			if _, ok := entry.dynamicServerMap[s.Name]; ok {
				log.V(2).Info("removing server from dynamic server map", "server", s.Name, "backend", backendName)
				delete(entry.dynamicServerMap, s.Name)
			}
			continue
		}

		if ep, ok := modifiedEndpoints[relatedEndpointID]; ok {
			configChanged = true
			log.V(2).Info("enabling server for modified endpoint", "endpoint", relatedEndpointID, "server", s.Name, "ip", ep.IP, "port", ep.Port, "appProtocol", ep.AppProtocol, "weight", weight)
			backend.UpdateServerInfo(s.Name, ep.IP, ep.Port, ep.AppProtocol, weight, weightIsRelative)
			backend.EnableServer(s.Name)

			delete(modifiedEndpoints, relatedEndpointID)
		}
	}

	// Processed all existing endpoints, now check if there's any more
	// more modified endpoints (aka newly added ones). For these, we can
	// choose any of the unused dynamic servers.
	for _, name := range unusedServerNames {
		if len(modifiedEndpoints) == 0 {
			break
		}

		var ep templaterouter.Endpoint
		for _, v := range modifiedEndpoints {
			// Just get first modified endpoint.
			ep = v
			break
		}

		// Add entry for the dyamic server used.
		configChanged = true
		entry.dynamicServerMap[name] = ep.ID

		log.V(2).Info("enabling server for added endpoint", "endpoint", ep.ID, "server", name, "ip", ep.IP, "port", ep.Port, "appProtocol", ep.AppProtocol, "weight", weight)
		backend.UpdateServerInfo(name, ep.IP, ep.Port, ep.AppProtocol, weight, weightIsRelative)
		backend.EnableServer(name)

		delete(modifiedEndpoints, ep.ID)
	}

	// If we got here, then either we are done with all the endpoints or
	// there are no free dynamic server slots available that we can use.
	if len(modifiedEndpoints) > 0 {
		return fmt.Errorf("no free dynamic server slots for backend %s, %d endpoint(s) remaining",
			id, len(modifiedEndpoints))
	}

	log.V(2).Info("committing backend", "backend", backendName)
	return backend.Commit()
}

// RemoveRouteEndpoints removes servers matching the endpoints from a haproxy backend.
func (cm *haproxyConfigManager) RemoveRouteEndpoints(id templaterouter.ServiceAliasConfigKey, endpoints []templaterouter.Endpoint) error {
	log.V(2).Info("removing endpoints", "id", id)
	if cm.isReloading() {
		return fmt.Errorf("Router reload in progress, cannot dynamically delete endpoints for %s", id)
	}

	cm.lock.Lock()
	defer func() {
		cm.lock.Unlock()
		cm.scheduleRouterReload()
	}()

	entry, ok := cm.backendEntries[id]
	if !ok {
		// Not registered - return error back.
		return fmt.Errorf("route id %s was not registered", id)
	}

	backendName := entry.BackendName()
	log.V(2).Info("finding backend", "backend", backendName)
	backend, err := cm.client.FindBackend(backendName)
	if err != nil {
		return err
	}

	// Build a reversed map (endpoint id -> server name) to allow us to
	// search by endpoint.
	endpointToDynServerMap := make(map[string]string)
	for serverName, endpointID := range entry.dynamicServerMap {
		endpointToDynServerMap[endpointID] = serverName
	}

	for _, ep := range endpoints {
		name := ep.ID
		if serverName, ok := endpointToDynServerMap[ep.ID]; ok {
			name = serverName
			delete(entry.dynamicServerMap, name)
		}

		log.V(2).Info("disabling server for endpoint", "endpoint", ep.ID, "server", name)
		backend.DisableServer(name)
	}

	log.V(2).Info("committing backend", "backend", backendName)
	return backend.Commit()
}

// Notify informs the config manager of any template router state changes.
// We only care about the reload specific events.
func (cm *haproxyConfigManager) Notify(event templaterouter.RouterEventType) {
	log.V(2).Info("received notification", "event", string(event))

	cm.lock.Lock()
	defer cm.lock.Unlock()

	switch event {
	case templaterouter.RouterEventReloadStart:
		cm.reloadInProgress = true
	case templaterouter.RouterEventReloadError:
		cm.reloadInProgress = false
	case templaterouter.RouterEventReloadEnd:
		cm.reloadInProgress = false
		cm.reset()
	}
}

// Commit commits the configuration and reloads the associated router.
func (cm *haproxyConfigManager) Commit() {
	log.V(2).Info("committing dynamic config manager changes")
	cm.commitRouterConfig()
}

// ServerTemplateName returns the dynamic server template name.
func (cm *haproxyConfigManager) ServerTemplateName(id templaterouter.ServiceAliasConfigKey) string {
	if cm.maxDynamicServers > 0 {
		// Adding the id makes the name unwieldy - use pod.
		return fmt.Sprintf("%s-pod", dynamicServerPrefix)
	}

	return ""
}

// ServerTemplateSize returns the dynamic server template size.
// Note this is returned as a string for easier use in the haproxy template.
func (cm *haproxyConfigManager) ServerTemplateSize(id templaterouter.ServiceAliasConfigKey) string {
	if cm.maxDynamicServers < 1 {
		return ""
	}

	return fmt.Sprintf("%v", cm.maxDynamicServers)
}

// GenerateDynamicServerNames generates the dynamic server names.
func (cm *haproxyConfigManager) GenerateDynamicServerNames(id templaterouter.ServiceAliasConfigKey) []string {
	if cm.maxDynamicServers > 0 {
		if prefix := cm.ServerTemplateName(id); len(prefix) > 0 {
			names := make([]string, cm.maxDynamicServers)
			for i := 0; i < cm.maxDynamicServers; i++ {
				names[i] = fmt.Sprintf("%s-%v", prefix, i+1)
			}
			return names
		}
	}

	return []string{}
}

// scheduleRouterReload schedules a reload by deferring commit on the
// associated template router using a internal flush timer.
func (cm *haproxyConfigManager) scheduleRouterReload() {
	cm.lock.Lock()
	defer cm.lock.Unlock()
	if cm.commitTimer == nil {
		cm.commitTimer = time.AfterFunc(cm.commitInterval, cm.commitRouterConfig)
	}
}

// commitRouterConfig calls Commit on the associated template router.
func (cm *haproxyConfigManager) commitRouterConfig() {
	cm.lock.Lock()
	cm.commitTimer = nil
	cm.lock.Unlock()

	// Adding (+removing) a new blueprint pool route triggers a router state
	// change. And calling Commit ensures that the config gets written out.
	route := createBlueprintRoute(routev1.TLSTerminationEdge)
	route.Name = fmt.Sprintf("%s-temp-%d", route.Name, time.Now().Unix())
	cm.router.AddRoute(route)
	cm.router.RemoveRoute(route)

	log.V(2).Info("committing associated template router")
	cm.router.Commit()
}

// reloadInProgress indicates if a router reload is in progress.
func (cm *haproxyConfigManager) isReloading() bool {
	cm.lock.Lock()
	defer cm.lock.Unlock()

	return cm.reloadInProgress
}

// isManagedPoolRoute indicates if a given route is a route from the managed
// pool of blueprint routes.
func (cm *haproxyConfigManager) isManagedPoolRoute(route *routev1.Route) bool {
	return route.Namespace == blueprintRoutePoolNamespace
}

// provisionRoutePool provisions a pre-allocated pool of routes based on a blueprint.
func (cm *haproxyConfigManager) provisionRoutePool(blueprint *routev1.Route) {
	poolSize := getPoolSize(blueprint, cm.blueprintRoutePoolSize)
	log.V(0).Info("provisioning blueprint route pool", "namespace", blueprint.Namespace, "name", blueprint.Name, "size", poolSize)
	for i := 0; i < poolSize; i++ {
		route := blueprint.DeepCopy()
		route.Namespace = blueprintRoutePoolNamespace
		route.Name = fmt.Sprintf("%v-%v", route.Name, i+1)
		route.Spec.Host = ""
		cm.router.AddRoute(route)
	}
}

// removeRoutePool removes a pre-allocated pool of routes based on a blueprint.
func (cm *haproxyConfigManager) removeRoutePool(blueprint *routev1.Route) {
	poolSize := getPoolSize(blueprint, cm.blueprintRoutePoolSize)
	log.V(0).Info("removing blueprint route pool", "namespace", blueprint.Namespace, "name", blueprint.Name, "size", poolSize)
	for i := 0; i < poolSize; i++ {
		route := blueprint.DeepCopy()
		route.Namespace = blueprintRoutePoolNamespace
		route.Name = fmt.Sprintf("%v-%v", route.Name, i+1)
		route.Spec.Host = ""
		cm.router.RemoveRoute(route)
	}
}

// processMapAssociations processes all the map associations for a backend.
func (cm *haproxyConfigManager) processMapAssociations(associations haproxyMapAssociation, add bool) error {
	log.V(2).Info("processing map associations", "associations", associations)

	haproxyMaps, err := cm.client.Maps()
	if err != nil {
		return err
	}

	for _, ham := range haproxyMaps {
		name := path.Base(ham.Name())
		if entries, ok := associations[name]; ok {
			log.V(2).Info("applying to map", "name", name, "entries", entries)
			if err := applyMapAssociations(ham, entries, add); err != nil {
				return err
			}
		}
	}

	return nil
}

// findFreeBackendPoolSlot returns a free pool slot backend name.
func (cm *haproxyConfigManager) findFreeBackendPoolSlot(blueprint *routev1.Route) (templaterouter.ServiceAliasConfigKey, error) {
	poolSize := getPoolSize(blueprint, cm.blueprintRoutePoolSize)
	idPrefix := fmt.Sprintf("%s:%s", blueprint.Namespace, blueprint.Name)
	for i := 0; i < poolSize; i++ {
		id := templaterouter.ServiceAliasConfigKey(fmt.Sprintf("%s-%v", idPrefix, i+1))
		name := routeBackendName(id, blueprint)
		if _, ok := cm.poolUsage[name]; !ok {
			return name, nil
		}
	}

	return "", fmt.Errorf("no %s free pool slot available", idPrefix)
}

// addMapAssociations adds all the map associations for a backend.
func (cm *haproxyConfigManager) addMapAssociations(m haproxyMapAssociation) error {
	return cm.processMapAssociations(m, true)
}

// removeMapAssociations removes all the map associations for a backend.
func (cm *haproxyConfigManager) removeMapAssociations(m haproxyMapAssociation) error {
	return cm.processMapAssociations(m, false)
}

// reset resets the haproxy dynamic configuration manager to a pristine
// state. Clears out any allocated pool backends and dynamic servers.
func (cm *haproxyConfigManager) reset() {
	if cm.commitTimer != nil {
		commitTimer := cm.commitTimer
		defer func() {
			commitTimer.Stop()
		}()

		cm.commitTimer = nil
	}

	// Reset the blueprint route pool use and dynamic server maps as
	// the router was reloaded.
	cm.poolUsage = make(map[templaterouter.ServiceAliasConfigKey]templaterouter.ServiceAliasConfigKey)
	for _, entry := range cm.backendEntries {
		entry.poolRouteBackendName = ""
		if len(entry.dynamicServerMap) > 0 {
			entry.dynamicServerMap = make(endpointToDynamicServerMap)
		}
	}

	// Reset the client - clear its caches.
	cm.client.Reset()
}

// findMatchingBlueprint finds a matching blueprint route that can be used
// as a "surrogate" for the route.
func (cm *haproxyConfigManager) findMatchingBlueprint(route *routev1.Route) *routev1.Route {
	termination := routeTerminationType(route)
	routeModifiers := backendModAnnotations(route)
	for _, candidate := range cm.blueprintRoutes {
		t2 := routeTerminationType(candidate)
		if termination != t2 {
			// not the day of judgement!
			continue
		}

		if len(routeModifiers) > 0 {
			if len(candidate.Annotations) == 0 {
				// Can't use this blueprint as it has no annotations.
				continue
			}

			candidateModifiers := backendModAnnotations(candidate)
			if !reflect.DeepEqual(routeModifiers, candidateModifiers) {
				continue
			}
		}

		// Ok we passed termination and annotation checks. Need to
		// pass the the certification tests aka no special
		// certificate information.
		if route.Spec.TLS == nil && candidate.Spec.TLS == nil {
			return candidate
		}
		tlsSpec := route.Spec.TLS
		if tlsSpec == nil {
			tlsSpec = &routev1.TLSConfig{Termination: routev1.TLSTerminationType("")}
		}
		if tlsSpec != nil && candidate.Spec.TLS != nil {
			// So we need compare the TLS fields but don't care
			// if InsecureEdgeTerminationPolicy doesn't match.
			candidateCopy := candidate.DeepCopy()
			candidateCopy.Spec.TLS.InsecureEdgeTerminationPolicy = tlsSpec.InsecureEdgeTerminationPolicy
			if reflect.DeepEqual(tlsSpec, candidateCopy.Spec.TLS) {
				return candidateCopy
			}
		}
	}

	return nil
}

// BackendName returns the associated backend name for a route.
func (entry *routeBackendEntry) BackendName() templaterouter.ServiceAliasConfigKey {
	if len(entry.poolRouteBackendName) > 0 {
		return entry.poolRouteBackendName
	}

	return entry.backendName
}

// BuildMapAssociations builds the associations to haproxy maps for a route.
func (entry *routeBackendEntry) BuildMapAssociations(route *routev1.Route) {
	termination := routeTerminationType(route)
	policy := routev1.InsecureEdgeTerminationPolicyNone
	if route.Spec.TLS != nil {
		policy = route.Spec.TLS.InsecureEdgeTerminationPolicy
	}

	entry.mapAssociations = make(haproxyMapAssociation)
	associate := func(name, k string, v templaterouter.ServiceAliasConfigKey) {
		m, ok := entry.mapAssociations[name]
		if !ok {
			m = make(configEntryMap)
		}

		m[k] = v
		entry.mapAssociations[name] = m
	}

	hostspec := route.Spec.Host
	pathspec := route.Spec.Path
	if len(hostspec) == 0 {
		return
	}

	name := entry.BackendName()

	// Do the path specific regular expression usage first.
	pathRE := templateutil.GenerateRouteRegexp(hostspec, pathspec, entry.wildcard)
	if policy == routev1.InsecureEdgeTerminationPolicyRedirect {
		associate("os_route_http_redirect.map", pathRE, name)
	}
	switch termination {
	case routev1.TLSTerminationType(""):
		associate("os_http_be.map", pathRE, name)

	case routev1.TLSTerminationEdge:
		associate("os_edge_reencrypt_be.map", pathRE, name)
		if policy == routev1.InsecureEdgeTerminationPolicyAllow {
			associate("os_http_be.map", pathRE, name)
		}

	case routev1.TLSTerminationReencrypt:
		associate("os_edge_reencrypt_be.map", pathRE, name)
		if policy == routev1.InsecureEdgeTerminationPolicyAllow {
			associate("os_http_be.map", pathRE, name)
		}
	}

	// And then handle the host specific regular expression usage.
	hostRE := templateutil.GenerateRouteRegexp(hostspec, "", entry.wildcard)
	if len(os.Getenv("ROUTER_ALLOW_WILDCARD_ROUTES")) > 0 && entry.wildcard {
		associate("os_wildcard_domain.map", hostRE, "1")
	}
	switch termination {
	case routev1.TLSTerminationReencrypt:
		associate("os_tcp_be.map", hostRE, name)

	case routev1.TLSTerminationPassthrough:
		associate("os_tcp_be.map", hostRE, name)
		associate("os_sni_passthrough.map", hostRE, "1")
	}
}

// buildBlueprintRoutes generates a list of blueprint routes.
func buildBlueprintRoutes(customRoutes []*routev1.Route, validate bool) []*routev1.Route {
	routes := make([]*routev1.Route, 0)

	// Add in defaults based on the different route termination types.
	terminationTypes := []routev1.TLSTerminationType{
		routev1.TLSTerminationType(""),
		routev1.TLSTerminationEdge,
		routev1.TLSTerminationPassthrough,
		// Disable re-encrypt routes for now as we may not be able
		// to validate signers.
		// routeapi.TLSTerminationReencrypt,
	}
	for _, v := range terminationTypes {
		r := createBlueprintRoute(v)
		routes = append(routes, r)
	}

	// Clone and add custom routes to the blueprint route pool namespace.
	for _, r := range customRoutes {
		dolly := r.DeepCopy()
		dolly.Namespace = blueprintRoutePoolNamespace
		if validate {
			if err := routeapihelpers.ExtendedValidateRoute(dolly).ToAggregate(); err != nil {
				log.Error(err, "skipping blueprint route due to invalid configuration", "namespace", r.Namespace, "name", r.Name)
				continue
			}
		}

		routes = append(routes, dolly)
	}

	return routes
}

// generateRouteName generates a name based on the route type.
func generateRouteName(routeType routev1.TLSTerminationType) string {
	prefix := "http"

	switch routeType {
	case routev1.TLSTerminationEdge:
		prefix = "edge"
	case routev1.TLSTerminationPassthrough:
		prefix = "passthrough"
	case routev1.TLSTerminationReencrypt:
		prefix = "reencrypt"
	}

	return fmt.Sprintf("_blueprint-%v-route", prefix)
}

// createBlueprintRoute creates a new blueprint route based on route type.
func createBlueprintRoute(routeType routev1.TLSTerminationType) *routev1.Route {
	name := generateRouteName(routeType)

	return &routev1.Route{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: blueprintRoutePoolNamespace,
			Name:      name,
		},
		Spec: routev1.RouteSpec{
			Host: "",
			TLS:  &routev1.TLSConfig{Termination: routeType},
			To: routev1.RouteTargetReference{
				Name:   blueprintRoutePoolServiceName,
				Weight: new(int32),
			},
		},
	}
}

// routeBackendName returns the haproxy backend name for a route.
func routeBackendName(id templaterouter.ServiceAliasConfigKey, route *routev1.Route) templaterouter.ServiceAliasConfigKey {
	termination := routeTerminationType(route)
	prefix := templateutil.GenerateBackendNamePrefix(termination)
	return templaterouter.ServiceAliasConfigKey(fmt.Sprintf("%s:%s", prefix, string(id)))
}

// getPoolSize returns the size to allocate for the pool for the specified
// blueprint route. Route annotations if they exist override the defaults.
func getPoolSize(r *routev1.Route, defaultSize int) int {
	v, ok := r.Annotations[routePoolSizeAnnotation]
	if ok {
		if poolSize, err := strconv.ParseInt(v, 10, 0); err != nil {
			return int(poolSize)
		} else {
			routeName := fmt.Sprintf("%s/%s", r.Namespace, r.Name)
			log.V(0).Info("blueprint route has an invalid pool size annotation; using default size",
				"route", routeName, "annotation", v, "defaultSize", defaultSize, "error", err)
		}
	}

	return defaultSize
}

// routeTerminationType returns a termination type for a route.
func routeTerminationType(route *routev1.Route) routev1.TLSTerminationType {
	termination := routev1.TLSTerminationType("")
	if route.Spec.TLS != nil {
		termination = route.Spec.TLS.Termination
	}

	return termination
}

// isDynamicBackendServer indicates if a backend server is a dynamic server.
func isDynamicBackendServer(server BackendServerInfo) bool {
	if len(dynamicServerPrefix) == 0 {
		return false
	}

	return strings.HasPrefix(server.Name, dynamicServerPrefix)
}

// applyMapAssociations applies the backend associations to a haproxy map.
func applyMapAssociations(m *HAProxyMap, associations configEntryMap, add bool) error {
	for k, v := range associations {
		log.V(2).Info("applying to map", "name", m.Name(), "key", k, "value", v, "add", add)
		if add {
			if err := m.Add(k, v, true); err != nil {
				return err
			}
		} else {
			if err := m.Delete(k); err != nil {
				return err
			}
		}

		if err := m.Commit(); err != nil {
			return err
		}
	}

	return nil
}

// backendModAnnotations return the annotations in a route that will
// require custom (or modified) backend configuration in haproxy.
func backendModAnnotations(route *routev1.Route) map[string]string {
	termination := routeTerminationType(route)
	backendModifiers := modAnnotationsList(termination)

	annotations := make(map[string]string)
	for _, name := range backendModifiers {
		if v, ok := route.Annotations[name]; ok {
			annotations[name] = v
		}
	}

	return annotations
}

// modAnnotationsList returns a list of annotations that can modify the
// haproxy config for a backend.
func modAnnotationsList(termination routev1.TLSTerminationType) []string {
	annotations := []string{
		"haproxy.router.openshift.io/balance",
		"haproxy.router.openshift.io/ip_whitelist",
		"haproxy.router.openshift.io/timeout",
		"haproxy.router.openshift.io/rate-limit-connections",
		"haproxy.router.openshift.io/rate-limit-connections.concurrent-tcp",
		"haproxy.router.openshift.io/rate-limit-connections.rate-tcp",
		"haproxy.router.openshift.io/rate-limit-connections.rate-http",
		"haproxy.router.openshift.io/pod-concurrent-connections",
		"router.openshift.io/haproxy.health.check.interval",
	}

	if termination == routev1.TLSTerminationPassthrough {
		return annotations
	}

	annotations = append(annotations, "haproxy.router.openshift.io/disable_cookies")
	annotations = append(annotations, "router.openshift.io/cookie_name")
	annotations = append(annotations, "haproxy.router.openshift.io/hsts_header")
	annotations = append(annotations, "haproxy.router.openshift.io/rewrite-target")
	annotations = append(annotations, "router.openshift.io/cookie-same-site")
	return annotations
}
