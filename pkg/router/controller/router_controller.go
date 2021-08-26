package controller

import (
	"context"
	"fmt"
	"sync"
	"time"

	routev1 "github.com/openshift/api/route/v1"
	projectclient "github.com/openshift/client-go/project/clientset/versioned/typed/project/v1"
	kapi "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/sets"
	utilwait "k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/apimachinery/pkg/watch"

	logf "github.com/openshift/router/log"
	"github.com/openshift/router/pkg/router"
	"github.com/openshift/router/pkg/router/controller/endpointsubset"
)

var log = logf.Logger.WithName("controller")

// RouterController abstracts the details of watching resources like Routes, Endpoints, etc.
// used by the plugin implementation.
type RouterController struct {
	lock sync.Mutex

	Plugin router.Plugin

	firstSyncDone bool

	FilteredNamespaceNames sets.String
	NamespaceLabels        labels.Selector
	// Holds Namespace --> RouteName --> RouteObject
	NamespaceRoutes map[string]map[string]*routev1.Route
	// Holds Namespace --> EndpointsName --> EndpointsObject
	NamespaceEndpoints map[string]map[string]*kapi.Endpoints

	ProjectClient       projectclient.ProjectInterface
	ProjectLabels       labels.Selector
	ProjectSyncInterval time.Duration
	ProjectWaitInterval time.Duration
	ProjectRetries      int

	WatchNodes bool
}

// Run begins watching and syncing.
func (c *RouterController) Run() {
	log.V(4).Info("running router controller")
	if c.ProjectLabels != nil {
		c.HandleProjects()
		go utilwait.Forever(c.HandleProjects, c.ProjectSyncInterval)
	}
	c.handleFirstSync()
}

func (c *RouterController) HandleProjects() {
	for i := 0; i < c.ProjectRetries; i++ {
		names, err := c.GetFilteredProjectNames()
		if err == nil {
			// Return early if there is no new change
			if names.Equal(c.FilteredNamespaceNames) {
				return
			}
			c.lock.Lock()
			defer c.lock.Unlock()

			c.FilteredNamespaceNames = names
			c.UpdateNamespaces()
			c.Commit()
			return
		}
		utilruntime.HandleError(fmt.Errorf("unable to get filtered projects for router: %v", err))
		time.Sleep(c.ProjectWaitInterval)
	}
	log.V(4).Info("unable to update list of filtered projects")
}

func (c *RouterController) GetFilteredProjectNames() (sets.String, error) {
	names := sets.String{}
	all, err := c.ProjectClient.List(context.TODO(), metav1.ListOptions{LabelSelector: c.ProjectLabels.String()})
	if err != nil {
		return nil, err
	}
	for _, item := range all.Items {
		names.Insert(item.Name)
	}
	return names, nil
}

func (c *RouterController) processNamespace(eventType watch.EventType, ns *kapi.Namespace) {
	before := c.FilteredNamespaceNames.Has(ns.Name)
	switch eventType {
	case watch.Added, watch.Modified:
		if c.NamespaceLabels.Matches(labels.Set(ns.Labels)) {
			c.FilteredNamespaceNames.Insert(ns.Name)
		} else {
			c.FilteredNamespaceNames.Delete(ns.Name)
		}
	case watch.Deleted:
		c.FilteredNamespaceNames.Delete(ns.Name)
	}
	after := c.FilteredNamespaceNames.Has(ns.Name)

	// Namespace added or deleted
	if (!before && after) || (before && !after) {
		log.V(5).Info("processing matched namespace", "namespace", ns.Name, "labels", ns.Labels)

		c.UpdateNamespaces()

		// New namespace created or router matching labels added to existing namespace
		// Routes for new namespace will be handled by HandleRoute().
		// For existing namespace, add corresponding endpoints/routes as watch endpoints
		// and routes won't be updated till the next resync interval which could be few mins.
		if !before && after {
			if epMap, ok := c.NamespaceEndpoints[ns.Name]; ok {
				for _, ep := range epMap {
					if err := c.Plugin.HandleEndpoints(watch.Modified, ep); err != nil {
						utilruntime.HandleError(err)
					}
				}
			}

			if routeMap, ok := c.NamespaceRoutes[ns.Name]; ok {
				for _, route := range routeMap {
					c.processRoute(watch.Modified, route)
				}
			}
		}
	}
}

func (c *RouterController) UpdateNamespaces() {
	// Note: Need to clone the filtered namespace names or else any updates
	//       we make locally in processNamespace() will be immediately
	//       reflected to plugins in the chain beneath us. This creates
	//       cleanup issues as old == new in Plugin.HandleNamespaces().
	namespaces := sets.NewString(c.FilteredNamespaceNames.List()...)

	log.V(4).Info("updating watched namespaces", "namespaces", namespaces)
	if err := c.Plugin.HandleNamespaces(namespaces); err != nil {
		utilruntime.HandleError(err)
	}
}

