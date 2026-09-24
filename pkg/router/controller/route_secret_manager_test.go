package controller

import (
	"context"
	"fmt"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/openshift/library-go/pkg/route/secretmanager/fake"
	"github.com/openshift/router/pkg/router/routeapihelpers"

	routev1 "github.com/openshift/api/route/v1"
	authorizationv1 "k8s.io/api/authorization/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	testclient "k8s.io/client-go/kubernetes/fake"
	corev1client "k8s.io/client-go/kubernetes/typed/core/v1"
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

type statusRecorder struct {
	sync.Mutex
	rejections                 []string
	updates                    []string
	unservableInFutureVersions map[string]string
}

func (r *statusRecorder) routeKey(route *routev1.Route) string {
	return route.Namespace + "-" + route.Name
}
func (r *statusRecorder) RecordRouteRejection(route *routev1.Route, reason, message string) {
	r.Lock()
	defer r.Unlock()
	r.rejections = append(r.rejections, fmt.Sprintf("%s:%s", r.routeKey(route), reason))
}

func (r *statusRecorder) RecordRouteUpdate(route *routev1.Route, reason, message string) {
	r.Lock()
	defer r.Unlock()
	r.updates = append(r.updates, fmt.Sprintf("%s:%s", r.routeKey(route), reason))
}

func (r *statusRecorder) RecordRouteUnservableInFutureVersionsClear(route *routev1.Route) {
	r.Lock()
	defer r.Unlock()
	delete(r.unservableInFutureVersions, r.routeKey(route))
}
func (r *statusRecorder) RecordRouteUnservableInFutureVersions(route *routev1.Route, reason, message string) {
	r.Lock()
	defer r.Unlock()
	r.unservableInFutureVersions[r.routeKey(route)] = reason
}

func (r *statusRecorder) GetRejections() []string {
	r.Lock()
	defer r.Unlock()
	var res []string
	res = append(res, r.rejections...)
	return res
}

func (r *statusRecorder) GetUpdates() []string {
	r.Lock()
	defer r.Unlock()
	var res []string
	res = append(res, r.updates...)
	return res
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
		expectedUpdates    []string
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
					Annotations: map[string]string{
						certResourceVersionAnnotation: "",
					},
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
			expectedUpdates: []string{
				"sandbox-route-test:ExternalCertificateSARCompleted",
			},
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
					Annotations: map[string]string{
						certResourceVersionAnnotation: "",
					},
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
			expectedUpdates: []string{
				"sandbox-route-test:ExternalCertificateSARCompleted",
			},
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
			name: "route updated: old route with externalCertificate, new route with same externalCertificate allowed and correct secret (no SARCompleted on re-validation)",
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
					Annotations: map[string]string{
						certResourceVersionAnnotation: "",
					},
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
					Annotations: map[string]string{
						certResourceVersionAnnotation: "",
					},
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
			expectedUpdates: []string{
				"sandbox-route-test:ExternalCertificateSARCompleted",
			},
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
			routeapihelpers.ClearAsyncSARCacheForTest()
			p := &fakePlugin{}
			recorder := &statusRecorder{}
			rsm := NewRouteSecretManager(p, recorder, &s.secretManager, testRouterName, &testSecretGetter{namespace: s.route.Namespace, secret: s.secretManager.Secret}, &routeLister{items: []*routev1.Route{s.route}}, &testSARCreator{allow: s.allow})

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
			if !reflect.DeepEqual(s.expectedRejections, recorder.GetRejections()) {
				t.Fatalf("expected rejections %v, but got %v", s.expectedRejections, recorder.GetRejections())
			}
			if !reflect.DeepEqual(s.expectedUpdates, recorder.GetUpdates()) {
				t.Fatalf("expected updates %v, but got %v", s.expectedUpdates, recorder.GetUpdates())
			}
			if _, exists := rsm.deletedSecrets.Load(routeKey(s.route.Namespace, s.route.Name)); exists {
				t.Fatalf("expected deletedSecrets to not have %q key", routeKey(s.route.Namespace, s.route.Name))
			}
		})
	}
}

