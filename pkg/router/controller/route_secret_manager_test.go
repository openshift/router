package controller

import (
	"context"
	"fmt"
	"reflect"
	"testing"

	"github.com/openshift/library-go/pkg/route/secretmanager/fake"
	"github.com/openshift/router/pkg/router"

	routev1 "github.com/openshift/api/route/v1"
	authorizationv1 "k8s.io/api/authorization/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/watch"
	testclient "k8s.io/client-go/kubernetes/fake"
	corev1client "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/cache"
)

const testRouterName = "test-router"

type testSARCreator struct {
	allow bool
	err   error
	sar   *authorizationv1.SubjectAccessReview
}

func (t *testSARCreator) Create(_ context.Context, subjectAccessReview *authorizationv1.SubjectAccessReview, _ metav1.CreateOptions) (*authorizationv1.SubjectAccessReview, error) {
	t.sar = subjectAccessReview
	return &authorizationv1.SubjectAccessReview{
		Status: authorizationv1.SubjectAccessReviewStatus{
			Allowed: t.allow,
		},
	}, t.err
}

type testSecretGetter struct {
	namespace string
	secret    *corev1.Secret
}

func (t *testSecretGetter) Secrets(_ string) corev1client.SecretInterface {
	return testclient.NewSimpleClientset(t.secret).CoreV1().Secrets(t.namespace)
}

// fakeSecretInformer will list/watch only one secret inside a namespace
func fakeSecretInformer(fakeKubeClient *testclient.Clientset, namespace, name string) cache.SharedInformer {
	fieldSelector := fields.OneTermEqualSelector("metadata.name", name).String()
	return cache.NewSharedInformer(
		&cache.ListWatch{
			ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
				options.FieldSelector = fieldSelector
				return fakeKubeClient.CoreV1().Secrets(namespace).List(context.TODO(), options)
			},
			WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
				options.FieldSelector = fieldSelector
				return fakeKubeClient.CoreV1().Secrets(namespace).Watch(context.TODO(), options)
			},
		},
		&corev1.Secret{},
		0,
	)
}

func fakeSecret(namespace, name string, secretType corev1.SecretType, data map[string][]byte) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Data: data,
		Type: secretType,
	}
}

type fakePluginDone struct {
	eventType watch.EventType
	route     *routev1.Route
	err       error
	doneCh    chan struct{}
}

func (p *fakePluginDone) HandleRoute(eventType watch.EventType, route *routev1.Route) error {
	defer close(p.doneCh)
	p.eventType, p.route = eventType, route
	return p.err
}
func (p *fakePluginDone) HandleNode(t watch.EventType, node *corev1.Node) error {
	return fmt.Errorf("not expected")
}
func (p *fakePluginDone) HandleEndpoints(watch.EventType, *corev1.Endpoints) error {
	return fmt.Errorf("not expected")
}
func (p *fakePluginDone) HandleNamespaces(namespaces sets.String) error {
	return fmt.Errorf("not expected")
}
func (p *fakePluginDone) Commit() error {
	return p.err
}

var _ router.Plugin = &fakePluginDone{}

type statusRecorder struct {
	rejections                 []string
	updates                    []string
	unservableInFutureVersions map[string]string
	doneCh                     chan struct{}
}

func (r *statusRecorder) routeKey(route *routev1.Route) string {
	return route.Namespace + "-" + route.Name
}
func (r *statusRecorder) RecordRouteRejection(route *routev1.Route, reason, message string) {
	defer close(r.doneCh)
	r.rejections = append(r.rejections, fmt.Sprintf("%s:%s", r.routeKey(route), reason))
}

func (r *statusRecorder) RecordRouteUpdate(route *routev1.Route, reason, message string) {
	defer close(r.doneCh)
	r.updates = append(r.updates, fmt.Sprintf("%s:%s", r.routeKey(route), reason))
}

func (r *statusRecorder) RecordRouteUnservableInFutureVersionsClear(route *routev1.Route) {
	delete(r.unservableInFutureVersions, r.routeKey(route))
}
func (r *statusRecorder) RecordRouteUnservableInFutureVersions(route *routev1.Route, reason, message string) {
	r.unservableInFutureVersions[r.routeKey(route)] = reason
}

var _ RouteStatusRecorder = &statusRecorder{}

