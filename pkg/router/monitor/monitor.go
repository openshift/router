package monitor

import (
	"sync"

	routev1 "github.com/openshift/api/route/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/tools/cache"
)

type objectKey struct {
	namespace string
	name      string
	uid       types.UID
}

type ObjectMonitor struct {
	refCount int
	store    cache.Store
	informer cache.Controller

	hasSynced func() (bool, error)

	// waitGroup is used to ensure that there won't be two concurrent calls to reflector.Run
	waitGroup sync.WaitGroup

	// lock is to ensure the access and modify of lastAccessTime, stopped, and immutable are thread safety,
	// and protecting from closing stopCh multiple times.
	lock    sync.Mutex
	stopped bool
	stopCh  chan struct{}
}

func (i *ObjectMonitor) stop() bool {
	i.lock.Lock()
	defer i.lock.Unlock()
	return i.stopThreadUnsafe()
}

func (i *ObjectMonitor) stopThreadUnsafe() bool {
	if i.stopped {
		return false
	}
	i.stopped = true
	close(i.stopCh)
	return true
}

// key returns key of an object with a given name and namespace.
// This has to be in-sync with cache.MetaNamespaceKeyFunc.
func (c *ObjectMonitor) key(namespace, name string) string {
	if len(namespace) > 0 {
		return namespace + "/" + name
	}
	return name
}

func (i *ObjectMonitor) restartReflectorIfNeeded() {
	i.lock.Lock()
	defer i.lock.Unlock()
	if !i.stopped {
		return
	}
	i.stopCh = make(chan struct{})
	i.stopped = false
	go i.startInformer()
}

func (i *ObjectMonitor) startInformer() {
	i.waitGroup.Wait()
	i.waitGroup.Add(1)
	defer i.waitGroup.Done()
	i.informer.Run(i.stopCh)
}

type Manager interface {
	Get(namespace, name string) (runtime.Object, error)

	Register(parent *routev1.Route, getReferencedObjects func(*routev1.Route) sets.String)

	Unregister(parent *routev1.Route, getReferencedObjects func(*routev1.Route) sets.String)
}