// TestPopulateRouteTLSRace verifies that HandleRoute's DeepCopy of the route
// prevents data races between the main controller goroutine (which populates
// TLS cert/key fields) and informer goroutines (which read the same route via
// DeepCopy, as the secret handler's UpdateFunc does in production).
// Run with -race to confirm no races are detected.
func TestPopulateRouteTLSRace(t *testing.T) {
	routeapihelpers.ClearAsyncSARCacheForTest()

	secret := fakeSecret("sandbox", "tls-secret", corev1.SecretTypeTLS, map[string][]byte{
		"tls.crt": []byte("my-crt"),
		"tls.key": []byte("my-key"),
	})

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

	// The routeLister returns a pointer to the SAME route object,
	// faithfully reproducing the informer cache behavior in production.
	lister := &routeLister{items: []*routev1.Route{route}}

	secretMgr := &fake.SecretManager{
		Secret:     secret,
		IsPresent:  true,
		SecretName: "tls-secret",
	}

	rsm := NewRouteSecretManager(
		&fakePlugin{},
		&statusRecorder{},
		secretMgr,
		testRouterName,
		&testSecretGetter{namespace: "sandbox", secret: secret},
		lister,
		&testSARCreator{allow: true},
	)

	// First call to register the route with the secret manager.
	if err := rsm.HandleRoute(watch.Added, route); err != nil {
		t.Fatalf("initial HandleRoute failed: %v", err)
	}

	var wg sync.WaitGroup
	const iterations = 100

	// Goroutine A: simulates the main controller goroutine calling
	// HandleRoute, which calls populateRouteTLSFromSecret and WRITES
	// to route.Spec.TLS.Certificate and route.Spec.TLS.Key.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			routeapihelpers.ClearAsyncSARCacheForTest()
			if err := rsm.HandleRoute(watch.Modified, route); err != nil {
				t.Errorf("HandleRoute iteration %d: %v", i, err)
				return
			}
		}
	}()

	// Goroutine B: simulates the informer goroutine reading the same
	// shared route object. In production, the secret handler's UpdateFunc
	// calls isRouteAdmittedTrue(route.DeepCopy(), ...) which READS all
	// fields including the TLS fields being written by goroutine A.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			_ = route.DeepCopy()
		}
	}()

	wg.Wait()
}

// TestSARCompletedOnlyOnAdded verifies that HandleRoute emits
// RecordRouteUpdate(SARCompleted) only on watch.Added events, not on
// watch.Modified. Emitting on Modified creates a re-enqueue feedback
// loop and can re-admit routes that were rejected by secret handlers.
func TestSARCompletedOnlyOnAdded(t *testing.T) {
	routeapihelpers.ClearAsyncSARCacheForTest()

	secret := fakeSecret("sandbox", "tls-secret", corev1.SecretTypeTLS, map[string][]byte{
		"tls.crt": []byte("my-crt"),
		"tls.key": []byte("my-key"),
	})

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

	lister := &routeLister{items: []*routev1.Route{route}}
	recorder := &statusRecorder{}

	rsm := NewRouteSecretManager(
		&fakePlugin{},
		recorder,
		&fake.SecretManager{
			Secret:     secret,
			IsPresent:  true,
			SecretName: "tls-secret",
		},
		testRouterName,
		&testSecretGetter{namespace: "sandbox", secret: secret},
		lister,
		&testSARCreator{allow: true},
	)

	// HandleRoute(Added): should emit SARCompleted.
	if err := rsm.HandleRoute(watch.Added, route); err != nil {
		t.Fatalf("HandleRoute(Added) failed: %v", err)
	}
	updates := recorder.GetUpdates()
	if len(updates) != 1 || updates[0] != "sandbox-route-test:ExternalCertificateSARCompleted" {
		t.Fatalf("expected one SARCompleted on Added, got: %v", updates)
	}

	// HandleRoute(Modified): should NOT emit SARCompleted.
	routeapihelpers.ClearAsyncSARCacheForTest()
	if err := rsm.HandleRoute(watch.Modified, route); err != nil {
		t.Fatalf("HandleRoute(Modified) failed: %v", err)
	}

	updates = recorder.GetUpdates()
	sarCount := 0
	for _, u := range updates {
		if u == "sandbox-route-test:ExternalCertificateSARCompleted" {
			sarCount++
		}
	}
	if sarCount != 1 {
		t.Fatalf("expected exactly 1 SARCompleted (from Added only), got %d: %v", sarCount, updates)
	}
}

