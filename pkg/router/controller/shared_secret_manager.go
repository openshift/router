package controller

import (
	"context"
	"fmt"
	"strings"
	"sync"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
)

// SharedSecretManager implements secretmanager.SecretManager using per-namespace SharedIndexInformers.
// This prevents creating a new API watch for every individual route/secret combination.
type SharedSecretManager struct {
	kubeClient kubernetes.Interface
	queue      workqueue.RateLimitingInterface

	lock      sync.RWMutex
	informers map[string]cache.SharedIndexInformer // namespace -> informer

	// registeredRoutes maps "namespace/routeName" -> referencedSecret
	registeredRoutes map[string]referencedSecret
}

type referencedSecret struct {
	secretName string
	handler    cache.ResourceEventHandlerFuncs
}

func NewSharedSecretManager(kubeClient kubernetes.Interface, queue workqueue.RateLimitingInterface) *SharedSecretManager {
	return &SharedSecretManager{
		kubeClient:       kubeClient,
		queue:            queue,
		informers:        make(map[string]cache.SharedIndexInformer),
		registeredRoutes: make(map[string]referencedSecret),
	}
}

func (m *SharedSecretManager) Queue() workqueue.RateLimitingInterface {
	return m.queue
}

func (m *SharedSecretManager) RegisterRoute(ctx context.Context, namespace string, routeName string, secretName string, handler cache.ResourceEventHandlerFuncs) error {
	m.lock.Lock()
	key := namespace + "/" + routeName
	if ref, exists := m.registeredRoutes[key]; exists {
		if ref.secretName == secretName {
			// Already registered for the same secret, just update the handler and return success.
			// This handles the race condition where ADDED and MODIFIED events fire concurrently.
			m.registeredRoutes[key] = referencedSecret{
				secretName: secretName,
				handler:    handler,
			}
			m.lock.Unlock()
			return nil
		}
		m.lock.Unlock()
		return fmt.Errorf("route already registered with key %s", key)
	}

	m.registeredRoutes[key] = referencedSecret{
		secretName: secretName,
		handler:    handler,
	}

	inf, exists := m.informers[namespace]
	if !exists {
		inf = cache.NewSharedIndexInformer(
			cache.NewListWatchFromClient(
				m.kubeClient.CoreV1().RESTClient(),
				"secrets",
				namespace,
				fields.Everything(), // Watch all secrets in the namespace
			),
			&corev1.Secret{},
			0,
			cache.Indexers{},
		)

		inf.AddEventHandler(cache.ResourceEventHandlerFuncs{
			AddFunc: func(obj interface{}) {
				m.notify(namespace, obj, "Add", nil)
			},
			UpdateFunc: func(oldObj, newObj interface{}) {
				m.notify(namespace, newObj, "Update", oldObj)
			},
			DeleteFunc: func(obj interface{}) {
				m.notify(namespace, obj, "Delete", nil)
			},
		})

		m.informers[namespace] = inf
		// Start the informer
		// We use context.Background() since this informer lives as long as the router,
		// or until we implement unregistering namespaces if they become empty (optimization).
		go inf.Run(context.Background().Done())
	}
	m.lock.Unlock()

	// Wait for cache sync (lock released to allow concurrent registrations)
	if !cache.WaitForCacheSync(ctx.Done(), inf.HasSynced) {
		// Rollback registration on failure
		m.lock.Lock()
		delete(m.registeredRoutes, key)
		m.lock.Unlock()
		return fmt.Errorf("failed waiting for cache sync for namespace %s", namespace)
	}

	klog.V(4).Infof("secret manager registered route for key %s with secret %s", key, secretName)
	return nil
}

func (m *SharedSecretManager) notify(namespace string, obj interface{}, eventType string, oldObj interface{}) {
	var secret *corev1.Secret
	switch t := obj.(type) {
	case *corev1.Secret:
		secret = t
	case cache.DeletedFinalStateUnknown:
		secret, _ = t.Obj.(*corev1.Secret)
	}

	if secret == nil {
		return
	}

	// Find all routes in this namespace that reference this secret
	var handlers []cache.ResourceEventHandlerFuncs
	prefix := namespace + "/"

	m.lock.RLock()
	for key, ref := range m.registeredRoutes {
		if ref.secretName == secret.Name && strings.HasPrefix(key, prefix) {
			handlers = append(handlers, ref.handler)
		}
	}
	m.lock.RUnlock()

	for _, h := range handlers {
		switch eventType {
		case "Add":
			if h.AddFunc != nil {
				h.AddFunc(obj)
			}
		case "Update":
			if h.UpdateFunc != nil {
				h.UpdateFunc(oldObj, obj)
			}
		case "Delete":
			if h.DeleteFunc != nil {
				h.DeleteFunc(obj)
			}
		}
	}
}

func (m *SharedSecretManager) UnregisterRoute(namespace string, routeName string) error {
	m.lock.Lock()
	defer m.lock.Unlock()

	key := namespace + "/" + routeName
	if _, exists := m.registeredRoutes[key]; !exists {
		return fmt.Errorf("no handler registered with key %s", key)
	}

	delete(m.registeredRoutes, key)
	klog.V(4).Infof("secret manager unregistered route for key %s", key)
	return nil
}

func (m *SharedSecretManager) GetSecret(ctx context.Context, namespace string, routeName string) (*corev1.Secret, error) {
	m.lock.RLock()
	key := namespace + "/" + routeName
	ref, exists := m.registeredRoutes[key]
	inf, infExists := m.informers[namespace]
	m.lock.RUnlock()

	if !exists {
		return nil, fmt.Errorf("no handler registered with key %s", key)
	}

	if !infExists {
		return nil, fmt.Errorf("no informer for namespace %s", namespace)
	}

	// Wait for cache sync
	if !cache.WaitForCacheSync(ctx.Done(), inf.HasSynced) {
		return nil, fmt.Errorf("failed waiting for cache sync")
	}

	obj, exists, err := inf.GetStore().GetByKey(namespace + "/" + ref.secretName)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, apierrors.NewNotFound(corev1.Resource("secrets"), ref.secretName)
	}

	return obj.(*corev1.Secret), nil
}

func (m *SharedSecretManager) LookupRouteSecret(namespace string, routeName string) (string, bool) {
	m.lock.RLock()
	defer m.lock.RUnlock()
	key := namespace + "/" + routeName
	ref, exists := m.registeredRoutes[key]
	if !exists {
		return "", false
	}
	return ref.secretName, true
}
