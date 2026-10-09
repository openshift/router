package controller

import (
	"context"
	"fmt"
	"testing"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
	testing2 "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/cache"
)

func TestSharedSecretManagerHybrid(t *testing.T) {
	scenarios := []struct {
		name               string
		namespace          string
		allowList          bool
		expectedKey        types.NamespacedName
		expectedRestricted bool
	}{
		{
			name:               "unrestricted namespace uses namespace key",
			namespace:          "unrestricted",
			allowList:          true,
			expectedKey:        types.NamespacedName{Namespace: "unrestricted"},
			expectedRestricted: false,
		},
		{
			name:               "restricted namespace uses per-secret key",
			namespace:          "restricted",
			allowList:          false,
			expectedKey:        types.NamespacedName{Namespace: "restricted", Name: "my-secret"},
			expectedRestricted: true,
		},
	}

	for _, s := range scenarios {
		t.Run(s.name, func(t *testing.T) {
			client := fake.NewSimpleClientset()
			if !s.allowList {
				client.PrependReactor("list", "secrets", func(action testing2.Action) (handled bool, ret runtime.Object, err error) {
					return true, nil, apierrors.NewForbidden(schema.GroupResource{Resource: "secrets"}, "", fmt.Errorf("restricted"))
				})
			}

			mgr := NewSharedSecretManager(client, nil)
			ctx := context.Background()
			routeName := "my-route"
			secretName := "my-secret"
			handler := cache.ResourceEventHandlerFuncs{}

			err := mgr.RegisterRoute(ctx, s.namespace, routeName, secretName, handler)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			mgr.lock.RLock()
			infKey := mgr.getInformerKey(s.namespace, secretName, s.expectedRestricted)
			if infKey != s.expectedKey {
				t.Errorf("expected informer key %q, got %q", s.expectedKey, infKey)
			}

			if _, exists := mgr.informers[infKey]; !exists {
				t.Errorf("expected informer with key %q to exist", infKey)
			}

			ref, exists := mgr.registeredRoutes[s.namespace+"/"+routeName]
			if !exists {
				t.Fatalf("route not registered")
			}
			if ref.restricted != s.expectedRestricted {
				t.Errorf("expected restricted=%v, got %v", s.expectedRestricted, ref.restricted)
			}
			mgr.lock.RUnlock()

			// Test Unregister
			err = mgr.UnregisterRoute(s.namespace, routeName)
			if err != nil {
				t.Fatalf("unexpected error during unregister: %v", err)
			}

			mgr.lock.RLock()
			if _, exists := mgr.informers[infKey]; exists {
				t.Errorf("expected informer with key %q to be removed", infKey)
			}
			mgr.lock.RUnlock()
		})
	}
}

func TestSharedSecretManagerMultiRoute(t *testing.T) {
	client := fake.NewSimpleClientset()
	mgr := NewSharedSecretManager(client, nil)
	ctx := context.Background()
	ns := "test-ns"
	secretName := "shared-secret"

	// Register first route
	err := mgr.RegisterRoute(ctx, ns, "route1", secretName, cache.ResourceEventHandlerFuncs{})
	if err != nil {
		t.Fatal(err)
	}

	// Register second route
	err = mgr.RegisterRoute(ctx, ns, "route2", secretName, cache.ResourceEventHandlerFuncs{})
	if err != nil {
		t.Fatal(err)
	}

	mgr.lock.RLock()
	if len(mgr.informers) != 1 {
		t.Errorf("expected 1 informer, got %d", len(mgr.informers))
	}
	mgr.lock.RUnlock()

	// Unregister first route
	err = mgr.UnregisterRoute(ns, "route1")
	if err != nil {
		t.Fatal(err)
	}

	mgr.lock.RLock()
	if len(mgr.informers) != 1 {
		t.Errorf("expected informer to persist because route2 still exists")
	}
	mgr.lock.RUnlock()

	// Unregister second route
	err = mgr.UnregisterRoute(ns, "route2")
	if err != nil {
		t.Fatal(err)
	}

	mgr.lock.RLock()
	if len(mgr.informers) != 0 {
		t.Errorf("expected informer to be removed")
	}
	mgr.lock.RUnlock()
}

func TestSharedSecretManagerIgnoresUnchangedSecret(t *testing.T) {
	manager := NewSharedSecretManager(fake.NewSimpleClientset(), nil)
	updates := 0
	manager.registeredRoutes["sandbox/route"] = referencedSecret{
		secretName: "tls-secret",
		handler: cache.ResourceEventHandlerFuncs{
			UpdateFunc: func(_, _ interface{}) { updates++ },
		},
	}

	oldSecret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{
		Namespace: "sandbox", Name: "tls-secret", UID: "uid-1", ResourceVersion: "10",
	}}
	manager.notify("sandbox", oldSecret.DeepCopy(), "Update", oldSecret)
	if updates != 0 {
		t.Fatalf("unchanged secret produced %d update callbacks", updates)
	}

	newSecret := oldSecret.DeepCopy()
	newSecret.ResourceVersion = "11"
	manager.notify("sandbox", newSecret, "Update", oldSecret)
	if updates != 1 {
		t.Fatalf("new secret version produced %d update callbacks, want 1", updates)
	}

	recreatedSecret := oldSecret.DeepCopy()
	recreatedSecret.UID = "uid-2"
	manager.notify("sandbox", recreatedSecret, "Update", oldSecret)
	if updates != 2 {
		t.Fatalf("new secret UID produced %d update callbacks, want 2", updates)
	}
}

func TestSharedSecretManagerDispatchesChangedSecretToSixRoutes(t *testing.T) {
	manager := NewSharedSecretManager(fake.NewSimpleClientset(), nil)
	updates := make(map[string]int)
	for i := 0; i < 6; i++ {
		routeName := fmt.Sprintf("route-%d", i)
		manager.registeredRoutes["sandbox/"+routeName] = referencedSecret{
			secretName: "shared-secret",
			handler: cache.ResourceEventHandlerFuncs{UpdateFunc: func(_, _ interface{}) {
				updates[routeName]++
			}},
		}
	}
	oldSecret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{
		Namespace: "sandbox", Name: "shared-secret", UID: "uid-1", ResourceVersion: "10",
	}}
	manager.notify("sandbox", oldSecret.DeepCopy(), "Update", oldSecret)
	if len(updates) != 0 {
		t.Fatalf("unchanged Secret dispatched to routes: %v", updates)
	}
	newSecret := oldSecret.DeepCopy()
	newSecret.ResourceVersion = "11"
	manager.notify("sandbox", newSecret, "Update", oldSecret)
	for i := 0; i < 6; i++ {
		routeName := fmt.Sprintf("route-%d", i)
		if updates[routeName] != 1 {
			t.Fatalf("route %s received %d changed-Secret callbacks, want 1", routeName, updates[routeName])
		}
	}
}