// TestDeletedSecretDoesNotGetReadmitted reproduces the failure mode from
// the Hypershift conformance test "the secret is deleted then routes are
// not reachable": after the DeleteFunc rejects the route (Admitted=False),
// the status update triggers a re-enqueue. If the subsequent HandleRoute
// call succeeds (because GetSecret still returns the secret from cache
// during the deletion propagation window), the SARCompleted write must
// NOT flip the route back to Admitted=True. Otherwise the route bounces
// between admitted and rejected, and the E2E test polls for Admitted=False
// until timeout.
//
// Guards against a regression where the SARCompleted write re-admits
// the route during the informer cache propagation window after deletion.
func TestDeletedSecretDoesNotGetReadmitted(t *testing.T) {
	routeapihelpers.ClearAsyncSARCacheForTest()

	secret := fakeSecret("sandbox", "tls-secret", corev1.SecretTypeTLS, map[string][]byte{
		"tls.crt": []byte("my-crt"),
		"tls.key": []byte("my-key"),
	})

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

	lister := &routeLister{items: []*routev1.Route{route}}
	recorder := &statusRecorder{}
	secretMgr := &fake.SecretManager{
		Secret:     secret,
		IsPresent:  true,
		SecretName: "tls-secret",
	}

	rsm := NewRouteSecretManager(
		&fakePlugin{},
		recorder,
		secretMgr,
		testRouterName,
		&testSecretGetter{namespace: "sandbox", secret: secret},
		lister,
		&testSARCreator{allow: true},
	)

	// Step 1: Admit the route — should succeed and write SARCompleted.
	if err := rsm.HandleRoute(watch.Added, route); err != nil {
		t.Fatalf("initial HandleRoute failed: %v", err)
	}
	updates := recorder.GetUpdates()
	if len(updates) != 1 || updates[0] != "sandbox-route-test:ExternalCertificateSARCompleted" {
		t.Fatalf("expected SARCompleted after initial admission, got: %v", updates)
	}

	// Step 2: Simulate secret deletion via the handler.
	// This records a rejection (Admitted=False, ValidationFailed).
	handler := rsm.generateSecretHandler(route.Namespace, route.Name)
	handler.DeleteFunc(secret)

	rejections := recorder.GetRejections()
	if len(rejections) != 1 || rejections[0] != "sandbox-route-test:ExternalCertificateValidationFailed" {
		t.Fatalf("expected ValidationFailed rejection after delete, got: %v", rejections)
	}

	// Step 3: Simulate the re-enqueue triggered by the rejection status
	// update. The route still has externalCertificate in its spec. The
	// SecretManager still returns the secret (simulating the informer
	// cache race where the delete hasn't propagated yet). validate() and
	// populateRouteTLSFromSecret() both succeed.
	//
	// On unfixed code, HandleRoute succeeds, then the SARCompleted guard
	// sees the route has Admitted=False (no ext-cert admitted reason) and
	// writes SARCompleted — flipping the route BACK to Admitted=True.
	// This re-admission is the bug that causes the E2E test to timeout.
	routeapihelpers.ClearAsyncSARCacheForTest()
	if err := rsm.HandleRoute(watch.Modified, route); err != nil {
		t.Fatalf("re-enqueued HandleRoute failed: %v", err)
	}

	// Verify the route was NOT re-admitted. After the DeleteFunc rejection,
	// no new SARCompleted update should have been recorded.
	allUpdates := recorder.GetUpdates()
	sarCount := 0
	for _, u := range allUpdates {
		if u == "sandbox-route-test:ExternalCertificateSARCompleted" {
			sarCount++
		}
	}
	if sarCount != 1 {
		t.Fatalf("expected exactly 1 SARCompleted (from initial admission), got %d: %v",
			sarCount, allUpdates)
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
			recorder := &statusRecorder{}
			lister := &routeLister{items: []*routev1.Route{s.route}}

			// Create a fakeSecret
			secret := fakeSecret("sandbox", "tls-secret", corev1.SecretTypeTLS, map[string][]byte{})

			// update the secret
			updatedSecret := secret.DeepCopy()
			updatedSecret.ResourceVersion = "200"
			updatedSecret.Data = map[string][]byte{
				"tls.crt": []byte("my-crt"),
				"tls.key": []byte("my-key"),
			}

			plugin := &fakePlugin{}
			rsm := NewRouteSecretManager(
				plugin,
				recorder,
				&fake.SecretManager{Secret: updatedSecret, IsPresent: true, SecretName: "tls-secret"},
				testRouterName,
				&testSecretGetter{namespace: "sandbox", secret: updatedSecret},
				lister,
				// SAR is set to deny — UpdateFunc must NOT call validate()
				// synchronously, so SAR denial should not block the cert
				// refresh. The delayed re-check goroutine will check SAR
				// later, but we only assert immediate results here.
				&testSARCreator{allow: false},
			)

			// Get the handler
			handler := rsm.generateSecretHandler(s.route.Namespace, s.route.Name)

			// Call the handler directly (synchronous — the delayed goroutine
			// fires in the background but we only check immediate results).
			handler.UpdateFunc(secret, updatedSecret)

			// UpdateFunc always calls RecordRouteUpdate (keeps Admitted=True)
			// to ensure the route remains reachable while the new cert is
			// picked up on re-enqueue.
			expectedUpdates := []string{"sandbox-route-test:ExternalCertificateSecretUpdated"}
			if !reflect.DeepEqual(expectedUpdates, recorder.GetUpdates()) {
				t.Fatalf("expected updates %v, but got %v", expectedUpdates, recorder.GetUpdates())
			}

			// Verify HandleRoute was called with Modified event and the
			// cert data was populated from the secret.
			if plugin.t != watch.Modified {
				t.Fatalf("expected HandleRoute called with Modified, got %v", plugin.t)
			}
			if plugin.route == nil {
				t.Fatal("expected HandleRoute to receive a route")
			}
			if plugin.route.Spec.TLS.Certificate != "my-crt" {
				t.Fatalf("expected cert 'my-crt', got %q", plugin.route.Spec.TLS.Certificate)
			}
			if plugin.route.Spec.TLS.Key != "my-key" {
				t.Fatalf("expected key 'my-key', got %q", plugin.route.Spec.TLS.Key)
			}

			// Verify Commit() was called to trigger the HAProxy reload.
			if plugin.commits != 1 {
				t.Fatalf("expected Commit() called once, got %d", plugin.commits)
			}

			// Verify the cert-resource-version annotation was set.
			if v := plugin.route.Annotations[certResourceVersionAnnotation]; v != "200" {
				t.Fatalf("expected cert-resource-version annotation '200', got %q", v)
			}

			if _, exists := rsm.deletedSecrets.Load(routeKey(s.route.Namespace, s.route.Name)); exists {
				t.Fatalf("expected deletedSecrets to not have %q key", routeKey(s.route.Namespace, s.route.Name))
			}

		})
	}

}