func TestRouteSecretManager(t *testing.T) {

	scenarios := []struct {
		name               string
		route              *routev1.Route
		secretManager      fake.SecretManager
		eventType          watch.EventType
		allow              bool
		expectedRoute      *routev1.Route
		expectedEventType  watch.EventType
		expectedRejections []string
		expectedError      bool
	}{
		// scenarios when route is added
		{
			name: "route added with externalCertificate denied",
			route: &routev1.Route{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "route-test",
					Namespace: "sandbox",
				},
				Spec: routev1.RouteSpec{
					TLS: &routev1.TLSConfig{
						ExternalCertificate: &routev1.LocalObjectReference{
							Name: "tls-secret",
						},
					},
				},
			},
			secretManager: fake.SecretManager{
				Secret: fakeSecret("sandbox", "tls-secret", corev1.SecretTypeTLS, map[string][]byte{}),
			},
			eventType: watch.Added,
			allow:     false,
			expectedRoute: &routev1.Route{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "route-test",
					Namespace: "sandbox",
				},
				Spec: routev1.RouteSpec{
					TLS: &routev1.TLSConfig{
						ExternalCertificate: &routev1.LocalObjectReference{
							Name: "tls-secret",
						},
					},
				},
			},
			expectedEventType: watch.Deleted,
			expectedRejections: []string{
				"sandbox-route-test:ExternalCertificateValidationFailed",
			},
			expectedError: true,
		},
		{
			name: "route added with externalCertificate allowed but secret not found",
			route: &routev1.Route{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "route-test",
					Namespace: "sandbox",
				},
				Spec: routev1.RouteSpec{
					TLS: &routev1.TLSConfig{
						ExternalCertificate: &routev1.LocalObjectReference{
							Name: "tls-secret",
						},
					},
				},
			},
			secretManager: fake.SecretManager{
				Secret: fakeSecret("other-sandbox", "tls-secret", corev1.SecretTypeTLS, map[string][]byte{}),
			},
			eventType: watch.Added,
			allow:     true,
			expectedRoute: &routev1.Route{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "route-test",
					Namespace: "sandbox",
				},
				Spec: routev1.RouteSpec{
					TLS: &routev1.TLSConfig{
						ExternalCertificate: &routev1.LocalObjectReference{
							Name: "tls-secret",
						},
					},
				},
			},
			expectedEventType: watch.Deleted,
			expectedRejections: []string{
				"sandbox-route-test:ExternalCertificateValidationFailed",
			},
			expectedError: true,
		},
		{
			name: "route added with externalCertificate allowed but secret of incorrect type",
			route: &routev1.Route{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "route-test",
					Namespace: "sandbox",
				},
				Spec: routev1.RouteSpec{
					TLS: &routev1.TLSConfig{
						ExternalCertificate: &routev1.LocalObjectReference{
							Name: "tls-secret",
						},
					},
				},
			},
			secretManager: fake.SecretManager{
				Secret: fakeSecret("sandbox", "tls-secret", corev1.SecretTypeBasicAuth, map[string][]byte{}),
			},
			eventType: watch.Added,
			allow:     true,
			expectedRoute: &routev1.Route{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "route-test",
					Namespace: "sandbox",
				},
				Spec: routev1.RouteSpec{
					TLS: &routev1.TLSConfig{
						ExternalCertificate: &routev1.LocalObjectReference{
							Name: "tls-secret",
						},
					},
				},
			},
			expectedEventType: watch.Deleted,
			expectedRejections: []string{
				"sandbox-route-test:ExternalCertificateValidationFailed",
			},
			expectedError: true,
		},
		{
			name: "route added with externalCertificate allowed and correct secret but got error from secretManager",
			route: &routev1.Route{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "route-test",
					Namespace: "sandbox",
				},
				Spec: routev1.RouteSpec{
					TLS: &routev1.TLSConfig{
						ExternalCertificate: &routev1.LocalObjectReference{
							Name: "tls-secret",
						},
					},
				},
			},
			secretManager: fake.SecretManager{
				Secret: fakeSecret("sandbox", "tls-secret", corev1.SecretTypeTLS, map[string][]byte{
					"tls.crt": []byte("my-crt"),
					"tls.key": []byte("my-key"),
				}),
				Err: fmt.Errorf("something"),
			},
			eventType:     watch.Added,
			allow:         true,
			expectedError: true,
		},
		{
			name: "route added with externalCertificate allowed and correct secret",
			route: &routev1.Route{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "route-test",
					Namespace: "sandbox",
				},
				Spec: routev1.RouteSpec{
					TLS: &routev1.TLSConfig{
						ExternalCertificate: &routev1.LocalObjectReference{
							Name: "tls-secret",
						},
					},
				},
			},
			secretManager: fake.SecretManager{
				Secret: fakeSecret("sandbox", "tls-secret", corev1.SecretTypeTLS, map[string][]byte{
					"tls.crt": []byte("my-crt"),
					"tls.key": []byte("my-key"),
				}),
			},
			eventType: watch.Added,
			allow:     true,
			expectedRoute: &routev1.Route{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "route-test",
					Namespace: "sandbox",
				},
				Spec: routev1.RouteSpec{
					TLS: &routev1.TLSConfig{
						ExternalCertificate: &routev1.LocalObjectReference{
							Name: "tls-secret",
						},
						Certificate: "my-crt",
						Key:         "my-key",
					},
				},
			},
			expectedEventType: watch.Added,
		},
		{
			name: "route added without externalCertificate",
			route: &routev1.Route{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "route-test",
					Namespace: "sandbox",
				},
				Spec: routev1.RouteSpec{},
			},
			eventType: watch.Added,
			expectedRoute: &routev1.Route{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "route-test",
					Namespace: "sandbox",
				},
				Spec: routev1.RouteSpec{},
			},
			expectedEventType: watch.Added,
		},

		// scenarios when route is updated (old route without externalCertificate, new route with externalCertificate)
		{
			name: "route updated: old route without externalCertificate, new route with externalCertificate denied",
			route: &routev1.Route{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "route-test",
					Namespace: "sandbox",
				},
				Spec: routev1.RouteSpec{
					TLS: &routev1.TLSConfig{
						ExternalCertificate: &routev1.LocalObjectReference{
							Name: "tls-secret",
						},
					},
				},
			},
			secretManager: fake.SecretManager{
				Secret: fakeSecret("sandbox", "tls-secret", corev1.SecretTypeTLS, map[string][]byte{
					"tls.crt": []byte("my-crt"),
					"tls.key": []byte("my-key"),
				}),
				IsPresent: false,
			},
			allow:     false,
			eventType: watch.Modified,
			expectedRoute: &routev1.Route{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "route-test",
					Namespace: "sandbox",
				},
				Spec: routev1.RouteSpec{
					TLS: &routev1.TLSConfig{
						ExternalCertificate: &routev1.LocalObjectReference{
							Name: "tls-secret",
						},
					},
				},
			},
			expectedEventType: watch.Deleted,
			expectedRejections: []string{
				"sandbox-route-test:ExternalCertificateValidationFailed",
			},
			expectedError: true,
		},
		{
			name: "route updated: old route without externalCertificate, new route with externalCertificate allowed but secret not found",
			route: &routev1.Route{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "route-test",
					Namespace: "sandbox",
				},
				Spec: routev1.RouteSpec{
					TLS: &routev1.TLSConfig{
						ExternalCertificate: &routev1.LocalObjectReference{
							Name: "tls-secret",
						},
					},
				},
			},
			secretManager: fake.SecretManager{
				Secret: fakeSecret("other-sandbox", "tls-secret", corev1.SecretTypeTLS, map[string][]byte{
					"tls.crt": []byte("my-crt"),
					"tls.key": []byte("my-key"),
				}),
				IsPresent: false,
			},
			allow:     true,
			eventType: watch.Modified,
			expectedRoute: &routev1.Route{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "route-test",
					Namespace: "sandbox",
				},
				Spec: routev1.RouteSpec{
					TLS: &routev1.TLSConfig{
						ExternalCertificate: &routev1.LocalObjectReference{
							Name: "tls-secret",
						},
					},
				},
			},
			expectedEventType: watch.Deleted,
			expectedRejections: []string{
				"sandbox-route-test:ExternalCertificateValidationFailed",
			},
			expectedError: true,
		},
		{
			name: "route updated: old route without externalCertificate, new route with externalCertificate allowed but secret of incorrect type",
			route: &routev1.Route{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "route-test",
					Namespace: "sandbox",
				},
				Spec: routev1.RouteSpec{
					TLS: &routev1.TLSConfig{
						ExternalCertificate: &routev1.LocalObjectReference{
							Name: "tls-secret",
						},
					},
				},
			},
			secretManager: fake.SecretManager{
				Secret: fakeSecret("sandbox", "tls-secret", corev1.SecretTypeBasicAuth, map[string][]byte{
					"tls.crt": []byte("my-crt"),
					"tls.key": []byte("my-key"),
				}),
				IsPresent: false,
			},
			allow:     true,
			eventType: watch.Modified,
			expectedRoute: &routev1.Route{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "route-test",
					Namespace: "sandbox",
				},
				Spec: routev1.RouteSpec{
					TLS: &routev1.TLSConfig{
						ExternalCertificate: &routev1.LocalObjectReference{
							Name: "tls-secret",
						},
					},
				},
			},
			expectedEventType: watch.Deleted,
			expectedRejections: []string{
				"sandbox-route-test:ExternalCertificateValidationFailed",
			},
			expectedError: true,
		},
		{
			name: "route updated: old route without externalCertificate, new route with externalCertificate allowed and correct secret but got error from secretManager",
			route: &routev1.Route{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "route-test",
					Namespace: "sandbox",
				},
				Spec: routev1.RouteSpec{
					TLS: &routev1.TLSConfig{
						ExternalCertificate: &routev1.LocalObjectReference{
							Name: "tls-secret",
						},
					},
				},
			},
			secretManager: fake.SecretManager{
				Secret: fakeSecret("sandbox", "tls-secret", corev1.SecretTypeTLS, map[string][]byte{
					"tls.crt": []byte("my-crt"),
					"tls.key": []byte("my-key"),
				}),
				IsPresent: false,
				Err:       fmt.Errorf("something"),
			},
			allow:         true,
			eventType:     watch.Modified,
			expectedError: true,
		},
		{
			name: "route updated: old route without externalCertificate, new route with externalCertificate allowed and correct secret",
			route: &routev1.Route{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "route-test",
					Namespace: "sandbox",
				},
				Spec: routev1.RouteSpec{
					TLS: &routev1.TLSConfig{
						ExternalCertificate: &routev1.LocalObjectReference{
							Name: "tls-secret",
						},
					},
				},
			},
			secretManager: fake.SecretManager{
				Secret: fakeSecret("sandbox", "tls-secret", corev1.SecretTypeTLS, map[string][]byte{
					"tls.crt": []byte("my-crt"),
					"tls.key": []byte("my-key"),
				}),
				IsPresent: false,
			},
			allow:     true,
			eventType: watch.Modified,
			expectedRoute: &routev1.Route{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "route-test",
					Namespace: "sandbox",
				},
				Spec: routev1.RouteSpec{
					TLS: &routev1.TLSConfig{
						ExternalCertificate: &routev1.LocalObjectReference{
							Name: "tls-secret",
						},
						Certificate: "my-crt",
						Key:         "my-key",
					},
				},
			},
			expectedEventType: watch.Modified,
		},

		// scenarios when route is updated (old route with externalCertificate, new route with same externalCertificate)
		{
			name: "route updated: old route with externalCertificate, new route with same externalCertificate denied",
			route: &routev1.Route{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "route-test",
					Namespace: "sandbox",
				},
				Spec: routev1.RouteSpec{
					TLS: &routev1.TLSConfig{
						ExternalCertificate: &routev1.LocalObjectReference{
							Name: "tls-secret",
						},
					},
				},
			},
			secretManager: fake.SecretManager{
				Secret: fakeSecret("sandbox", "tls-secret", corev1.SecretTypeTLS, map[string][]byte{
					"tls.crt": []byte("my-crt"),
					"tls.key": []byte("my-key"),
				}),
				IsPresent:  true,
				SecretName: "tls-secret",
			},
			allow:     false,
			eventType: watch.Modified,
			expectedRoute: &routev1.Route{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "route-test",
					Namespace: "sandbox",
				},
				Spec: routev1.RouteSpec{
					TLS: &routev1.TLSConfig{
						ExternalCertificate: &routev1.LocalObjectReference{
							Name: "tls-secret",
						},
					},
				},
			},
			expectedEventType: watch.Deleted,
			expectedRejections: []string{
				"sandbox-route-test:ExternalCertificateValidationFailed",
			},
			expectedError: true,
		},
		{
			name: "route updated: old route with externalCertificate, new route with same externalCertificate allowed but secret not found",
			route: &routev1.Route{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "route-test",
					Namespace: "sandbox",
				},
				Spec: routev1.RouteSpec{
					TLS: &routev1.TLSConfig{
						ExternalCertificate: &routev1.LocalObjectReference{
							Name: "tls-secret",
						},
					},
				},
			},
			secretManager: fake.SecretManager{
				Secret: fakeSecret("other-sandbox", "tls-secret", corev1.SecretTypeTLS, map[string][]byte{
					"tls.crt": []byte("my-crt"),
					"tls.key": []byte("my-key"),
				}),
				IsPresent:  true,
				SecretName: "tls-secret",
			},
			allow:     true,
			eventType: watch.Modified,
			expectedRoute: &routev1.Route{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "route-test",
					Namespace: "sandbox",
				},
				Spec: routev1.RouteSpec{
					TLS: &routev1.TLSConfig{
						ExternalCertificate: &routev1.LocalObjectReference{
							Name: "tls-secret",
						},
					},
				},
			},
			expectedEventType: watch.Deleted,
			expectedRejections: []string{
				"sandbox-route-test:ExternalCertificateValidationFailed",
			},
			expectedError: true,
		},
		{
			name: "route updated: old route with externalCertificate, new route with same externalCertificate allowed but secret of incorrect type",
			route: &routev1.Route{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "route-test",
					Namespace: "sandbox",
				},
				Spec: routev1.RouteSpec{
					TLS: &routev1.TLSConfig{
						ExternalCertificate: &routev1.LocalObjectReference{
							Name: "tls-secret",
						},
					},
				},
			},
			secretManager: fake.SecretManager{
				Secret: fakeSecret("sandbox", "tls-secret", corev1.SecretTypeBasicAuth, map[string][]byte{
					"tls.crt": []byte("my-crt"),
					"tls.key": []byte("my-key"),
				}),
				IsPresent:  true,
				SecretName: "tls-secret",
			},
			allow:     true,
			eventType: watch.Modified,
			expectedRoute: &routev1.Route{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "route-test",
					Namespace: "sandbox",
				},
				Spec: routev1.RouteSpec{
					TLS: &routev1.TLSConfig{
						ExternalCertificate: &routev1.LocalObjectReference{
							Name: "tls-secret",
						},
					},
				},
			},
			expectedEventType: watch.Deleted,
			expectedRejections: []string{
				"sandbox-route-test:ExternalCertificateValidationFailed",
			},
			expectedError: true,
		},
		{
			name: "route updated: old route with externalCertificate, new route with same externalCertificate allowed and correct secret but got error from secretManager",
			route: &routev1.Route{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "route-test",
					Namespace: "sandbox",
				},
				Spec: routev1.RouteSpec{
					TLS: &routev1.TLSConfig{
						ExternalCertificate: &routev1.LocalObjectReference{
							Name: "tls-secret",
						},
					},
				},
			},
			secretManager: fake.SecretManager{
				Secret: fakeSecret("sandbox", "tls-secret", corev1.SecretTypeTLS, map[string][]byte{
					"tls.crt": []byte("my-crt"),
					"tls.key": []byte("my-key"),
				}),
				IsPresent:  true,
				SecretName: "tls-secret",
				Err:        fmt.Errorf("something"),
			},
			allow:     true,
			eventType: watch.Modified,
			expectedRoute: &routev1.Route{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "route-test",
					Namespace: "sandbox",
				},
				Spec: routev1.RouteSpec{
					TLS: &routev1.TLSConfig{
						ExternalCertificate: &routev1.LocalObjectReference{
							Name: "tls-secret",
						},
					},
				},
			},
			expectedEventType: watch.Deleted,
			expectedRejections: []string{
				"sandbox-route-test:ExternalCertificateGetFailed",
			},
			expectedError: true,
		},
		{
			name: "route updated: old route with externalCertificate, new route with same externalCertificate allowed and correct secret",
			route: &routev1.Route{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "route-test",
					Namespace: "sandbox",
				},
				Spec: routev1.RouteSpec{
					TLS: &routev1.TLSConfig{
						ExternalCertificate: &routev1.LocalObjectReference{
							Name: "tls-secret",
						},
					},
				},
			},
			secretManager: fake.SecretManager{
				Secret: fakeSecret("sandbox", "tls-secret", corev1.SecretTypeTLS, map[string][]byte{
					"tls.crt": []byte("my-crt"),
					"tls.key": []byte("my-key"),
				}),
				IsPresent:  true,
				SecretName: "tls-secret",
			},
			allow:     true,
			eventType: watch.Modified,
			expectedRoute: &routev1.Route{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "route-test",
					Namespace: "sandbox",
				},
				Spec: routev1.RouteSpec{
					TLS: &routev1.TLSConfig{
						ExternalCertificate: &routev1.LocalObjectReference{
							Name: "tls-secret",
						},
						Certificate: "my-crt",
						Key:         "my-key",
					},
				},
			},
			expectedEventType: watch.Modified,
		},

		// scenarios when route is updated (old route with externalCertificate, new route with different externalCertificate)
		{
			name: "route updated: old route with externalCertificate, new route with different externalCertificate denied",
			route: &routev1.Route{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "route-test",
					Namespace: "sandbox",
				},
				Spec: routev1.RouteSpec{
					TLS: &routev1.TLSConfig{
						ExternalCertificate: &routev1.LocalObjectReference{
							Name: "different-tls-secret",
						},
					},
				},
			},
			secretManager: fake.SecretManager{
				Secret: fakeSecret("sandbox", "different-tls-secret", corev1.SecretTypeTLS, map[string][]byte{
					"tls.crt": []byte("my-crt"),
					"tls.key": []byte("my-key"),
				}),
				IsPresent:  true,
				SecretName: "tls-secret", // Used by LookupRouteSecret() to get the old secretName
			},
			allow:     false,
			eventType: watch.Modified,
			expectedRoute: &routev1.Route{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "route-test",
					Namespace: "sandbox",
				},
				Spec: routev1.RouteSpec{
					TLS: &routev1.TLSConfig{
						ExternalCertificate: &routev1.LocalObjectReference{
							Name: "different-tls-secret",
						},
					},
				},
			},
			expectedEventType: watch.Deleted,
			expectedRejections: []string{
				"sandbox-route-test:ExternalCertificateValidationFailed",
			},
			expectedError: true,
		},
		{
			name: "route updated: old route with externalCertificate, new route with different externalCertificate allowed but secret not found",
			route: &routev1.Route{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "route-test",
					Namespace: "sandbox",
				},
				Spec: routev1.RouteSpec{
					TLS: &routev1.TLSConfig{
						ExternalCertificate: &routev1.LocalObjectReference{
							Name: "different-tls-secret",
						},
					},
				},
			},
			secretManager: fake.SecretManager{
				Secret: fakeSecret("other-sandbox", "different-tls-secret", corev1.SecretTypeTLS, map[string][]byte{
					"tls.crt": []byte("my-crt"),
					"tls.key": []byte("my-key"),
				}),
				IsPresent:  true,
				SecretName: "tls-secret", // Used by LookupRouteSecret() to get the old secretName
			},
			allow:     true,
			eventType: watch.Modified,
			expectedRoute: &routev1.Route{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "route-test",
					Namespace: "sandbox",
				},
				Spec: routev1.RouteSpec{
					TLS: &routev1.TLSConfig{
						ExternalCertificate: &routev1.LocalObjectReference{
							Name: "different-tls-secret",
						},
					},
				},
			},
			expectedEventType: watch.Deleted,
			expectedRejections: []string{
				"sandbox-route-test:ExternalCertificateValidationFailed",
			},
			expectedError: true,
		},
		{
			name: "route updated: old route with externalCertificate, new route with different externalCertificate allowed but secret of incorrect type",
			route: &routev1.Route{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "route-test",
					Namespace: "sandbox",
				},
				Spec: routev1.RouteSpec{
					TLS: &routev1.TLSConfig{
						ExternalCertificate: &routev1.LocalObjectReference{
							Name: "different-tls-secret",
						},
					},
				},
			},
			secretManager: fake.SecretManager{
				Secret: fakeSecret("sandbox", "different-tls-secret", corev1.SecretTypeBasicAuth, map[string][]byte{
					"tls.crt": []byte("my-crt"),
					"tls.key": []byte("my-key"),
				}),
				IsPresent:  true,
				SecretName: "tls-secret", // Used by LookupRouteSecret() to get the old secretName
			},
			allow:     true,
			eventType: watch.Modified,
			expectedRoute: &routev1.Route{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "route-test",
					Namespace: "sandbox",
				},
				Spec: routev1.RouteSpec{
					TLS: &routev1.TLSConfig{
						ExternalCertificate: &routev1.LocalObjectReference{
							Name: "different-tls-secret",
						},
					},
				},
			},
			expectedEventType: watch.Deleted,
			expectedRejections: []string{
				"sandbox-route-test:ExternalCertificateValidationFailed",
			},
			expectedError: true,
		},
		{
			name: "route updated: old route with externalCertificate, new route with different externalCertificate allowed and correct secret but got error from secretManager",
			route: &routev1.Route{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "route-test",
					Namespace: "sandbox",
				},
				Spec: routev1.RouteSpec{
					TLS: &routev1.TLSConfig{
						ExternalCertificate: &routev1.LocalObjectReference{
							Name: "different-tls-secret",
						},
					},
				},
			},
			secretManager: fake.SecretManager{
				Secret: fakeSecret("sandbox", "different-tls-secret", corev1.SecretTypeTLS, map[string][]byte{
					"tls.crt": []byte("my-crt"),
					"tls.key": []byte("my-key"),
				}),
				IsPresent:  true,
				SecretName: "tls-secret", // Used by LookupRouteSecret() to get the old secretName
				Err:        fmt.Errorf("something"),
			},
			allow:         true,
			eventType:     watch.Modified,
			expectedError: true,
		},
		{
			name: "route updated: old route with externalCertificate, new route with different externalCertificate allowed and correct secret",
			route: &routev1.Route{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "route-test",
					Namespace: "sandbox",
				},
				Spec: routev1.RouteSpec{
					TLS: &routev1.TLSConfig{
						ExternalCertificate: &routev1.LocalObjectReference{
							Name: "different-tls-secret",
						},
					},
				},
			},
			secretManager: fake.SecretManager{
				Secret: fakeSecret("sandbox", "different-tls-secret", corev1.SecretTypeTLS, map[string][]byte{
					"tls.crt": []byte("my-crt"),
					"tls.key": []byte("my-key"),
				}),
				IsPresent:  true,
				SecretName: "tls-secret", // Used by LookupRouteSecret() to get the old secretName
			},
			allow:     true,
			eventType: watch.Modified,
			expectedRoute: &routev1.Route{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "route-test",
					Namespace: "sandbox",
				},
				Spec: routev1.RouteSpec{
					TLS: &routev1.TLSConfig{
						ExternalCertificate: &routev1.LocalObjectReference{
							Name: "different-tls-secret",
						},
						Certificate: "my-crt",
						Key:         "my-key",
					},
				},
			},
			expectedEventType: watch.Modified,
		},

		// scenarios when route is updated (old route with externalCertificate, new route without externalCertificate)
		{
			name: "route updated: old route with externalCertificate, new route without externalCertificate but got error from secretManager",
			route: &routev1.Route{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "route-test",
					Namespace: "sandbox",
				},
				Spec: routev1.RouteSpec{},
			},
			secretManager: fake.SecretManager{
				IsPresent:  true,
				SecretName: "tls-secret",
				Err:        fmt.Errorf("something"),
			},
			eventType:     watch.Modified,
			expectedError: true,
		},
		{
			name: "route updated: old route with externalCertificate, new route without externalCertificate: works",
			route: &routev1.Route{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "route-test",
					Namespace: "sandbox",
				},
				Spec: routev1.RouteSpec{},
			},
			secretManager: fake.SecretManager{
				IsPresent:  true,
				SecretName: "tls-secret",
			},
			eventType: watch.Modified,
			expectedRoute: &routev1.Route{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "route-test",
					Namespace: "sandbox",
				},
				Spec: routev1.RouteSpec{},
			},
			expectedEventType: watch.Modified,
		},

		// scenario when route is updated (old route without externalCertificate, new route without externalCertificate)
		{
			name: "route updated: old route without externalCertificate, new route without externalCertificate",
			route: &routev1.Route{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "route-test",
					Namespace: "sandbox",
				},
				Spec: routev1.RouteSpec{},
			},
			secretManager: fake.SecretManager{
				IsPresent: false,
			},
			eventType: watch.Modified,
			expectedRoute: &routev1.Route{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "route-test",
					Namespace: "sandbox",
				},
				Spec: routev1.RouteSpec{},
			},
			expectedEventType: watch.Modified,
		},

		// scenarios when route is deleted
		{
			name: "route deleted without externalCertificate registered",
			route: &routev1.Route{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "route-test",
					Namespace: "sandbox",
				},
				Spec: routev1.RouteSpec{},
			},
			secretManager: fake.SecretManager{IsPresent: false},
			eventType:     watch.Deleted,
			expectedRoute: &routev1.Route{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "route-test",
					Namespace: "sandbox",
				},
				Spec: routev1.RouteSpec{},
			},
			expectedEventType: watch.Deleted,
		},
		{
			name: "route deleted with externalCertificate registered",
			route: &routev1.Route{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "route-test",
					Namespace: "sandbox",
				},
				Spec: routev1.RouteSpec{
					TLS: &routev1.TLSConfig{
						ExternalCertificate: &routev1.LocalObjectReference{
							Name: "tls-secret",
						},
					},
				},
			},
			secretManager: fake.SecretManager{
				IsPresent:  true,
				SecretName: "tls-secret",
			},
			eventType: watch.Deleted,
			expectedRoute: &routev1.Route{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "route-test",
					Namespace: "sandbox",
				},
				Spec: routev1.RouteSpec{
					TLS: &routev1.TLSConfig{
						ExternalCertificate: &routev1.LocalObjectReference{
							Name: "tls-secret",
						},
					},
				},
			},
			expectedEventType: watch.Deleted,
		},
		{
			name: "route deleted with externalCertificate registered, but got error from secretManager",
			route: &routev1.Route{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "route-test",
					Namespace: "sandbox",
				},
				Spec: routev1.RouteSpec{
					TLS: &routev1.TLSConfig{
						ExternalCertificate: &routev1.LocalObjectReference{
							Name: "tls-secret",
						},
					},
				},
			},
			secretManager: fake.SecretManager{
				IsPresent:  true,
				SecretName: "tls-secret",
				Err:        fmt.Errorf("something"),
			},
			eventType:     watch.Deleted,
			expectedError: true,
		},
	}

	for _, s := range scenarios {
		t.Run(s.name, func(t *testing.T) {
			p := &fakePlugin{}
			recorder := &statusRecorder{
				doneCh: make(chan struct{}),
			}
			rsm := NewRouteSecretManager(p, recorder, &s.secretManager, testRouterName, &testSecretGetter{namespace: s.route.Namespace, secret: s.secretManager.Secret}, &routeLister{}, &testSARCreator{allow: s.allow})

			gotErr := rsm.HandleRoute(s.eventType, s.route)
			if (gotErr != nil) != s.expectedError {
				t.Fatalf("expected error to be %t, but got %t", s.expectedError, gotErr != nil)
			}
			if !reflect.DeepEqual(s.expectedRoute, p.route) {
				t.Fatalf("expected route for next plugin %v, but got %v", s.expectedRoute, p.route)
			}
			if s.expectedEventType != p.t {
				t.Fatalf("expected %s event for next plugin, but got %s", s.expectedEventType, p.t)
			}
			if !reflect.DeepEqual(s.expectedRejections, recorder.rejections) {
				t.Fatalf("expected rejections %v, but got %v", s.expectedRejections, recorder.rejections)
			}
			if _, exists := rsm.deletedSecrets.Load(generateKey(s.route.Namespace, s.route.Name)); exists {
				t.Fatalf("expected deletedSecrets to not have %q key", generateKey(s.route.Namespace, s.route.Name))
			}
		})
	}
}