func (c *RouterController) RecordNamespaceEndpoints(eventType watch.EventType, ep *kapi.Endpoints) {
	switch eventType {
	case watch.Added, watch.Modified:
		if _, ok := c.NamespaceEndpoints[ep.Namespace]; !ok {
			c.NamespaceEndpoints[ep.Namespace] = make(map[string]*kapi.Endpoints)
		}
		c.NamespaceEndpoints[ep.Namespace][ep.Name] = ep
	case watch.Deleted:
		if _, ok := c.NamespaceEndpoints[ep.Namespace]; ok {
			delete(c.NamespaceEndpoints[ep.Namespace], ep.Name)
			if len(c.NamespaceEndpoints[ep.Namespace]) == 0 {
				delete(c.NamespaceEndpoints, ep.Namespace)
			}
		}
	}
}

func (c *RouterController) RecordNamespaceRoutes(eventType watch.EventType, rt *routev1.Route) {
	switch eventType {
	case watch.Added, watch.Modified:
		if _, ok := c.NamespaceRoutes[rt.Namespace]; !ok {
			c.NamespaceRoutes[rt.Namespace] = make(map[string]*routev1.Route)
		}
		c.NamespaceRoutes[rt.Namespace][rt.Name] = rt
	case watch.Deleted:
		if _, ok := c.NamespaceRoutes[rt.Namespace]; ok {
			delete(c.NamespaceRoutes[rt.Namespace], rt.Name)
			if len(c.NamespaceRoutes[rt.Namespace]) == 0 {
				delete(c.NamespaceRoutes, rt.Namespace)
			}
		}
	}
}

func (c *RouterController) HandleNamespace(eventType watch.EventType, obj interface{}) {
	ns := obj.(*kapi.Namespace)
	c.lock.Lock()
	defer c.lock.Unlock()

	log.V(4).Info("processing namespace", "namespace", ns.Name, "event", eventType)

	c.processNamespace(eventType, ns)
	c.Commit()
}

// HandleNode handles a single Node event and synchronizes the router backend
func (c *RouterController) HandleNode(eventType watch.EventType, obj interface{}) {
	node := obj.(*kapi.Node)
	c.lock.Lock()
	defer c.lock.Unlock()

	log.V(4).Info("processing node", "node", node.Name, "event", eventType)

	if err := c.Plugin.HandleNode(eventType, node); err != nil {
		utilruntime.HandleError(err)
	}
}

// HandleRoute handles a single Route event and synchronizes the router backend.
func (c *RouterController) HandleRoute(eventType watch.EventType, obj interface{}) {
	route := obj.(*routev1.Route)
	c.lock.Lock()
	defer c.lock.Unlock()

	c.processRoute(eventType, route)
	c.Commit()
}

// HandleEndpoints handles a single Endpoints event and refreshes the router backend.
func (c *RouterController) HandleEndpoints(eventType watch.EventType, obj interface{}) {
	endpoints := obj.(*kapi.Endpoints)
	c.lock.Lock()
	defer c.lock.Unlock()

	c.RecordNamespaceEndpoints(eventType, endpoints)
	if err := c.Plugin.HandleEndpoints(eventType, endpoints); err != nil {
		utilruntime.HandleError(err)
	}
	c.Commit()
}

// HandleEndpointSlice handles a single EndpointSlice event and refreshes the router backend.
func (c *RouterController) HandleEndpointSlice(eventType watch.EventType, objMeta metav1.ObjectMeta, items []discoveryv1.EndpointSlice) {
	endpoints := &kapi.Endpoints{
		ObjectMeta: metav1.ObjectMeta{
			Name:            objMeta.Name,
			Namespace:       objMeta.Namespace,
			Labels:          objMeta.Labels,
			Annotations:     objMeta.Annotations,
			OwnerReferences: objMeta.OwnerReferences,
			ClusterName:     objMeta.ClusterName,
		},
		Subsets: endpointsubset.ConvertEndpointSlice(items, endpointsubset.DefaultEndpointAddressOrderByFuncs(), endpointsubset.DefaultEndpointPortOrderByFuncs()),
	}

	// RecordNamespaceEndpoints and all HandleEndpoints
	// implementations treat watch.Modified and watch.Added the
	// same, so we can conflate watch.Modified and watch.Added
	// here
	if len(items) == 0 {
		eventType = watch.Deleted
	} else {
		eventType = watch.Modified
	}

	c.HandleEndpoints(eventType, endpoints)
}

// Commit notifies the plugin that it is safe to commit state.
func (c *RouterController) Commit() {
	if c.firstSyncDone {
		if err := c.Plugin.Commit(); err != nil {
			utilruntime.HandleError(err)
		}
	}
}

// processRoute logs and propagates a route event to the plugin
func (c *RouterController) processRoute(eventType watch.EventType, route *routev1.Route) {
	log.V(4).Info("processing route", "event", eventType, "route", route)

	c.RecordNamespaceRoutes(eventType, route)
	if err := c.Plugin.HandleRoute(eventType, route); err != nil {
		utilruntime.HandleError(err)
	}
}

func (c *RouterController) handleFirstSync() {
	c.lock.Lock()
	defer c.lock.Unlock()

	c.firstSyncDone = true
	log.V(4).Info("router first sync complete")
	c.Commit()
}