// TestSecretUpdateDelayedRecheck verifies the delayed re-check spawned by
// UpdateFunc only writes a status update when it actually needs to: it must
// be a silent no-op when SAR still passes (the common case: secret rotated,
// RBAC unchanged), and it must reject the route when SAR now fails (RBAC was
// revoked and has since propagated). Making the common case a no-op avoids
// doubling the load on the router's status-write queue for every secret
// update, regardless of whether RBAC ever changes.
func TestSecretUpdateDelayedRecheck(t *testing.T) {
	// Shrink the delay so the test doesn't sleep for real seconds. Stored
	// atomically since a prior test's spawned goroutine (e.g. TestSecretUpdate,
	// which uses the real default delay) may still be sleeping on this value.
	originalDelay := secretUpdateRecheckDelay.Load()
	secretUpdateRecheckDelay.Store(int64(10 * time.Millisecond))
	defer secretUpdateRecheckDelay.Store(originalDelay)

	scenarios := []struct {
		name               string
		allow              bool
		expectedRejections []string
	}{
		{
			name:               "SAR still allowed: delayed re-check is a no-op",
			allow:              true,
			expectedRejections: nil,
		},
		{
			name:  "SAR now denied: delayed re-check rejects the route",
			allow: false,
			expectedRejections: []string{
				"sandbox-route-test:ExternalCertificateValidationFailed",
			},
		},
	}

	for _, s := range scenarios {
		t.Run(s.name, func(t *testing.T) {
			routeapihelpers.ClearAsyncSARCacheForTest()

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

			recorder := &statusRecorder{}
			lister := &routeLister{items: []*routev1.Route{route}}
			secret := fakeSecret("sandbox", "tls-secret", corev1.SecretTypeTLS, map[string][]byte{
				"tls.crt": []byte("my-crt"),
				"tls.key": []byte("my-key"),
			})
			updatedSecret := secret.DeepCopy()
			updatedSecret.Data = map[string][]byte{
				"tls.crt": []byte("new-crt"),
				"tls.key": []byte("new-key"),
			}
			// UpdateFunc reads the secret synchronously (no SAR check)
			// and defers the SAR check to the delayed re-check goroutine.
			rsm := NewRouteSecretManager(
				&fakePlugin{},
				recorder,
				&fake.SecretManager{Secret: updatedSecret, IsPresent: true, SecretName: "tls-secret"},
				testRouterName,
				&testSecretGetter{namespace: "sandbox", secret: secret},
				lister,
				&testSARCreator{allow: s.allow},
			)

			handler := rsm.generateSecretHandler(route.Namespace, route.Name)
			handler.UpdateFunc(secret, updatedSecret)

			// Wait for the delayed re-check goroutine to finish rather than
			// sleeping a fixed amount, keeping the test fast and non-flaky.
			deadline := time.Now().Add(2 * time.Second)
			for time.Now().Before(deadline) {
				if len(recorder.GetRejections()) == len(s.expectedRejections) {
					break
				}
				time.Sleep(time.Millisecond)
			}

			if !reflect.DeepEqual(s.expectedRejections, recorder.GetRejections()) {
				t.Fatalf("expected rejections %v, but got %v", s.expectedRejections, recorder.GetRejections())
			}

			// The immediate write always happens regardless of the delayed
			// re-check's outcome.
			expectedUpdates := []string{"sandbox-route-test:ExternalCertificateSecretUpdated"}
			if !reflect.DeepEqual(expectedUpdates, recorder.GetUpdates()) {
				t.Fatalf("expected updates %v, but got %v", expectedUpdates, recorder.GetUpdates())
			}
		})
	}
}