func TestSecretUpdate(t *testing.T) {

	scenarios := []struct {
		name                string
		route               *routev1.Route
		isRouteAdmittedTrue bool
	}{
		{
			name: "Secret updated when route status was Admitted=False",
			route: &routev1.Route{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "route-test",
					Namespace: "sandbox",
				},
				Spec: routev1.RouteSpec{
					TLS: &routev1.TLSConfig{
						ExternalCertificate: &routev1.LocalObjectReference{
							Name: "tls-secret",
						},
					},
				},
				Status: routev1.RouteStatus{
					Ingress: []routev1.RouteIngress{
						{
							Conditions: []routev1.RouteIngressCondition{
								{
									Type:   routev1.RouteAdmitted,
									Status: corev1.ConditionFalse,
								},
							},
						},
					},
				},
			},
		},
		{
			name: "Secret updated when route status was Admitted=True by the same router",
			route: &routev1.Route{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "route-test",
					Namespace: "sandbox",
				},
				Spec: routev1.RouteSpec{
					TLS: &routev1.TLSConfig{
						ExternalCertificate: &routev1.LocalObjectReference{
							Name: "tls-secret",
						},
					},
				},
				Status: routev1.RouteStatus{
					Ingress: []routev1.RouteIngress{
						{
							RouterName: testRouterName,
							Conditions: []routev1.RouteIngressCondition{
								{
									Type:   routev1.RouteAdmitted,
									Status: corev1.ConditionTrue,
								},
							},
						},
					},
				},
			},
			isRouteAdmittedTrue: true,
		},
		{
			name: "Secret updated when route status was Admitted=True by some different router",
			route: &routev1.Route{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "route-test",
					Namespace: "sandbox",
				},
				Spec: routev1.RouteSpec{
					TLS: &routev1.TLSConfig{
						ExternalCertificate: &routev1.LocalObjectReference{
							Name: "tls-secret",
						},
					},
				},
				Status: routev1.RouteStatus{
					Ingress: []routev1.RouteIngress{
						{
							RouterName: "some-different-router",
							Conditions: []routev1.RouteIngressCondition{
								{
									Type:   routev1.RouteAdmitted,
									Status: corev1.ConditionTrue,
								},
							},
						},
					},
				},
			},
		},
	}

	for _, s := range scenarios {
		t.Run(s.name, func(t *testing.T) {
			recorder := &statusRecorder{
				doneCh: make(chan struct{}),
			}
			lister := &routeLister{items: []*routev1.Route{s.route}}
			rsm := NewRouteSecretManager(&fakePlugin{}, recorder, &fake.SecretManager{}, testRouterName, &testSecretGetter{}, lister, &testSARCreator{})

			// Create a fakeSecret and start an informer for it
			secret := fakeSecret("sandbox", "tls-secret", corev1.SecretTypeTLS, map[string][]byte{})
			kubeClient := testclient.NewSimpleClientset(secret)
			informer := fakeSecretInformer(kubeClient, "sandbox", "tls-secret")
			go informer.Run(context.TODO().Done())

			// wait for informer to start
			if !cache.WaitForCacheSync(context.TODO().Done(), informer.HasSynced) {
				t.Fatal("cache not synced yet")
			}

			if _, err := informer.AddEventHandler(rsm.generateSecretHandler(s.route.Namespace, s.route.Name)); err != nil {
				t.Fatalf("failed to add handler: %v", err)
			}

			// update the secret
			updatedSecret := secret.DeepCopy()
			updatedSecret.Data = map[string][]byte{
				"tls.crt": []byte("my-crt"),
				"tls.key": []byte("my-key"),
			}
			if _, err := kubeClient.CoreV1().Secrets(s.route.Namespace).Update(context.TODO(), updatedSecret, metav1.UpdateOptions{}); err != nil {
				t.Fatalf("failed to update secret: %v", err)
			}

			// wait until route's status is updated
			<-recorder.doneCh

			expectedStatus := []string{"sandbox-route-test:ExternalCertificateSecretUpdated"}

			if s.isRouteAdmittedTrue {
				// RecordRouteUpdate will be called if `Admitted=True`
				if !reflect.DeepEqual(expectedStatus, recorder.updates) {
					t.Fatalf("expected status %v, but got %v", expectedStatus, recorder.updates)
				}
			} else {
				// RecordRouteRejection will be called if `Admitted=False`
				if !reflect.DeepEqual(expectedStatus, recorder.rejections) {
					t.Fatalf("expected status %v, but got %v", expectedStatus, recorder.rejections)
				}
			}

			if _, exists := rsm.deletedSecrets.Load(generateKey(s.route.Namespace, s.route.Name)); exists {
				t.Fatalf("expected deletedSecrets to not have %q key", generateKey(s.route.Namespace, s.route.Name))
			}

		})
	}

}

