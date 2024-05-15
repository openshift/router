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

func TestRouteSecretManager(t *testing.T) {

	scenarios := []struct {
		name               string
		route              *routev1.Route
		secretManager      fake.SecretManager
		eventType          watch.EventType
		allow              bool
		expectedRoute      *routev1.Route
		expectedEventType  watch.EventType
		expectedRejections map[string]string
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
			expectedRejections: map[string]string{
				"sandbox-route-test": "ExternalCertificateValidationFailed",
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
			expectedRejections: map[string]string{
				"sandbox-route-test": "ExternalCertificateValidationFailed",
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
			expectedRejections: map[string]string{
				"sandbox-route-test": "ExternalCertificateValidationFailed",
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
				IsRegistered: false,
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
			expectedRejections: map[string]string{
				"sandbox-route-test": "ExternalCertificateValidationFailed",
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
				IsRegistered: false,
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
			expectedRejections: map[string]string{
				"sandbox-route-test": "ExternalCertificateValidationFailed",
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
				IsRegistered: false,
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
			expectedRejections: map[string]string{
				"sandbox-route-test": "ExternalCertificateValidationFailed",
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
				IsRegistered: false,
				Err:          fmt.Errorf("something"),
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
				IsRegistered: false,
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

		// scenarios when route is updated (old route with externalCertificate, new route with externalCertificate)
		{
			name: "route updated: old route with externalCertificate, new route with externalCertificate denied",
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
				IsRegistered: true,
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
			expectedRejections: map[string]string{
				"sandbox-route-test": "ExternalCertificateValidationFailed",
			},
			expectedError: true,
		},
		{
			name: "route updated: old route with externalCertificate, new route with externalCertificate allowed but secret not found",
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
				IsRegistered: true,
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
			expectedRejections: map[string]string{
				"sandbox-route-test": "ExternalCertificateValidationFailed",
			},
			expectedError: true,
		},
		{
			name: "route updated: old route with externalCertificate, new route with externalCertificate allowed but secret of incorrect type",
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
				IsRegistered: true,
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
			expectedRejections: map[string]string{
				"sandbox-route-test": "ExternalCertificateValidationFailed",
			},
			expectedError: true,
		},
		{
			name: "route updated: old route with externalCertificate, new route with externalCertificate allowed and correct secret but got error from secretManager",
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
				IsRegistered: true,
				Err:          fmt.Errorf("something"),
			},
			allow:         true,
			eventType:     watch.Modified,
			expectedError: true,
		},
		{
			name: "route updated: old route with externalCertificate, new route with externalCertificate allowed and correct secret",
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
				IsRegistered: true,
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
				IsRegistered: true,
				Err:          fmt.Errorf("something"),
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
				IsRegistered: true,
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
				IsRegistered: false,
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
			secretManager: fake.SecretManager{IsRegistered: false},
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
			secretManager: fake.SecretManager{IsRegistered: true},
			eventType:     watch.Deleted,
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
			secretManager: fake.SecretManager{IsRegistered: true, Err: fmt.Errorf("something")},
			eventType:     watch.Deleted,
			expectedError: true,
		},
	}

	for _, s := range scenarios {
		t.Run(s.name, func(t *testing.T) {
			p := &fakePlugin{}
			recorder := routeStatusRecorder{rejections: make(map[string]string)}

			// assign default value to expectedRejections
			if s.expectedRejections == nil {
				s.expectedRejections = map[string]string{}
			}
			rsm := NewRouteSecretManager(p, recorder, &s.secretManager, &testSecretGetter{namespace: s.route.Namespace, secret: s.secretManager.Secret}, &testSARCreator{allow: s.allow})

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
		})
	}
}

func TestSecretUpdateAndDelete(t *testing.T) {

	scenarios := []struct {
		name               string
		route              *routev1.Route
		secretManager      fake.SecretManager
		allow              bool
		deleteSecret       bool
		expectedRoute      *routev1.Route
		expectedEventType  watch.EventType
		expectedRejections map[string]string
	}{
		{
			name: "secret updated but permission revoked",
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
			allow: false,
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
			expectedRejections: map[string]string{
				"sandbox-route-test": "ExternalCertificateValidationFailed",
			},
		},
		{
			name: "secret updated with permission but got error from secretManager",
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
			allow: true,
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
			expectedRejections: map[string]string{
				"sandbox-route-test": "ExternalCertificateGetFailed",
			},
		},
		{
			name: "secret updated with permission correctly",
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
			allow: true,
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
			expectedEventType:  watch.Modified,
			expectedRejections: map[string]string{},
		},
		{
			name: "secret deleted but got error from secretManager",
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
			deleteSecret: true,
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
			expectedRejections: map[string]string{
				"sandbox-route-test": "ExternalCertificateSecretDeleted",
			},
		},
		{
			name: "secret deleted and route successfully unregistered",
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
			deleteSecret: true,
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
			expectedRejections: map[string]string{
				"sandbox-route-test": "ExternalCertificateSecretDeleted",
			},
		},
	}

	for _, s := range scenarios {
		t.Run(s.name, func(t *testing.T) {
			oldSecret := fakeSecret("sandbox", "tls-secret", corev1.SecretTypeTLS, map[string][]byte{})
			kubeClient := testclient.NewSimpleClientset(oldSecret)
			informer := fakeSecretInformer(kubeClient, "sandbox", "tls-secret")
			go informer.Run(context.TODO().Done())

			// wait for informer to start
			if !cache.WaitForCacheSync(context.TODO().Done(), informer.HasSynced) {
				t.Fatal("cache not synced yet")
			}

			p := &fakePluginDone{
				doneCh: make(chan struct{}),
			}
			recorder := routeStatusRecorder{rejections: make(map[string]string)}
			rsm := NewRouteSecretManager(p, recorder, &s.secretManager, &testSecretGetter{namespace: s.route.Namespace, secret: oldSecret}, &testSARCreator{allow: s.allow})

			if _, err := informer.AddEventHandler(rsm.generateSecretHandler(s.route)); err != nil {
				t.Fatalf("failed to add handler: %v", err)
			}

			if s.deleteSecret {
				// delete the secret
				if err := kubeClient.CoreV1().Secrets(s.route.Namespace).Delete(context.TODO(), s.secretManager.Secret.Name, metav1.DeleteOptions{}); err != nil {
					t.Fatalf("failed to delete secret: %v", err)
				}

			} else {
				// update the secret
				if _, err := kubeClient.CoreV1().Secrets(s.route.Namespace).Update(context.TODO(), s.secretManager.Secret, metav1.UpdateOptions{}); err != nil {
					t.Fatalf("failed to update secret: %v", err)
				}
			}
			// wait until p.plugin.HandleRoute() completes (required to handle race condition)
			<-p.doneCh

			if !reflect.DeepEqual(s.expectedRoute, p.route) {
				t.Fatalf("expected route for next plugin %v, but got %v", s.expectedRoute, p.route)
			}
			if s.expectedEventType != p.eventType {
				t.Fatalf("expected %s event for next plugin, but got %s", s.expectedEventType, p.eventType)
			}
			if !reflect.DeepEqual(s.expectedRejections, recorder.rejections) {
				t.Fatalf("expected rejections %v, but got %v", s.expectedRejections, recorder.rejections)
			}
		})
	}
}
