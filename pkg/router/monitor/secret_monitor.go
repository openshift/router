package monitor

import (
	"context"
	"fmt"
	"sync"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"

	routev1 "github.com/openshift/api/route/v1"
)

type listObjectFunc func(string, metav1.ListOptions) (runtime.Object, error)
type watchObjectFunc func(string, metav1.ListOptions) (watch.Interface, error)

type SecretMonitor struct {
	registeredPods map[objectKey]*routev1.Route
	monitors       map[objectKey]*ObjectMonitor

	lock sync.RWMutex

	stopCh <-chan struct{}

	listObject  listObjectFunc
	watchObject watchObjectFunc

	// monitors are the producer of the resourceChanges queue
	resourceChanges workqueue.RateLimitingInterface
}

// dummy get secrets referenced method
var GetSecretsReferenced = func(route *routev1.Route) sets.String {
	result := sets.NewString()

	if len(route.Spec.TLS.Certificate) == 0 && len(route.Spec.TLS.CertificateRef.Name) > 0 {
		result.Insert(route.Spec.TLS.CertificateRef.Name)
	}

	return result
}

func NewSecretMonitor(clientset kubernetes.Interface, queue workqueue.RateLimitingInterface) *SecretMonitor {
	return &SecretMonitor{
		registeredPods: make(map[objectKey]*routev1.Route),
		monitors:       make(map[objectKey]*ObjectMonitor),
		stopCh:         make(<-chan struct{}),
		listObject: func(namespace string, opts metav1.ListOptions) (runtime.Object, error) {
			return clientset.CoreV1().Secrets(namespace).List(context.TODO(), opts)
		},
		watchObject: func(namespace string, opts metav1.ListOptions) (watch.Interface, error) {
			return clientset.CoreV1().Secrets(namespace).Watch(context.TODO(), opts)
		},
		resourceChanges: queue,
	}
}

var _ Manager = (*SecretMonitor)(nil)

func (sm *SecretMonitor) Get(namespace, name string) (runtime.Object, error) {
	key := objectKey{namespace: namespace, name: name}
	gr := appsv1.Resource("secret")

	sm.lock.RLock()
	item, exists := sm.monitors[key]
	sm.lock.RUnlock()

	if !exists {
		return nil, fmt.Errorf("object %q/%q not registered", namespace, name)
	}

	if err := wait.PollImmediate(10*time.Millisecond, time.Second, item.hasSynced); err != nil {
		return nil, fmt.Errorf("failed to sync %s cache: %v", gr.String(), err)
	}

	obj, exists, err := item.store.GetByKey(item.key(namespace, name))
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, apierrors.NewNotFound(gr, name)
	}

	if object, ok := obj.(runtime.Object); ok {
		return object, nil
	}
	return nil, fmt.Errorf("unexpected object type: %v", obj)
}

func (sm *SecretMonitor) Register(parent *routev1.Route, getReferencedObjects func(*routev1.Route) sets.String) {

	names := getReferencedObjects(parent)

	sm.lock.Lock()
	defer sm.lock.Unlock()

	for name := range names {
		key := objectKey{namespace: parent.Namespace, name: name}
		monitor, exists := sm.monitors[key]

		if !exists {
			fieldSelector := fields.Set{"metadata.name": name}.AsSelector().String()
			listFunc := func(options metav1.ListOptions) (runtime.Object, error) {
				options.FieldSelector = fieldSelector
				return sm.listObject(parent.Namespace, options)
			}
			watchFunc := func(options metav1.ListOptions) (watch.Interface, error) {
				options.FieldSelector = fieldSelector
				return sm.watchObject(parent.Namespace, options)
			}

			store, informer := cache.NewInformer(
				&cache.ListWatch{ListFunc: listFunc, WatchFunc: watchFunc},
				&v1.Secret{},
				0, cache.ResourceEventHandlerFuncs{
					AddFunc: func(obj interface{}) {
						key, _ := cache.MetaNamespaceKeyFunc(obj)

						secret := obj.(*v1.Secret)
						klog.Info("Secret added ", "obj ", secret.ResourceVersion, " key ", key)

						sm.resourceChanges.Add(fmt.Sprintf("%s/%s", parent.Namespace, parent.Name))

					},
					UpdateFunc: func(old interface{}, new interface{}) {
						key, _ := cache.MetaNamespaceKeyFunc(new)

						secretOld := old.(*v1.Secret)
						secretNew := new.(*v1.Secret)
						klog.Info("Secret updated ", "old ", secretOld.ResourceVersion, " new ", secretNew.ResourceVersion, " key ", key)

						sm.resourceChanges.Add(fmt.Sprintf("%s/%s", parent.Namespace, parent.Name))
					},
					DeleteFunc: func(obj interface{}) {
						if deletedFinalStateUnknown, ok := obj.(cache.DeletedFinalStateUnknown); ok {
							obj = deletedFinalStateUnknown.Obj
						}

						secret := obj.(*v1.Secret)
						// IndexerInformer uses a delta queue, therefore for deletes we have to use this
						// key function.
						key, _ := cache.DeletionHandlingMetaNamespaceKeyFunc(obj)

						klog.Info("Secret deleted ", " obj ", secret.ResourceVersion, " key ", key)
						sm.resourceChanges.Add(fmt.Sprintf("%s/%s", parent.Namespace, parent.Name))
					},
				})

			monitor = &ObjectMonitor{
				refCount:  0,
				store:     store,
				informer:  informer,
				hasSynced: func() (bool, error) { return informer.HasSynced(), nil },
				stopCh:    make(chan struct{}),
			}

			go monitor.startInformer()

			sm.monitors[key] = monitor
		}
		monitor.refCount++
		klog.Info("watch based manager add", " ref count ", monitor.refCount, " item key ", key)
	}

	var prev *routev1.Route
	key := objectKey{namespace: parent.Namespace, name: parent.Name, uid: parent.UID}
	prev = sm.registeredPods[key]
	sm.registeredPods[key] = parent

	if prev != nil {
		for name := range getReferencedObjects(prev) {
			key := objectKey{namespace: prev.Namespace, name: name}

			if item, ok := sm.monitors[key]; ok {
				item.refCount--
				klog.Info("watch based manager delete", " ref count ", item.refCount, " item key ", key)
				if item.refCount == 0 {
					// Stop the underlying reflector.
					if item.stop() {
						klog.Info("watch based manager delete informer stopped ", " ref count ", item.refCount, " item key ", key)
					}
					delete(sm.monitors, key)
				}
			}

		}
	}
}

func (sm *SecretMonitor) Unregister(parent *routev1.Route, getReferencedObjects func(*routev1.Route) sets.String) {
	var prev *routev1.Route
	key := objectKey{namespace: parent.Namespace, name: parent.Name, uid: parent.UID}

	sm.lock.Lock()
	defer sm.lock.Unlock()

	prev = sm.registeredPods[key]
	delete(sm.registeredPods, key)

	if prev != nil {
		for name := range getReferencedObjects(prev) {
			key := objectKey{namespace: prev.Namespace, name: name}

			if item, ok := sm.monitors[key]; ok {
				item.refCount--
				klog.Info("watch based manager delete", " ref count ", item.refCount, " item key ", key)
				if item.refCount == 0 {
					// Stop the underlying reflector.
					if item.stop() {
						klog.Info("watch based manager delete informer stopped ", " ref count ", item.refCount, " item key ", key)
					}
					delete(sm.monitors, key)
				}
			}

		}
	}
}
