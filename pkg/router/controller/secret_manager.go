package controller

import (
	"fmt"
	"sync"
	"time"

	kapi "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"

	routev1 "github.com/openshift/api/route/v1"
	"github.com/openshift/library-go/pkg/route/secret"
	"github.com/openshift/router/pkg/router"
)

type SecretManagerLoader struct {
	// plugin is the next plugin in the chain.
	plugin router.Plugin

	secretManager Manager
	// recorder is an interface for indicating route rejections.
	recorder RejectionRecorder
}

func NewSecretManagerLoader(plugin router.Plugin, manager Manager, recorder RejectionRecorder) *SecretManagerLoader {
	return &SecretManagerLoader{
		plugin:        plugin,
		secretManager: manager,
		recorder:      recorder,
	}
}

func (sm *SecretManagerLoader) HandleRoute(eventType watch.EventType, route *routev1.Route) error {

	tls := route.Spec.TLS.DeepCopy()

	switch eventType {
	case watch.Modified:
		if len(tls.Certificate) > 0 && tls.ExternalCertificate != nil && len(tls.ExternalCertificate.Name) == 0 {
			sm.secretManager.UnregisterRoute(route)
		} else if len(route.Spec.TLS.Certificate) == 0 && tls.ExternalCertificate != nil && len(tls.ExternalCertificate.Name) > 0 {
			sm.secretManager.RegisterRoute(route)
		}
	case watch.Added:
		if len(tls.Certificate) == 0 && tls.ExternalCertificate != nil && len(tls.ExternalCertificate.Name) > 0 {
			sm.secretManager.RegisterRoute(route)
		}
	case watch.Deleted:
		if len(tls.Certificate) == 0 && tls.ExternalCertificate != nil && len(tls.ExternalCertificate.Name) > 0 {
			sm.secretManager.UnregisterRoute(route)
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

type Manager interface {
	GetSecret(parent *routev1.Route, namespace, name string) (*v1.Secret, error)
	RegisterRoute(parent *routev1.Route) error
	UnregisterRoute(parent *routev1.Route) error
}

type secretManager struct {
	monitor            secret.SecretMonitor
	registeredHandlers map[string]secret.SecretEventHandlerRegistration

	lock sync.RWMutex

	// monitors are the producer of the resourceChanges queue
	resourceChanges workqueue.RateLimitingInterface
}

func NewSecretManager(kubeClient *kubernetes.Clientset, queue workqueue.RateLimitingInterface) *secretManager {
	return &secretManager{
		monitor:            secret.NewSecretMonitor(kubeClient),
		lock:               sync.RWMutex{},
		resourceChanges:    queue,
		registeredHandlers: make(map[string]secret.SecretEventHandlerRegistration),
	}
}

func (m *secretManager) GetSecret(parent *routev1.Route, namespace, name string) (*v1.Secret, error) {
	m.lock.Lock()
	defer m.lock.Unlock()

	key := fmt.Sprintf("%s/%s", parent.Namespace, parent.Name)
	handle, exists := m.registeredHandlers[key]

	if !exists {
		return nil, apierrors.NewNotFound(schema.GroupResource{Resource: "routes"}, key)
	}

	if err := wait.PollImmediate(10*time.Millisecond, time.Second, func() (done bool, err error) { return handle.HasSynced(), nil }); err != nil {
		return nil, apierrors.NewInternalError(err)
	}

	obj, err := m.monitor.GetSecret(handle)
	if err != nil {
		return nil, err
	}

	return obj, nil
}

func (m *secretManager) RegisterRoute(parent *routev1.Route) error {
	m.lock.Lock()
	defer m.lock.Unlock()

	handle, err := m.monitor.AddEventHandler(parent.Namespace, fmt.Sprintf("%s_%s", parent.Name, parent.Spec.TLS.ExternalCertificate.Name), cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			// TODO need to update object transform to Key{event: watch.Modified, key: key}
			m.resourceChanges.Add(parent)
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			m.resourceChanges.Add(parent)
		},
		DeleteFunc: func(obj interface{}) {
			m.resourceChanges.Add(parent)
		},
	})
	if err != nil {
		return apierrors.NewInternalError(err)
	}

	key := fmt.Sprintf("%s/%s", parent.Namespace, parent.Name)
	m.registeredHandlers[key] = handle

	klog.Info("secret manager registered route", " route", key)

	return nil

}

func (m *secretManager) UnregisterRoute(parent *routev1.Route) error {
	m.lock.Lock()
	defer m.lock.Unlock()

	key := fmt.Sprintf("%s/%s", parent.Namespace, parent.Name)
	handle, ok := m.registeredHandlers[key]
	if !ok {
		return apierrors.NewNotFound(schema.GroupResource{Resource: "routes"}, key)
	}

	err := m.monitor.RemoveEventHandler(handle)
	if err != nil {
		return apierrors.NewNotFound(schema.GroupResource{Resource: "routes"}, key)
	}

	delete(m.registeredHandlers, key)

	klog.Info("secret manager unregistered route", " route", key)

	return nil
}
