package controller

import (
	"context"
	"errors"
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
)

type RouteController struct {
	indexer  cache.Indexer
	queue    workqueue.RateLimitingInterface
	informer cache.SharedIndexInformer

	handleFunc func(watch.EventType, interface{})
}

type Key struct {
	event watch.EventType
	key   string
}

func (r *RouteController) Queue() workqueue.RateLimitingInterface {
	return r.queue
}

func NewRouteController(queue workqueue.RateLimitingInterface) *RouteController {
	return &RouteController{
		queue: queue,
	}
}

func (r *RouteController) WithSharedInformer(informer cache.SharedIndexInformer) *RouteController {
	r.informer = informer
	return r
}

func (r *RouteController) WithHandleFunc(handleFunc func(watch.EventType, interface{})) *RouteController {
	if handleFunc != nil {
		r.handleFunc = handleFunc
	}

	return r
}

func (r *RouteController) Run(ctx context.Context, stopCh <-chan struct{}) error {
	if r.informer == nil {
		return errors.New("missing informer")
	}

	r.indexer = r.informer.GetIndexer()
	_, err := r.informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			key, err := cache.MetaNamespaceKeyFunc(obj)
			if err == nil {
				klog.Info("new route controller: route added", key)
				r.queue.Add(Key{event: watch.Added, key: key})
			}
		},
		UpdateFunc: func(oldObj interface{}, newObj interface{}) {
			key, err := cache.MetaNamespaceKeyFunc(newObj)
			if err == nil {
				klog.Info("new route controller: route updated", key)
				r.queue.Add(Key{event: watch.Modified, key: key})
			}
		},
		DeleteFunc: func(obj interface{}) {
			key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
			if err == nil {
				klog.Info("new route controller: route deleted", key)
				r.queue.Add(Key{event: watch.Deleted, key: key})
			}
		},
	})
	if err != nil {
		return fmt.Errorf("failed to initialize informer event handlers: %w", err)
	}

	if !r.informer.IsStopped() {
		klog.Info("new route controller: restarting informers")
		r.informer.Run(stopCh)
	}

	if !cache.WaitForCacheSync(stopCh, r.informer.HasSynced) {
		return fmt.Errorf("timed out waiting for caches to sync")
	}

	go wait.Until(r.runWorker, time.Second, stopCh)

	<-ctx.Done()

	return ctx.Err()
}

func (r *RouteController) runWorker() {
	for r.processNextItem() {
	}
}

func (r *RouteController) processNextItem() bool {
	key, quit := r.queue.Get()
	if quit {
		return false
	}
	defer r.queue.Done(key)

	item, ok := key.(Key)
	if !ok {
		return true
	}

	klog.Infof("processing route %s", item.key)

	obj, exists, err := r.indexer.GetByKey(item.key)
	if err != nil {
		klog.Errorf("Fetching route object with key %s from store failed with %v", key, err)
		return false
	}
	if !exists {
		klog.Infof("route %s does not exist anymore", key)
		return true
	}
	r.handleFunc(item.event, obj)
	return true
}