func TestSecretDelete(t *testing.T) {
	route := &routev1.Route{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "route-test",
			Namespace: "sandbox",
		},
		Spec: routev1.RouteSpec{
			TLS: &routev1.TLSConfig{
				ExternalCertificate: &routev1.LocalObjectReference{
					Name: "tls-secret",
				},
			},
		},
	}
	recorder := &statusRecorder{
		doneCh: make(chan struct{}),
	}
	lister := &routeLister{items: []*routev1.Route{route}}
	rsm := NewRouteSecretManager(&fakePlugin{}, recorder, &fake.SecretManager{}, testRouterName, &testSecretGetter{}, lister, &testSARCreator{})

	// Create a fakeSecret and start an informer for it
	secret := fakeSecret("sandbox", "tls-secret", corev1.SecretTypeTLS, map[string][]byte{})
	kubeClient := testclient.NewSimpleClientset(secret)
	informer := fakeSecretInformer(kubeClient, "sandbox", "tls-secret")
	go informer.Run(context.TODO().Done())

	// wait for informer to start
	if !cache.WaitForCacheSync(context.TODO().Done(), informer.HasSynced) {
		t.Fatal("cache not synced yet")
	}

	if _, err := informer.AddEventHandler(rsm.generateSecretHandler(route.Namespace, route.Name)); err != nil {
		t.Fatalf("failed to add handler: %v", err)
	}

	// delete the secret
	if err := kubeClient.CoreV1().Secrets(route.Namespace).Delete(context.TODO(), secret.Name, metav1.DeleteOptions{}); err != nil {
		t.Fatalf("failed to delete secret: %v", err)
	}

	<-recorder.doneCh // wait until the route's status is updated

	expectedRejections := []string{"sandbox-route-test:ExternalCertificateSecretDeleted"}
	expectedDeletedSecrets := true

	if !reflect.DeepEqual(expectedRejections, recorder.rejections) {
		t.Fatalf("expected rejections %v, but got %v", expectedRejections, recorder.rejections)
	}

	if val, _ := rsm.deletedSecrets.Load(generateKey(route.Namespace, route.Name)); !reflect.DeepEqual(val, expectedDeletedSecrets) {
		t.Fatalf("expected deletedSecrets %v, but got %v", expectedDeletedSecrets, val)
	}
}

