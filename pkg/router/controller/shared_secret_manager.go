package controller

import (
	"context"
	"fmt"
	"strings"
	"sync"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
)

type informerState struct {
	informer cache.SharedIndexInformer
	cancel   context.CancelFunc
}

// SharedSecretManager implements secretmanager.SecretManager using per-namespace SharedIndexInformers.
// This prevents creating a new API watch for every individual route/secret combination.
// It uses a hybrid strategy: it attempts to watch all secrets in a namespace (Fast Path),
// but falls back to watching specific secrets by name if RBAC is restricted (Safe Path).
type SharedSecretManager struct {
	kubeClient kubernetes.Interface
	queue      workqueue.RateLimitingInterface

	lock      sync.RWMutex
	informers map[types.NamespacedName]*informerState // Namespace is always set, Name is empty for Fast Path, or secretName for Safe Path

	// restrictedNamespaces tracks namespaces where we don't have permission to list all secrets.
	// value is true if restricted, false if not restricted.
	restrictedNamespaces map[string]bool

	// registeredRoutes maps "namespace/routeName" -> referencedSecret
	registeredRoutes map[string]referencedSecret
}

type referencedSecret struct {
	secretName string
	handler    cache.ResourceEventHandlerFuncs
	restricted bool // indicates if this route is using a restricted (per-secret) informer
}

func NewSharedSecretManager(kubeClient kubernetes.Interface, queue workqueue.RateLimitingInterface) *SharedSecretManager {
	return &SharedSecretManager{
		kubeClient:           kubeClient,
		queue:                queue,
		informers:            make(map[types.NamespacedName]*informerState),
		restrictedNamespaces: make(map[string]bool),
		registeredRoutes:     make(map[string]referencedSecret),
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
			m.registeredRoutes[key] = referencedSecret{
				secretName: secretName,
				handler:    handler,
				restricted: ref.restricted,
			}
			m.lock.Unlock()
			return nil
		}
		m.lock.Unlock()
		return fmt.Errorf("route already registered with key %s", key)
	}

	// Determine if this namespace is restricted (e.g. resourceNames used in RBAC).
	// Restricted namespaces require per-secret informers using FieldSelectors.
	restricted, known := m.restrictedNamespaces[namespace]
	if !known {
		// Release lock during API probe to avoid blocking other registrations.
		m.lock.Unlock()
		// We use a metadata-only List check to see if we have namespace-wide permissions.
		_, err := m.kubeClient.CoreV1().Secrets(namespace).List(ctx, metav1.ListOptions{Limit: 1})
		m.lock.Lock()

		// Re-check if someone else probed while we were unlocked.
		if restricted, known = m.restrictedNamespaces[namespace]; !known {
			restricted = (err != nil && apierrors.IsForbidden(err))
			m.restrictedNamespaces[namespace] = restricted
			if restricted {
				klog.V(2).Infof("namespace %s is restricted (RBAC), falling back to per-secret informers for routes", namespace)
			}
		}
	}

	m.registeredRoutes[key] = referencedSecret{
		secretName: secretName,
		handler:    handler,
		restricted: restricted,
	}

	infKey := m.getInformerKey(namespace, secretName, restricted)
	infState, exists := m.informers[infKey]
	if !exists {
		var selector fields.Selector
		if restricted {
			// Safe Path: Watch only this specific secret by name.
			selector = fields.OneTermEqualSelector("metadata.name", secretName)
		} else {
			// Fast Path: Watch all secrets in the namespace.
			selector = fields.Everything()
		}

		inf := cache.NewSharedIndexInformer(
			cache.NewListWatchFromClient(
				m.kubeClient.CoreV1().RESTClient(),
				"secrets",
				namespace,
				selector,
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

		ctx, cancel := context.WithCancel(context.Background())
		infState = &informerState{
			informer: inf,
			cancel:   cancel,
		}
		m.informers[infKey] = infState

		// Start the informer
		go inf.Run(ctx.Done())
	}
	m.lock.Unlock()

	klog.V(4).Infof("secret manager registered route for key %s with secret %s (restricted=%v)", key, secretName, restricted)
	return nil
}

func (m *SharedSecretManager) getInformerKey(namespace, secretName string, restricted bool) types.NamespacedName {
	if restricted {
		return types.NamespacedName{Namespace: namespace, Name: secretName}
	}
	return types.NamespacedName{Namespace: namespace}
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
	ref, exists := m.registeredRoutes[key]
	if !exists {
		return fmt.Errorf("no handler registered with key %s", key)
	}

	delete(m.registeredRoutes, key)

	// Check if there are any remaining routes using the same informer (same namespace and same secret if restricted).
	infKey := m.getInformerKey(namespace, ref.secretName, ref.restricted)
	hasRoutesForInformer := false
	for k, r := range m.registeredRoutes {
		// Only check routes in the same namespace
		if !strings.HasPrefix(k, namespace+"/") {
			continue
		}
		if m.getInformerKey(namespace, r.secretName, r.restricted) == infKey {
			hasRoutesForInformer = true
			break
		}
	}

	// If no routes remain for this informer, stop it and clean up.
	if !hasRoutesForInformer {
		if infState, exists := m.informers[infKey]; exists {
			infState.cancel()
			delete(m.informers, infKey)
			klog.V(4).Infof("secret manager shut down informer for key %s", infKey)
		}
	}

	klog.V(4).Infof("secret manager unregistered route for key %s", key)
	return nil
}

func (m *SharedSecretManager) GetSecret(ctx context.Context, namespace string, routeName string) (*corev1.Secret, error) {
	m.lock.RLock()
	key := namespace + "/" + routeName
	ref, exists := m.registeredRoutes[key]
	if !exists {
		m.lock.RUnlock()
		return nil, fmt.Errorf("no handler registered with key %s", key)
	}

	infKey := m.getInformerKey(namespace, ref.secretName, ref.restricted)
	inf, infExists := m.informers[infKey]
	m.lock.RUnlock()

	if !infExists {
		return nil, fmt.Errorf("no informer for key %s", infKey)
	}

	// Try to get from cache first
	if inf.informer.HasSynced() {
		obj, exists, err := inf.informer.GetStore().GetByKey(namespace + "/" + ref.secretName)
		if err == nil && exists {
			return obj.(*corev1.Secret), nil
		}
	}

	// Fallback to direct API call if cache is not synced or object is missing
	secret, err := m.kubeClient.CoreV1().Secrets(namespace).Get(ctx, ref.secretName, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	return secret, nil
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
