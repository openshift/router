package haproxy

import (
	"errors"
	"fmt"
	"os"
	"path"
	"reflect"
	"slices"
	"sync"
	"time"

	routev1 "github.com/openshift/api/route/v1"

	templaterouter "github.com/openshift/router/pkg/router/template"
	templateutil "github.com/openshift/router/pkg/router/template/util"

	logf "github.com/openshift/router/log"
)

var log = logf.Logger.WithName("manager")

const (
	// haproxyManagerName is the name of this config manager.
	haproxyManagerName = "haproxy-manager"

	// haproxyConnectionTimeout is the timeout (in seconds) used for
	// preventing slow connections to the haproxy socket from blocking
	// the config manager from doing any work.
	haproxyConnectionTimeout = 30
)

// configEntryMap is a map containing name-value pairs representing the
// config entries to add to an haproxy map.
type configEntryMap map[string]templaterouter.ServiceAliasConfigKey

// haproxyMapAssociation is a map of haproxy maps and their config entries for a backend.
type haproxyMapAssociation map[string]configEntryMap

// routeBackendEntry is the entry for a route and its associated backend.
type routeBackendEntry struct {
	// id is the route id.
	id string

	//
	backend *templaterouter.ServiceAliasConfig

	// termination is the route termination.
	termination routev1.TLSTerminationType

	// wildcard indicates if the route is a wildcard route.
	wildcard bool

	// BackendName is the name of the associated haproxy backend.
	backendName templaterouter.ServiceAliasConfigKey

	// mapAssociations is the associated set of haproxy maps and their
	// config entries.
	mapAssociations haproxyMapAssociation
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

	// wildcardRoutesAllowed indicates if wildcard routes are allowed.
	wildcardRoutesAllowed bool

	// extendedValidation indicates if extended route validation is enabled.
	extendedValidation bool

	// router is the associated template router.
	router templaterouter.RouterInterface

	// workingDir is the router's working directory containing configuration
	// files, certificates, and other router-managed resources.
	workingDir string

	// defaultCertificate is the default certificate bytes.
	defaultCertificate string

	// defaultDestinationCA is the path to the default CA certificate file used
	// to verify backend server certificates for re-encrypt routes when no
	// route-specific destination CA is configured.
	defaultDestinationCA string

	// client is the client used to dynamically manage haproxy.
	client *Client

	// reloadInProgress indicates if a router reload is in progress.
	reloadInProgress bool

	// backendEntries is a map of route id to the route backend entry.
	backendEntries map[templaterouter.ServiceAliasConfigKey]*routeBackendEntry

	// lock is a mutex used to prevent concurrent config changes.
	lock sync.Mutex

	// commitTimer indicates if a router config commit is pending.
	commitTimer *time.Timer
}

// NewHAProxyConfigManager returns a new haproxyConfigManager.
func NewHAProxyConfigManager(options templaterouter.ConfigManagerOptions) *haproxyConfigManager {
	client := NewClient(options.ConnectionInfo, haproxyConnectionTimeout)

	log.V(4).Info("creating new manager", "manager", haproxyManagerName, "options", options)

	return &haproxyConfigManager{
		connectionInfo:        options.ConnectionInfo,
		commitInterval:        options.CommitInterval,
		wildcardRoutesAllowed: options.WildcardRoutesAllowed,
		extendedValidation:    options.ExtendedValidation,
		workingDir:            options.WorkingDir,
		defaultCertificate:    "",
		defaultDestinationCA:  options.DefaultDestinationCA,

		client:           client,
		reloadInProgress: false,
		backendEntries:   make(map[templaterouter.ServiceAliasConfigKey]*routeBackendEntry),
	}
}

// Initialize initializes the haproxy config manager.
func (cm *haproxyConfigManager) Initialize(router templaterouter.RouterInterface, certPath string) {
	certBytes := []byte{}
	if len(certPath) > 0 {
		if b, err := os.ReadFile(certPath); err != nil {
			log.Error(err, "loading router default certificate", "certPath", certPath)
		} else {
			certBytes = b
		}
	}

	cm.lock.Lock()
	cm.router = router
	cm.defaultCertificate = string(certBytes)
	cm.lock.Unlock()

	log.V(2).Info("haproxy Config Manager router will flush out any dynamically configured changes within some interval of each other", "interval", cm.commitInterval.String())
}