// TestInFlightRegistrationDoesNotReAdmitDeletedSecretRoute reproduces the
// production failure of the same "the secret is deleted then routes are not
// reachable" E2E test, but via a different trigger than
// TestDeletedSecretDoesNotGetReadmitted: a watch.Added registration that was
// already in flight when the secret got deleted, rather than a re-validation
// after the rejection.
//
// validateAndRegister (driven by the route informer, on the router's main
// control loop) and DeleteFunc (driven by the secret informer, on its own
// goroutine) run concurrently. If DeleteFunc's rejection lands first but the
// already-in-flight Added registration finishes afterward, the `registered`
// flag alone does not protect against it -- registered is legitimately true
// for a first-time registration, so without also checking deletedSecrets the
// late-finishing SARCompleted write silently re-admits a route that was just
// correctly rejected.
func TestInFlightRegistrationDoesNotReAdmitDeletedSecretRoute(t *testing.T) {
	routeapihelpers.ClearAsyncSARCacheForTest()

	secret := fakeSecret("sandbox", "tls-secret", corev1.SecretTypeTLS, map[string][]byte{
		"tls.crt": []byte("my-crt"),
		"tls.key": []byte("my-key"),
	})

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

	lister := &routeLister{items: []*routev1.Route{route}}
	recorder := &statusRecorder{}

	rsm := NewRouteSecretManager(
		&fakePlugin{},
		recorder,
		&fake.SecretManager{
			Secret:     secret,
			IsPresent:  true,
			SecretName: "tls-secret",
		},
		testRouterName,
		&testSecretGetter{namespace: "sandbox", secret: secret},
		lister,
		&testSARCreator{allow: true},
	)

	// Simulate the secret being deleted BEFORE the route's own watch.Added
	// registration (started earlier, e.g. right after route creation) gets a
	// chance to finish. This is exactly DeleteFunc's own behavior.
	handler := rsm.generateSecretHandler(route.Namespace, route.Name)
	handler.DeleteFunc(secret)

	rejections := recorder.GetRejections()
	if len(rejections) != 1 || rejections[0] != "sandbox-route-test:ExternalCertificateValidationFailed" {
		t.Fatalf("expected ValidationFailed rejection after delete, got: %v", rejections)
	}

	// Now the in-flight registration finishes. The fake SecretManager still
	// returns the secret (simulating the informer cache race where the
	// delete hasn't propagated to GetSecret's cache yet), so validate() and
	// populateRouteTLSFromSecret() both succeed and registered=true.
	//
	// On unfixed code, the SARCompleted guard only checks `registered`, so
	// it writes SARCompleted here -- flipping the route BACK to Admitted=True
	// even though the secret is already gone.
	if err := rsm.HandleRoute(watch.Added, route); err != nil {
		t.Fatalf("in-flight HandleRoute(Added) failed: %v", err)
	}

	updates := recorder.GetUpdates()
	for _, u := range updates {
		if u == "sandbox-route-test:ExternalCertificateSARCompleted" {
			t.Fatalf("expected no SARCompleted write for a route whose secret was already deleted, got updates: %v", updates)
		}
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
	recorder := &statusRecorder{}
	lister := &routeLister{items: []*routev1.Route{route}}
	rsm := NewRouteSecretManager(&fakePlugin{}, recorder, &fake.SecretManager{}, testRouterName, &testSecretGetter{}, lister, &testSARCreator{})

	// Create a fakeSecret
	secret := fakeSecret("sandbox", "tls-secret", corev1.SecretTypeTLS, map[string][]byte{})

	// Get the handler
	handler := rsm.generateSecretHandler(route.Namespace, route.Name)

	// delete the secret by calling the handler directly
	handler.DeleteFunc(secret)

	expectedRejections := []string{
		"sandbox-route-test:ExternalCertificateValidationFailed",
	}
	expectedDeletedSecrets := true

	if !reflect.DeepEqual(expectedRejections, recorder.GetRejections()) {
		t.Fatalf("expected rejections %v, but got %v", expectedRejections, recorder.GetRejections())
	}

	if val, _ := rsm.deletedSecrets.Load(routeKey(route.Namespace, route.Name)); !reflect.DeepEqual(val, expectedDeletedSecrets) {
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
	recorder := &statusRecorder{}
	lister := &routeLister{items: []*routev1.Route{route}}
	rsm := NewRouteSecretManager(&fakePlugin{}, recorder, &fake.SecretManager{}, testRouterName, &testSecretGetter{}, lister, &testSARCreator{})

	// Create a fakeSecret
	secret := fakeSecret("sandbox", "tls-secret", corev1.SecretTypeTLS, map[string][]byte{})

	// Get the handler
	handler := rsm.generateSecretHandler(route.Namespace, route.Name)

	// 1. delete the secret
	handler.DeleteFunc(secret)

	// 2. re-create the secret
	handler.AddFunc(secret)

	expectedRejections := []string{
		"sandbox-route-test:ExternalCertificateValidationFailed",
		"sandbox-route-test:ExternalCertificateSecretRecreated",
	}
	if !reflect.DeepEqual(expectedRejections, recorder.GetRejections()) {
		t.Fatalf("expected rejections %v, but got %v", expectedRejections, recorder.GetRejections())
	}
	if _, exists := rsm.deletedSecrets.Load(routeKey(route.Namespace, route.Name)); exists {
		t.Fatalf("expected deletedSecrets to not have %q key", routeKey(route.Namespace, route.Name))
	}
}

// TestLockRouteSerializesSameKey verifies lockRoute provides genuine mutual
// exclusion per key: concurrent callers for the SAME route key never
// overlap their critical sections, while callers for DIFFERENT keys don't
// block each other at all. This is the core guarantee that closes the race
// between HandleRoute's periodic re-validation and UpdateFunc's
// secret-triggered refresh (see the routeLocks field comment) -- both call
// lockRoute with the same "namespace/routeName" key, so proving the
// primitive itself is correct here is what makes that higher-level claim
// trustworthy without needing to reproduce the full race end-to-end.
func TestLockRouteSerializesSameKey(t *testing.T) {
	rsm := &RouteSecretManager{}

	const goroutines = 50
	var active int32
	var maxObservedActive int32
	var wg sync.WaitGroup

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			unlock := rsm.lockRoute(routeKey("sandbox", "route-test"))
			defer unlock()

			n := atomic.AddInt32(&active, 1)
			for {
				m := atomic.LoadInt32(&maxObservedActive)
				if n <= m || atomic.CompareAndSwapInt32(&maxObservedActive, m, n) {
					break
				}
			}
			// Give another goroutine a chance to (incorrectly) enter the
			// critical section concurrently, if the lock were not working.
			time.Sleep(time.Millisecond)
			atomic.AddInt32(&active, -1)
		}()
	}
	wg.Wait()

	if maxObservedActive != 1 {
		t.Fatalf("expected at most 1 goroutine in the critical section at a time for the same key, observed %d", maxObservedActive)
	}
}

// TestLockRouteDoesNotSerializeDifferentKeys verifies lockRoute only
// serializes callers sharing the same key -- different routes must still be
// able to make progress concurrently.
func TestLockRouteDoesNotSerializeDifferentKeys(t *testing.T) {
	rsm := &RouteSecretManager{}

	release := make(chan struct{})
	holding := make(chan struct{})

	go func() {
		unlock := rsm.lockRoute(routeKey("sandbox", "route-a"))
		defer unlock()
		close(holding)
		<-release
	}()

	<-holding

	done := make(chan struct{})
	go func() {
		unlock := rsm.lockRoute(routeKey("sandbox", "route-b"))
		unlock()
		close(done)
	}()

	select {
	case <-done:
		// Different key acquired the lock without waiting for route-a's
		// holder to release -- correct.
	case <-time.After(2 * time.Second):
		t.Fatal("lockRoute for a different key blocked on an unrelated key's lock")
	}

	close(release)
}
