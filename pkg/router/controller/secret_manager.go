package controller

import (
	kapi "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/watch"

	routev1 "github.com/openshift/api/route/v1"
	"github.com/openshift/library-go/pkg/route/secret"
	"github.com/openshift/router/pkg/router"
)

type SecretManagerLoader struct {
	// plugin is the next plugin in the chain.
	plugin router.Plugin

	secretManager *secret.Manager

	// recorder is an interface for indicating route rejections.
	recorder RejectionRecorder
}

func NewSecretManagerLoader(plugin router.Plugin, secretmanager *secret.Manager, recorder RejectionRecorder) *SecretManagerLoader {
	return &SecretManagerLoader{
		plugin:        plugin,
		secretManager: secretmanager,
		recorder:      recorder,
	}
}

func (sm *SecretManagerLoader) HandleRoute(eventType watch.EventType, route *routev1.Route) error {

	dummyFunc := func(r *routev1.Route) sets.String { return sets.NewString() }
	switch eventType {
	case watch.Modified:
		if len(route.Spec.TLS.Certificate) > 0 && len(route.Spec.TLS.CertificateRef.Name) == 0 {
			sm.secretManager.UnregisterRoute(route, dummyFunc)
		} else if len(route.Spec.TLS.Certificate) == 0 && len(route.Spec.TLS.CertificateRef.Name) > 0 {
			sm.secretManager.RegisterRoute(route, dummyFunc)
		}
	case watch.Added:
		if len(route.Spec.TLS.Certificate) == 0 && len(route.Spec.TLS.CertificateRef.Name) > 0 {
			sm.secretManager.RegisterRoute(route, dummyFunc)
		}
	case watch.Deleted:
		if len(route.Spec.TLS.Certificate) == 0 && len(route.Spec.TLS.CertificateRef.Name) > 0 {
			sm.secretManager.UnregisterRoute(route, dummyFunc)
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