// Register registers an id with an expected haproxy backend for a route.
func (cm *haproxyConfigManager) Register(id templaterouter.ServiceAliasConfigKey, backend *templaterouter.ServiceAliasConfig, route *routev1.Route) {
	wildcard := cm.wildcardRoutesAllowed && (route.Spec.WildcardPolicy == routev1.WildcardPolicySubdomain)
	entry := &routeBackendEntry{
		id:          string(id),
		backend:     backend,
		termination: routeTerminationType(route),
		wildcard:    wildcard,
		backendName: routeBackendName(id, route),
	}

	cm.lock.Lock()
	defer cm.lock.Unlock()

	entry.BuildMapAssociations(route)
	cm.backendEntries[id] = entry
}

// RemoveRoute removes a route.
func (cm *haproxyConfigManager) RemoveRoute(id templaterouter.ServiceAliasConfigKey, route *routev1.Route) error {
	log.V(4).Info("removing route", "id", id)
	if cm.isReloading() {
		return fmt.Errorf("Router reload in progress, cannot dynamically remove route id %s", id)
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

	backendName := entry.backendName
	log.V(4).Info("removing backend", "id", id, "backend", backendName)

	// Remove the associated haproxy map entries.
	if err := cm.removeMapAssociations(entry.mapAssociations); err != nil {
		log.V(0).Info("continuing despite errors removing backend map associations", "backend", backendName, "error", err)
	}

	// Delete entry for route id to backend info.
	delete(cm.backendEntries, id)

	// Finally, disable all the servers.
	log.V(4).Info("finding backend", "backend", backendName)
	backend, err := cm.client.FindBackend(backendName)
	if err != nil {
		return err
	}

	log.V(4).Info("deleting all servers for backend", "backend", backendName)
	servers, err := backend.Servers()
	if err != nil {
		return err
	}
	for _, server := range servers {
		if _, err := backend.DeleteServer(templaterouter.Endpoint{ID: server.Name}); err != nil {
			return err
		}
	}

	log.V(4).Info("committing changes made to backend", "backend", backendName)
	return backend.Commit()
}

// ReplaceRouteEndpoints dynamically replaces a subset of the endpoints for
// a route - modifies a subset of the servers on an haproxy backend.
func (cm *haproxyConfigManager) ReplaceRouteEndpoints(id templaterouter.ServiceAliasConfigKey, svc *templaterouter.ServiceUnit, oldEndpoints, newEndpoints []templaterouter.Endpoint, activeEndpoints int) error {
	log.V(4).Info("replacing route endpoints", "id", id)
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

	backendName := entry.backendName
	log.V(4).Info("finding backend", "backend", backendName)
	backend, err := cm.client.FindBackend(backendName)
	if err != nil {
		return err
	}

	type epPair struct{ oldEP, newEP templaterouter.Endpoint }
	addedEndpoints := make(map[string]templaterouter.Endpoint)
	modifiedEndpoints := make(map[string]epPair)
	for i := range newEndpoints {
		newEP := newEndpoints[i]
		j := slices.IndexFunc(oldEndpoints, func(oldEP templaterouter.Endpoint) bool {
			return oldEP.ID == newEP.ID
		})
		if j >= 0 {
			oldEP := oldEndpoints[j]
			if !reflect.DeepEqual(oldEP, newEP) {
				if oldEP.NoHealthCheck != newEP.NoHealthCheck {
					// This is not a frequent update and it is currently challenging to implement.
					// Taking the simple route for now, stopping here before any dynamic update and ask for a reload.
					// In case this changes, the `len(newEndpoints) == 1` branch in the end of this method
					// should also be revisited, since the `oldEP.NoHealthCheck == newEP.NoHealthCheck`
					// would no longer hold.
					return fmt.Errorf("detected change in idled configuration in service %q, need to reload", svc.Name)
				}
				modifiedEndpoints[newEP.ID] = epPair{oldEP: oldEP, newEP: newEP}
			}
		} else {
			addedEndpoints[newEP.ID] = newEP
		}
	}

	deletedEndpoints := make(map[string]templaterouter.Endpoint)
	for i := range oldEndpoints {
		oldEP := oldEndpoints[i]
		found := slices.ContainsFunc(newEndpoints, func(newEP templaterouter.Endpoint) bool {
			return oldEP.ID == newEP.ID
		})
		if !found {
			deletedEndpoints[oldEP.ID] = oldEP
		}
	}

	// there is a configuration change if any of the tracking maps have endpoint(s)
	configChanged = len(deletedEndpoints) > 0 || len(modifiedEndpoints) > 0 || len(addedEndpoints) > 0

	log.V(4).Info("processing endpoint changes", "added", addedEndpoints, "deleted", deletedEndpoints, "modified", modifiedEndpoints)

	// Aggregating errors instead of failing fast in the first API error. This ensures that the old
	// process has a more accurate configuration in case it lives longer due to persistent connections.
	var errs []error
	for name, ep := range addedEndpoints {
		if err := backend.AddServer(entry.backend, svc, ep, cm.workingDir, cm.defaultDestinationCA); err != nil {
			errs = append(errs, fmt.Errorf("error adding backend server %s: %w", name, err))
		}
	}
	var addedFromUpdate []templaterouter.Endpoint
	for name, epPair := range modifiedEndpoints {
		oldEP := epPair.oldEP
		newEP := epPair.newEP
		if added, err := backend.UpdateServer(entry.backend, svc, oldEP, newEP, entry.termination == routev1.TLSTerminationPassthrough, cm.workingDir, cm.defaultDestinationCA); err != nil {
			errs = append(errs, fmt.Errorf("error updating backend server %s: %w", name, err))
		} else if added {
			addedFromUpdate = append(addedFromUpdate, newEP)
		}
	}
	for name, ep := range deletedEndpoints {
		if _, err := backend.DeleteServer(ep); err != nil {
			errs = append(errs, fmt.Errorf("error deleting backend server %s: %w", name, err))
		}
	}
	if len(errs) > 0 {
		return errors.Join(errs...)
	}

	// Checking health check. We need to:
	// * enable new endpoints if `cfg.ActiveEndpoints > 1`
	// * enable also the only former endpoint if scaling out from 1 to 2 or more
	// * disable the only current endpoint if scaling in to 1
	if activeEndpoints > 1 {
		var newEPs []templaterouter.Endpoint
		for _, ep := range addedEndpoints {
			// enabling for all the new added endpoints
			newEPs = append(newEPs, ep)
		}
		for _, ep := range addedFromUpdate {
			newEPs = append(newEPs, ep)
		}
		if len(oldEndpoints) == 1 {
			ep := oldEndpoints[0]
			_, deleted := deletedEndpoints[ep.ID]
			if !ep.NoHealthCheck && !deleted {
				// The backend was previously in the single server scenario, so health check should be enabled.
				// Dynamically enabling health check only works if health check is configured, and we only
				// configure health check upfront in dynamically added servers.
				// So, we are trying to enable health check first, and if HAProxy responds that it is not
				// configured, we'll need to remove and add it again.
				err := backend.EnableHealthCheck(ep)
				if errors.Is(err, ErrHealthCheckNotConfigured) {
					// Health check not configured on this server. Replace it dynamically to reconfigure with health check.
					err = backend.ReplaceServer(entry.backend, svc, ep, ep, cm.workingDir, cm.defaultDestinationCA)
					if err == nil {
						// Server replaced successfully, mark to enable health check later.
						newEPs = append(newEPs, ep)
					}
				}
				if err != nil {
					// Failed either enabling health check or replacing backend server.
					errs = append(errs, err)
				}
			}
		}
		for _, ep := range newEPs {
			if !ep.NoHealthCheck {
				if err := backend.EnableHealthCheck(ep); err != nil {
					errs = append(errs, err)
				}
			}
		}
	} else if len(newEndpoints) == 1 {
		ep := newEndpoints[0]
		if !ep.NoHealthCheck {
			// the single backend server scenario, health check should be disabled,
			// but only if NoHealthCheck was not flagged. NoHealthCheck is guaranteed
			// not to have changed due to a validation in the `newEndpoints` for-loop above.
			if err := backend.DisableHealthCheck(ep); err != nil {
				errs = append(errs, err)
			}
		}
	}

	return errors.Join(errs...)
}

// RemoveRouteEndpoints removes servers matching the endpoints from a haproxy backend.
func (cm *haproxyConfigManager) RemoveRouteEndpoints(id templaterouter.ServiceAliasConfigKey, endpoints []templaterouter.Endpoint) error {
	log.V(4).Info("removing endpoints", "id", id)
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

	backendName := entry.backendName
	log.V(4).Info("finding backend", "backend", backendName)
	backend, err := cm.client.FindBackend(backendName)
	if err != nil {
		return err
	}

	var errs []error
	for _, ep := range endpoints {
		log.V(4).Info("deleting server for endpoint", "endpoint", ep.ID)
		if _, err := backend.DeleteServer(ep); err != nil {
			errs = append(errs, fmt.Errorf("error deleting server %s: %w", ep.ID, err))
		}
	}

	return errors.Join(errs...)
}

// Notify informs the config manager of any template router state changes.
// We only care about the reload specific events.
func (cm *haproxyConfigManager) Notify(event templaterouter.RouterEventType) {
	log.V(4).Info("received notification", "event", string(event))

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

	log.V(4).Info("committing associated template router")
	cm.router.Commit(true)
}

// reloadInProgress indicates if a router reload is in progress.
func (cm *haproxyConfigManager) isReloading() bool {
	cm.lock.Lock()
	defer cm.lock.Unlock()

	return cm.reloadInProgress
}

// processMapAssociations processes all the map associations for a backend.
func (cm *haproxyConfigManager) processMapAssociations(associations haproxyMapAssociation, add bool) error {
	log.V(4).Info("processing map associations", "associations", associations)

	haproxyMaps, err := cm.client.Maps()
	if err != nil {
		return err
	}

	for _, ham := range haproxyMaps {
		name := path.Base(ham.Name())
		if entries, ok := associations[name]; ok {
			log.V(4).Info("applying to map", "name", name, "entries", entries)
			if err := ham.SyncEntries(entries, add); err != nil {
				return err
			}
		}
	}

	return nil
}

// addMapAssociations adds all the map associations for a backend.
func (cm *haproxyConfigManager) addMapAssociations(m haproxyMapAssociation) error {
	return cm.processMapAssociations(m, true)
}

// removeMapAssociations removes all the map associations for a backend.
func (cm *haproxyConfigManager) removeMapAssociations(m haproxyMapAssociation) error {
	return cm.processMapAssociations(m, false)
}

// reset resets the haproxy dynamic configuration manager to a pristine state.
func (cm *haproxyConfigManager) reset() {
	if cm.commitTimer != nil {
		commitTimer := cm.commitTimer
		defer func() {
			commitTimer.Stop()
		}()

		cm.commitTimer = nil
	}

	// Reset the client - clear its caches.
	cm.client.Reset()
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

	name := entry.backendName

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
	switch termination {
	case routev1.TLSTerminationReencrypt:
		associate("os_tcp_be.map", hostRE, name)

	case routev1.TLSTerminationPassthrough:
		associate("os_tcp_be.map", hostRE, name)
		associate("os_sni_passthrough.map", hostRE, "1")
	}
}

// routeBackendName returns the haproxy backend name for a route.
func routeBackendName(id templaterouter.ServiceAliasConfigKey, route *routev1.Route) templaterouter.ServiceAliasConfigKey {
	termination := routeTerminationType(route)
	prefix := templateutil.GenerateBackendNamePrefix(termination)
	return templaterouter.ServiceAliasConfigKey(fmt.Sprintf("%s:%s", prefix, string(id)))
}

// routeTerminationType returns a termination type for a route.
func routeTerminationType(route *routev1.Route) routev1.TLSTerminationType {
	termination := routev1.TLSTerminationType("")
	if route.Spec.TLS != nil {
		termination = route.Spec.TLS.Termination
	}

	return termination
}
