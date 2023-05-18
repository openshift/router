package controller

import (
	"fmt"
	"sync"

	kapi "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/watch"

	routev1 "github.com/openshift/api/route/v1"
	"github.com/openshift/router/pkg/router"
	"github.com/openshift/router/pkg/router/monitor"
)

type SecretManagerLoader struct {
	// plugin is the next plugin in the chain.
	plugin router.Plugin

	secretManager monitor.Manager

	state map[string]*routev1.Route

	// lock is a mutex used to prevent concurrent router reloads.
	lock sync.Mutex

	// recorder is an interface for indicating route rejections.
	recorder RejectionRecorder
}

func NewSecretManagerLoader(plugin router.Plugin, secretmanager monitor.Manager, recorder RejectionRecorder) *SecretManagerLoader {
	return &SecretManagerLoader{
		plugin:        plugin,
		secretManager: secretmanager,
		recorder:      recorder,
	}
}

func (sm *SecretManagerLoader) HandleRoute(eventType watch.EventType, route *routev1.Route) error {

	// We have to call the internal form of functions after this
	// because we are holding the state lock.
	sm.lock.Lock()
	defer sm.lock.Unlock()

	key := fmt.Sprintf("%s/%s", route.Namespace, route.Name)
	switch eventType {
	case watch.Modified:
		if len(route.Spec.TLS.Certificate) > 0 && len(route.Spec.TLS.CertificateRef.Name) == 0 {
			if _, exists := sm.state[key]; exists {
				sm.secretManager.Unregister(route, monitor.GetSecretsReferenced)
				delete(sm.state, key)
			}
		} else if len(route.Spec.TLS.Certificate) == 0 && len(route.Spec.TLS.CertificateRef.Name) > 0 {
			if old, exists := sm.state[key]; exists {
				if old.Spec.TLS.CertificateRef.Name != route.Spec.TLS.CertificateRef.Name {
					sm.secretManager.Unregister(old, monitor.GetSecretsReferenced)
					sm.secretManager.Register(route, monitor.GetSecretsReferenced)
				}
			} else {
				sm.secretManager.Register(route, monitor.GetSecretsReferenced)
			}
			sm.state[key] = route
		}
	case watch.Added:
		if len(route.Spec.TLS.Certificate) == 0 && len(route.Spec.TLS.CertificateRef.Name) > 0 {
			sm.secretManager.Register(route, monitor.GetSecretsReferenced)
			sm.state[key] = route
		}
	case watch.Deleted:
		if len(route.Spec.TLS.Certificate) == 0 && len(route.Spec.TLS.CertificateRef.Name) > 0 {
			sm.secretManager.Unregister(route, monitor.GetSecretsReferenced)
			delete(sm.state, key)
		}
	}

	return sm.plugin.HandleRoute(eventType, route)
}

func (sm *SecretManagerLoader) HandleNode(eventType watch.EventType, node *kapi.Node) error {
	return sm.plugin.HandleNode(eventType, node)
}

func (sm *SecretManagerLoader) HandleEndpoints(eventType watch.EventType, endpoints *kapi.Endpoints) error {
	return sm.plugin.HandleEndpoints(eventType, endpoints)
}

func (sm *SecretManagerLoader) HandleNamespaces(namespaces sets.String) error {
	return sm.plugin.HandleNamespaces(namespaces)
}

func (sm *SecretManagerLoader) Commit() error {
	return sm.plugin.Commit()
}