func TestSecretRecreation(t *testing.T) {
	route := &routev1.Route{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "route-test",
			Namespace: "sandbox",
		},
		Spec: routev1.RouteSpec{
			TLS: &routev1.TLSConfig{
				ExternalCertificate: &routev1.LocalObjectReference{
					Name: "tls-secret",
				},
			},
		},
	}
	recorder := &statusRecorder{
		doneCh: make(chan struct{}),
	}
	lister := &routeLister{items: []*routev1.Route{route}}
	rsm := NewRouteSecretManager(&fakePlugin{}, recorder, &fake.SecretManager{}, testRouterName, &testSecretGetter{}, lister, &testSARCreator{})

	// Create a fakeSecret and start an informer for it
	secret := fakeSecret("sandbox", "tls-secret", corev1.SecretTypeTLS, map[string][]byte{})
	kubeClient := testclient.NewSimpleClientset(secret)
	informer := fakeSecretInformer(kubeClient, "sandbox", "tls-secret")
	go informer.Run(context.TODO().Done())

	// wait for informer to start
	if !cache.WaitForCacheSync(context.TODO().Done(), informer.HasSynced) {
		t.Fatal("cache not synced yet")
	}

	if _, err := informer.AddEventHandler(rsm.generateSecretHandler(route.Namespace, route.Name)); err != nil {
		t.Fatalf("failed to add handler: %v", err)
	}

	// delete the secret
	if err := kubeClient.CoreV1().Secrets(route.Namespace).Delete(context.TODO(), secret.Name, metav1.DeleteOptions{}); err != nil {
		t.Fatalf("failed to delete secret: %v", err)
	}

	<-recorder.doneCh // wait until the route's status is updated (deletion)

	// re-create the secret
	recorder.doneCh = make(chan struct{}) // need a new doneCh for re-creation
	if _, err := kubeClient.CoreV1().Secrets(route.Namespace).Create(context.TODO(), secret, metav1.CreateOptions{}); err != nil {
		t.Fatalf("failed to create secret: %v", err)
	}

	<-recorder.doneCh // wait until the route's status is updated (re-creation)

	expectedRejections := []string{
		"sandbox-route-test:ExternalCertificateSecretDeleted",
		"sandbox-route-test:ExternalCertificateSecretRecreated",
	}
	if !reflect.DeepEqual(expectedRejections, recorder.rejections) {
		t.Fatalf("expected rejections %v, but got %v", expectedRejections, recorder.rejections)
	}
	if _, exists := rsm.deletedSecrets.Load(generateKey(route.Namespace, route.Name)); exists {
		t.Fatalf("expected deletedSecrets to not have %q key", generateKey(route.Namespace, route.Name))
	}
}
