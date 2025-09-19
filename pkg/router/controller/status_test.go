package controller

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	corev1 "k8s.io/api/core/v1"
	kapi "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/watch"
	clientgotesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/cache"

	"github.com/openshift/api/route"
	routev1 "github.com/openshift/api/route/v1"
	"github.com/openshift/client-go/route/clientset/versioned/fake"
	routelisters "github.com/openshift/client-go/route/listers/route/v1"
	"github.com/openshift/router/pkg/router/writerlease"
)

type noopLease struct{}

func (_ noopLease) Wait() bool {
	panic("not implemented")
}

func (_ noopLease) WaitUntil(t time.Duration) (leader bool, ok bool) {
	panic("not implemented")
}

func (_ noopLease) Try(key writerlease.WorkKey, fn writerlease.WorkFunc) {
	fn()
}

func (_ noopLease) Extend(key writerlease.WorkKey) {
}

func (_ noopLease) Remove(key writerlease.WorkKey) {
	panic("not implemented")
}

type fakePlugin struct {
	t     watch.EventType
	route *routev1.Route
	err   error
}

func (p *fakePlugin) HandleRoute(t watch.EventType, route *routev1.Route) error {
	p.t, p.route = t, route
	return p.err
}

func (p *fakePlugin) HandleNode(t watch.EventType, node *kapi.Node) error {
	return fmt.Errorf("not expected")
}

func (p *fakePlugin) HandleEndpoints(watch.EventType, *kapi.Endpoints) error {
	return fmt.Errorf("not expected")
}
func (p *fakePlugin) HandleNamespaces(namespaces sets.String) error {
	return fmt.Errorf("not expected")
}
func (p *fakePlugin) Commit() error {
	return fmt.Errorf("not expected")
}

type routeLister struct {
	items []*routev1.Route
	err   error
}

func (l *routeLister) List(selector labels.Selector) (ret []*routev1.Route, err error) {
	return l.items, l.err
}

func (l *routeLister) Routes(namespace string) routelisters.RouteNamespaceLister {
	return routeNamespaceLister{namespace: namespace, l: l}
}

type routeNamespaceLister struct {
	l         *routeLister
	namespace string
}

func (l routeNamespaceLister) List(selector labels.Selector) (ret []*routev1.Route, err error) {
	var items []*routev1.Route
	for _, item := range l.l.items {
		if item.Namespace == l.namespace {
			items = append(items, item)
		}
	}
	return items, l.l.err
}

func (l routeNamespaceLister) Get(name string) (*routev1.Route, error) {
	for _, item := range l.l.items {
		if item.Namespace == l.namespace && item.Name == name {
			return item, nil
		}
	}
	return nil, errors.NewNotFound(route.Resource("route"), name)
}

type recorded struct {
	at      time.Time
	ingress *routev1.RouteIngress
}

type fakeTracker struct {
	contended map[contentionKey]recorded
	cleared   map[contentionKey]recorded
	results   map[contentionKey]bool
}

func (t *fakeTracker) IsChangeContended(id contentionKey, now time.Time, ingress *routev1.RouteIngress) bool {
	if t.contended == nil {
		t.contended = make(map[contentionKey]recorded)
	}
	t.contended[id] = recorded{
		at:      now,
		ingress: ingress,
	}
	return t.results[id]
}

func (t *fakeTracker) Clear(id contentionKey, ingress *routev1.RouteIngress) {
	if t.cleared == nil {
		t.cleared = make(map[contentionKey]recorded)
	}
	lastTouchedTime := ingressConditionTouched(ingress)
	if lastTouchedTime == nil {
		now := nowFn()
		lastTouchedTime = &now
	}
	t.cleared[id] = recorded{
		ingress: ingress,
		at:      lastTouchedTime.Time,
	}
}

func TestStatusNoOp(t *testing.T) {
	now := nowFn()
	touched := metav1.Time{Time: now.Add(-time.Minute)}
	p := &fakePlugin{}
	c := fake.NewSimpleClientset()
	tracker := &fakeTracker{}
	route := &routev1.Route{
		ObjectMeta: metav1.ObjectMeta{Name: "route1", Namespace: "default", UID: types.UID("uid1")},
		Spec:       routev1.RouteSpec{Host: "route1.test.local"},
		Status: routev1.RouteStatus{
			Ingress: []routev1.RouteIngress{
				{
					Host:                    "route1.test.local",
					RouterName:              "test",
					RouterCanonicalHostname: "a.b.c.d",
					Conditions: []routev1.RouteIngressCondition{
						{
							Type:               routev1.RouteAdmitted,
							Status:             corev1.ConditionTrue,
							LastTransitionTime: &touched,
						},
					},
				},
			},
		},
	}
	lister := &routeLister{items: []*routev1.Route{route}}
	admitter := NewStatusAdmitter(p, c.RouteV1(), lister, "test", "a.b.c.d", noopLease{}, tracker)
	err := admitter.HandleRoute(watch.Added, route)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(c.Actions()) > 0 {
		t.Fatalf("unexpected actions: %#v", c.Actions())
	}
}

func checkResult(t *testing.T, err error, c *fake.Clientset, admitter *StatusAdmitter, targetHost string, targetObjTime metav1.Time, targetCachedTime *time.Time, ingressInd int, actionInd int) *routev1.Route {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(c.Actions()) != actionInd+1 {
		t.Fatalf("unexpected actions: %#v", c.Actions())
	}
	action := c.Actions()[actionInd]
	if action.GetVerb() != "update" || action.GetResource().Resource != "routes" || action.GetSubresource() != "status" {
		t.Fatalf("unexpected action: %#v", action)
	}
	obj := c.Actions()[actionInd].(clientgotesting.UpdateAction).GetObject().(*routev1.Route)
	if len(obj.Status.Ingress) != ingressInd+1 || obj.Status.Ingress[ingressInd].Host != targetHost {
		t.Fatalf("expected route reset: expected %q / actual %q -- %#v", targetHost, obj.Status.Ingress[ingressInd].Host, obj)
	}
	condition := obj.Status.Ingress[ingressInd].Conditions[0]
	if condition.LastTransitionTime == nil || *condition.LastTransitionTime != targetObjTime || condition.Status != corev1.ConditionTrue || condition.Reason != "" {
		t.Fatalf("%s: unexpected condition: %#v %s/%s", targetHost, condition, condition.LastTransitionTime, targetObjTime)
	}
	if targetCachedTime != nil {
		switch tracker := admitter.tracker.(type) {
		case *SimpleContentionTracker:
			if tracker.ids["uid1"].at != *targetCachedTime {
				t.Fatalf("unexpected status time")
			}
		}
	}

	return obj
}

func TestStatusResetsHost(t *testing.T) {
	now := metav1.Now()
	nowFn = func() metav1.Time { return now }
	touched := metav1.Time{Time: now.Add(-time.Minute)}
	p := &fakePlugin{}
	c := fake.NewSimpleClientset(&routev1.Route{ObjectMeta: metav1.ObjectMeta{Name: "route1", Namespace: "default", UID: types.UID("uid1")}})
	tracker := &fakeTracker{}
	route := &routev1.Route{
		ObjectMeta: metav1.ObjectMeta{Name: "route1", Namespace: "default", UID: types.UID("uid1")},
		Spec:       routev1.RouteSpec{Host: "route1.test.local"},
		Status: routev1.RouteStatus{
			Ingress: []routev1.RouteIngress{
				{
					Host:       "route2.test.local",
					RouterName: "test",
					Conditions: []routev1.RouteIngressCondition{
						{
							Type:               routev1.RouteAdmitted,
							Status:             corev1.ConditionTrue,
							LastTransitionTime: &touched,
						},
					},
				},
			},
		},
	}
	lister := &routeLister{items: []*routev1.Route{route}}
	admitter := NewStatusAdmitter(p, c.RouteV1(), lister, "test", "", noopLease{}, tracker)
	err := admitter.HandleRoute(watch.Added, route)

	route = checkResult(t, err, c, admitter, "route1.test.local", now, &now.Time, 0, 0)
	ingress := findIngressForRoute(route, "test")
	if ingress == nil {
		t.Fatalf("no ingress found: %#v", route)
	}
	if ingress.Host != "route1.test.local" {
		t.Fatalf("incorrect ingress: %#v", ingress)
	}
}

func findIngressForRoute(route *routev1.Route, routerName string) *routev1.RouteIngress {
	for i := range route.Status.Ingress {
		if route.Status.Ingress[i].RouterName == routerName {
			return &route.Status.Ingress[i]
		}
	}
	return nil
}

func TestStatusAdmitsRouteOnForbidden(t *testing.T) {
	now := nowFn()
	nowFn = func() metav1.Time { return now }
	touched := metav1.Time{Time: now.Add(-time.Minute)}
	p := &fakePlugin{}
	c := fake.NewSimpleClientset(&routev1.Route{ObjectMeta: metav1.ObjectMeta{Name: "route1", Namespace: "default", UID: types.UID("uid1")}})
	c.PrependReactor("update", "routes", func(action clientgotesting.Action) (handled bool, ret runtime.Object, err error) {
		if action.GetSubresource() != "status" {
			return false, nil, nil
		}
		return true, nil, errors.NewForbidden(corev1.Resource("Route"), "route1", nil)
	})
	tracker := &fakeTracker{}
	route := &routev1.Route{
		ObjectMeta: metav1.ObjectMeta{Name: "route1", Namespace: "default", UID: types.UID("uid1")},
		Spec:       routev1.RouteSpec{Host: "route1.test.local"},
		Status: routev1.RouteStatus{
			Ingress: []routev1.RouteIngress{
				{
					Host:       "route2.test.local",
					RouterName: "test",
					Conditions: []routev1.RouteIngressCondition{
						{
							Type:               routev1.RouteAdmitted,
							Status:             corev1.ConditionTrue,
							LastTransitionTime: &touched,
						},
					},
				},
			},
		},
	}
	lister := &routeLister{items: []*routev1.Route{route}}
	admitter := NewStatusAdmitter(p, c.RouteV1(), lister, "test", "", noopLease{}, tracker)
	err := admitter.HandleRoute(watch.Added, route)
	route = checkResult(t, err, c, admitter, "route1.test.local", now, &touched.Time, 0, 0)
	ingress := findIngressForRoute(route, "test")
	if ingress == nil {
		t.Fatalf("no ingress found: %#v", route)
	}
	if ingress.Host != "route1.test.local" {
		t.Fatalf("incorrect ingress: %#v", ingress)
	}
}

func TestStatusBackoffOnConflict(t *testing.T) {
	now := nowFn()
	nowFn = func() metav1.Time { return now }
	touched := metav1.Time{Time: now.Add(-time.Minute)}
	p := &fakePlugin{}
	c := fake.NewSimpleClientset(&routev1.Route{ObjectMeta: metav1.ObjectMeta{Name: "route1", Namespace: "default", UID: types.UID("uid1")}})
	c.PrependReactor("update", "routes", func(action clientgotesting.Action) (handled bool, ret runtime.Object, err error) {
		if action.GetSubresource() != "status" {
			return false, nil, nil
		}
		return true, nil, errors.NewConflict(corev1.Resource("Route"), "route1", nil)
	})
	tracker := &fakeTracker{}
	route := &routev1.Route{
		ObjectMeta: metav1.ObjectMeta{Name: "route1", Namespace: "default", UID: types.UID("uid1")},
		Spec:       routev1.RouteSpec{Host: "route1.test.local"},
		Status: routev1.RouteStatus{
			Ingress: []routev1.RouteIngress{
				{
					Host:       "route2.test.local",
					RouterName: "test",
					Conditions: []routev1.RouteIngressCondition{
						{
							Type:               routev1.RouteAdmitted,
							Status:             corev1.ConditionFalse,
							LastTransitionTime: &touched,
						},
					},
				},
			},
		},
	}
	lister := &routeLister{items: []*routev1.Route{route}}
	admitter := NewStatusAdmitter(p, c.RouteV1(), lister, "test", "", noopLease{}, tracker)
	err := admitter.HandleRoute(watch.Added, route)
	checkResult(t, err, c, admitter, "route1.test.local", now, nil, 0, 0)
}

func TestStatusRecordRejection(t *testing.T) {
	now := nowFn()
	nowFn = func() metav1.Time { return now }
	p := &fakePlugin{}
	c := fake.NewSimpleClientset(&routev1.Route{ObjectMeta: metav1.ObjectMeta{Name: "route1", Namespace: "default", UID: types.UID("uid1")}})
	tracker := &fakeTracker{}
	route := &routev1.Route{
		ObjectMeta: metav1.ObjectMeta{Name: "route1", Namespace: "default", UID: types.UID("uid1")},
		Spec:       routev1.RouteSpec{Host: "route1.test.local"},
	}
	lister := &routeLister{items: []*routev1.Route{route}}
	admitter := NewStatusAdmitter(p, c.RouteV1(), lister, "test", "", noopLease{}, tracker)
	admitter.RecordRouteRejection(route, "Failed", "generic error")

	if len(c.Actions()) != 1 {
		t.Fatalf("unexpected actions: %#v", c.Actions())
	}
	action := c.Actions()[0]
	if action.GetVerb() != "update" || action.GetResource().Resource != "routes" || action.GetSubresource() != "status" {
		t.Fatalf("unexpected action: %#v", action)
	}
	obj := c.Actions()[0].(clientgotesting.UpdateAction).GetObject().(*routev1.Route)
	if len(obj.Status.Ingress) != 1 || obj.Status.Ingress[0].Host != "route1.test.local" {
		t.Fatalf("expected route reset: %#v", obj)
	}
	condition := obj.Status.Ingress[0].Conditions[0]
	if condition.LastTransitionTime == nil || *condition.LastTransitionTime != now || condition.Status != corev1.ConditionFalse || condition.Reason != "Failed" || condition.Message != "generic error" {
		t.Fatalf("unexpected condition: %#v", condition)
	}
}

func TestStatusRecordRejectionNoChange(t *testing.T) {
	now := nowFn()
	nowFn = func() metav1.Time { return now }
	touched := metav1.Time{Time: now.Add(-time.Minute)}
	p := &fakePlugin{}
	c := fake.NewSimpleClientset(&routev1.Route{ObjectMeta: metav1.ObjectMeta{Name: "route1", Namespace: "default", UID: types.UID("uid1")}})
	tracker := &fakeTracker{}
	route := &routev1.Route{
		ObjectMeta: metav1.ObjectMeta{Name: "route1", Namespace: "default", UID: types.UID("uid1")},
		Spec:       routev1.RouteSpec{Host: "route1.test.local"},
		Status: routev1.RouteStatus{
			Ingress: []routev1.RouteIngress{
				{
					Host:       "route1.test.local",
					RouterName: "test",
					Conditions: []routev1.RouteIngressCondition{
						{
							Type:               routev1.RouteAdmitted,
							Status:             corev1.ConditionFalse,
							Reason:             "Failed",
							Message:            "generic error",
							LastTransitionTime: &touched,
						},
					},
				},
			},
		},
	}
	lister := &routeLister{items: []*routev1.Route{route}}
	admitter := NewStatusAdmitter(p, c.RouteV1(), lister, "test", "", noopLease{}, tracker)
	admitter.RecordRouteRejection(route, "Failed", "generic error")

	if len(c.Actions()) != 0 {
		t.Fatalf("unexpected actions: %#v", c.Actions())
	}
}

func TestStatusRecordRejectionWithStatus(t *testing.T) {
	now := nowFn()
	nowFn = func() metav1.Time { return now }
	touched := metav1.Time{Time: now.Add(-time.Minute)}
	p := &fakePlugin{}
	c := fake.NewSimpleClientset(&routev1.Route{ObjectMeta: metav1.ObjectMeta{Name: "route1", Namespace: "default", UID: types.UID("uid1")}})
	tracker := &fakeTracker{}
	route := &routev1.Route{
		ObjectMeta: metav1.ObjectMeta{Name: "route1", Namespace: "default", UID: types.UID("uid1")},
		Spec:       routev1.RouteSpec{Host: "route1.test.local"},
		Status: routev1.RouteStatus{
			Ingress: []routev1.RouteIngress{
				{
					Host:       "route2.test.local",
					RouterName: "test",
					Conditions: []routev1.RouteIngressCondition{
						{
							Type:               routev1.RouteAdmitted,
							Status:             corev1.ConditionFalse,
							LastTransitionTime: &touched,
						},
					},
				},
			},
		},
	}
	lister := &routeLister{items: []*routev1.Route{route}}
	admitter := NewStatusAdmitter(p, c.RouteV1(), lister, "test", "", noopLease{}, tracker)
	admitter.RecordRouteRejection(route, "Failed", "generic error")

	if len(c.Actions()) != 1 {
		t.Fatalf("unexpected actions: %#v", c.Actions())
	}
	action := c.Actions()[0]
	if action.GetVerb() != "update" || action.GetResource().Resource != "routes" || action.GetSubresource() != "status" {
		t.Fatalf("unexpected action: %#v", action)
	}
	obj := c.Actions()[0].(clientgotesting.UpdateAction).GetObject().(*routev1.Route)
	if len(obj.Status.Ingress) != 1 || obj.Status.Ingress[0].Host != "route1.test.local" {
		t.Fatalf("expected route reset: %#v", obj)
	}
	condition := obj.Status.Ingress[0].Conditions[0]
	if condition.LastTransitionTime == nil || *condition.LastTransitionTime != now || condition.Status != corev1.ConditionFalse || condition.Reason != "Failed" || condition.Message != "generic error" {
		t.Fatalf("unexpected condition: %#v", condition)
	}
}

func TestStatusRecordRejectionOnHostUpdateOnly(t *testing.T) {
	now := nowFn()
	nowFn = func() metav1.Time { return now }
	touched := metav1.Time{Time: now.Add(-time.Minute)}
	p := &fakePlugin{}
	c := fake.NewSimpleClientset(&routev1.Route{ObjectMeta: metav1.ObjectMeta{Name: "route1", Namespace: "default", UID: types.UID("uid1")}})
	tracker := &fakeTracker{}
	route := &routev1.Route{
		ObjectMeta: metav1.ObjectMeta{Name: "route1", Namespace: "default", UID: types.UID("uid1")},
		Spec:       routev1.RouteSpec{Host: "route1.test.local"},
		Status: routev1.RouteStatus{
			Ingress: []routev1.RouteIngress{
				{
					Host:       "route2.test.local",
					RouterName: "test",
					Conditions: []routev1.RouteIngressCondition{
						{
							Type:               routev1.RouteAdmitted,
							Status:             corev1.ConditionFalse,
							LastTransitionTime: &touched,
							Reason:             "Failed",
							Message:            "generic error",
						},
					},
				},
			},
		},
	}
	lister := &routeLister{items: []*routev1.Route{route}}
	admitter := NewStatusAdmitter(p, c.RouteV1(), lister, "test", "", noopLease{}, tracker)
	admitter.RecordRouteRejection(route, "Failed", "generic error")

	if len(c.Actions()) != 1 {
		t.Fatalf("unexpected actions: %#v", c.Actions())
	}
	action := c.Actions()[0]
	if action.GetVerb() != "update" || action.GetResource().Resource != "routes" || action.GetSubresource() != "status" {
		t.Fatalf("unexpected action: %#v", action)
	}
	obj := c.Actions()[0].(clientgotesting.UpdateAction).GetObject().(*routev1.Route)
	if len(obj.Status.Ingress) != 1 || obj.Status.Ingress[0].Host != "route1.test.local" {
		t.Fatalf("expected route reset: %#v", obj)
	}
	condition := obj.Status.Ingress[0].Conditions[0]
	if condition.LastTransitionTime == nil || *condition.LastTransitionTime != now || condition.Status != corev1.ConditionFalse || condition.Reason != "Failed" || condition.Message != "generic error" {
		t.Fatalf("unexpected condition: %#v", condition)
	}
	if tracker.contended["uid1"].at != now.Time || tracker.cleared["uid1"].at.IsZero() {
		t.Fatal(tracker)
	}
}

func TestStatusRecordRejectionConflict(t *testing.T) {
	now := nowFn()
	nowFn = func() metav1.Time { return now }
	touched := metav1.Time{Time: now.Add(-time.Minute)}
	p := &fakePlugin{}
	c := fake.NewSimpleClientset(&routev1.Route{ObjectMeta: metav1.ObjectMeta{Name: "route1", Namespace: "default", UID: types.UID("uid1")}})
	c.PrependReactor("update", "routes", func(action clientgotesting.Action) (handled bool, ret runtime.Object, err error) {
		if action.GetSubresource() != "status" {
			return false, nil, nil
		}
		return true, nil, errors.NewConflict(corev1.Resource("Route"), "route1", nil)
	})
	tracker := &fakeTracker{}
	route := &routev1.Route{
		ObjectMeta: metav1.ObjectMeta{Name: "route1", Namespace: "default", UID: types.UID("uid1")},
		Spec:       routev1.RouteSpec{Host: "route1.test.local"},
		Status: routev1.RouteStatus{
			Ingress: []routev1.RouteIngress{
				{
					Host:       "route2.test.local",
					RouterName: "test",
					Conditions: []routev1.RouteIngressCondition{
						{
							Type:               routev1.RouteAdmitted,
							Status:             corev1.ConditionFalse,
							LastTransitionTime: &touched,
						},
					},
				},
			},
		},
	}
	lister := &routeLister{items: []*routev1.Route{route}}
	admitter := NewStatusAdmitter(p, c.RouteV1(), lister, "test", "", noopLease{}, tracker)
	admitter.RecordRouteRejection(route, "Failed", "generic error")

	if len(c.Actions()) != 1 {
		t.Fatalf("unexpected actions: %#v", c.Actions())
	}
	action := c.Actions()[0]
	if action.GetVerb() != "update" || action.GetResource().Resource != "routes" || action.GetSubresource() != "status" {
		t.Fatalf("unexpected action: %#v", action)
	}
	obj := c.Actions()[0].(clientgotesting.UpdateAction).GetObject().(*routev1.Route)
	if len(obj.Status.Ingress) != 1 || obj.Status.Ingress[0].Host != "route1.test.local" {
		t.Fatalf("expected route reset: %#v", obj)
	}
	condition := obj.Status.Ingress[0].Conditions[0]
	if condition.LastTransitionTime == nil || *condition.LastTransitionTime != now || condition.Status != corev1.ConditionFalse || condition.Reason != "Failed" || condition.Message != "generic error" {
		t.Fatalf("unexpected condition: %#v", condition)
	}
}

func TestStatusFightBetweenReplicas(t *testing.T) {
	p := &fakePlugin{}
	stopCh := make(chan struct{})
	defer close(stopCh)

	// the initial pre-population
	now1 := metav1.Now()
	nowFn = func() metav1.Time { return now1 }
	c1 := fake.NewSimpleClientset(&routev1.Route{ObjectMeta: metav1.ObjectMeta{Name: "route1", Namespace: "default", UID: types.UID("uid1")}})
	tracker1 := &fakeTracker{}
	route1 := &routev1.Route{
		ObjectMeta: metav1.ObjectMeta{Name: "route1", Namespace: "default", UID: types.UID("uid1")},
		Spec:       routev1.RouteSpec{Host: "route1.test.local"},
		Status:     routev1.RouteStatus{},
	}
	lister1 := &routeLister{items: []*routev1.Route{route1}}
	admitter1 := NewStatusAdmitter(p, c1.RouteV1(), lister1, "test", "", noopLease{}, tracker1)
	err := admitter1.HandleRoute(watch.Added, route1)

	outObj1 := checkResult(t, err, c1, admitter1, "route1.test.local", now1, &now1.Time, 0, 0)
	if tracker1.cleared["uid1"].at != now1.Time {
		t.Fatal(tracker1)
	}
	outObj1 = outObj1.DeepCopy()

	// the new deployment's replica
	now2 := metav1.Time{Time: now1.Time.Add(2 * time.Minute)}
	nowFn = func() metav1.Time { return now2 }
	c2 := fake.NewSimpleClientset(&routev1.Route{ObjectMeta: metav1.ObjectMeta{Name: "route1", Namespace: "default", UID: types.UID("uid1")}})
	tracker2 := &fakeTracker{}
	lister2 := &routeLister{items: []*routev1.Route{outObj1}}
	admitter2 := NewStatusAdmitter(p, c2.RouteV1(), lister2, "test", "", noopLease{}, tracker2)
	outObj1.Spec.Host = "route1.test-new.local"
	err = admitter2.HandleRoute(watch.Added, outObj1)

	outObj2 := checkResult(t, err, c2, admitter2, "route1.test-new.local", now2, &now2.Time, 0, 0)
	if tracker2.cleared["uid1"].at != now2.Time {
		t.Fatal(tracker2)
	}
	outObj2 = outObj2.DeepCopy()

	lister1.items[0] = outObj2

	tracker1.results = map[contentionKey]bool{"uid1": true}
	now3 := metav1.Time{Time: now1.Time.Add(time.Minute)}
	nowFn = func() metav1.Time { return now3 }
	outObj2.Spec.Host = "route1.test.local"
	err = admitter1.HandleRoute(watch.Modified, outObj2)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// expect the last HandleRoute not to have performed any actions
	if len(c1.Actions()) != 1 {
		t.Fatalf("unexpected actions: %#v", c1.Actions())
	}
}

func TestStatusFightBetweenRouters(t *testing.T) {
	p := &fakePlugin{}

	// initial try, results in conflict
	now1 := metav1.Now()
	nowFn = func() metav1.Time { return now1 }
	touched1 := metav1.Time{Time: now1.Add(-time.Minute)}
	c1 := fake.NewSimpleClientset(&routev1.Route{ObjectMeta: metav1.ObjectMeta{Name: "route1", Namespace: "default", UID: types.UID("uid1")}})
	returnConflict := true
	c1.PrependReactor("update", "routes", func(action clientgotesting.Action) (handled bool, ret runtime.Object, err error) {
		if action.GetSubresource() != "status" {
			return false, nil, nil
		}
		if returnConflict {
			returnConflict = false
			return true, nil, errors.NewConflict(corev1.Resource("Route"), "route1", nil)
		}
		return false, nil, nil
	})
	tracker := &fakeTracker{}
	route1 := &routev1.Route{
		ObjectMeta: metav1.ObjectMeta{Name: "route1", Namespace: "default", UID: types.UID("uid1")},
		Spec:       routev1.RouteSpec{Host: "route2.test-new.local"},
		Status: routev1.RouteStatus{
			Ingress: []routev1.RouteIngress{
				{
					Host:       "route1.test.local",
					RouterName: "test1",
					Conditions: []routev1.RouteIngressCondition{
						{
							Type:               routev1.RouteAdmitted,
							Status:             corev1.ConditionFalse,
							LastTransitionTime: &touched1,
						},
					},
				},
				{
					Host:       "route1.test-new.local",
					RouterName: "test2",
					Conditions: []routev1.RouteIngressCondition{
						{
							Type:               routev1.RouteAdmitted,
							Status:             corev1.ConditionFalse,
							LastTransitionTime: &touched1,
						},
					},
				},
			},
		},
	}
	lister1 := &routeLister{items: []*routev1.Route{route1}}
	admitter1 := NewStatusAdmitter(p, c1.RouteV1(), lister1, "test2", "", noopLease{}, tracker)
	err := admitter1.HandleRoute(watch.Added, route1)

	checkResult(t, err, c1, admitter1, "route2.test-new.local", now1, nil, 1, 0)
	if tracker.contended["uid1"].at != now1.Time || !tracker.cleared["uid1"].at.IsZero() {
		t.Fatalf("should have recorded uid1 into tracker: %#v", tracker)
	}

	// second try, should not send status because the tracker reports a conflict
	now2 := metav1.Now()
	nowFn = func() metav1.Time { return now2 }
	touched2 := metav1.Time{Time: now2.Add(-time.Minute)}
	tracker.cleared = nil
	tracker.results = map[contentionKey]bool{"uid1": true}
	route2 := &routev1.Route{
		ObjectMeta: metav1.ObjectMeta{Name: "route1", Namespace: "default", UID: types.UID("uid1")},
		Spec:       routev1.RouteSpec{Host: "route2.test-new.local"},
		Status: routev1.RouteStatus{
			Ingress: []routev1.RouteIngress{
				{
					Host:       "route2.test.local",
					RouterName: "test1",
					Conditions: []routev1.RouteIngressCondition{
						{
							Type:               routev1.RouteAdmitted,
							Status:             corev1.ConditionFalse,
							LastTransitionTime: &touched1,
						},
					},
				},
				{
					Host:       "route1.test-new.local",
					RouterName: "test2",
					Conditions: []routev1.RouteIngressCondition{
						{
							Type:               routev1.RouteAdmitted,
							Status:             corev1.ConditionFalse,
							LastTransitionTime: &touched2,
						},
					},
				},
			},
		},
	}
	lister1.items[0] = route2
	err = admitter1.HandleRoute(watch.Modified, route2)

	checkResult(t, err, c1, admitter1, "route2.test-new.local", now1, &now2.Time, 1, 0)
	if tracker.contended["uid1"].at != now2.Time {
		t.Fatalf("should have recorded uid1 into tracker: %#v", tracker)
	}
}

func makePass(t *testing.T, host string, admitter *StatusAdmitter, srcObj *routev1.Route, expectUpdate bool, conflict bool) *routev1.Route {
	t.Helper()
	// initialize a new client
	c := fake.NewSimpleClientset(srcObj)
	if conflict {
		c.PrependReactor("update", "routes", func(action clientgotesting.Action) (handled bool, ret runtime.Object, err error) {
			if action.GetSubresource() != "status" {
				return false, nil, nil
			}
			return true, nil, errors.NewConflict(corev1.Resource("Route"), "route1", nil)
		})
	}

	admitter.client = c.RouteV1()

	inputObj := srcObj.DeepCopy()
	inputObj.Spec.Host = host

	admitter.lister.(*routeLister).items = []*routev1.Route{inputObj}

	err := admitter.HandleRoute(watch.Modified, inputObj)

	if expectUpdate {
		now := nowFn()
		return checkResult(t, err, c, admitter, inputObj.Spec.Host, now, nil, 0, 0)
	}

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// expect the last HandleRoute not to have performed any actions
	if len(c.Actions()) != 0 {
		t.Fatalf("expected no actions: %#v", c)
	}

	return nil
}

func TestRouterContention(t *testing.T) {
	p := &fakePlugin{}
	stopCh := make(chan struct{})
	defer close(stopCh)

	now := metav1.Now()
	nowFn = func() metav1.Time { return now }

	initObj := &routev1.Route{
		ObjectMeta: metav1.ObjectMeta{Name: "route1", Namespace: "default", UID: types.UID("uid1")},
		Spec:       routev1.RouteSpec{Host: "route1.new.local"},
		Status:     routev1.RouteStatus{},
	}

	// NB: contention period is 1 minute
	i1 := &fakeInformer{}
	t1 := NewSimpleContentionTracker(i1, "test", time.Minute)
	lister1 := &routeLister{}

	r1 := NewStatusAdmitter(p, nil, lister1, "test", "", noopLease{}, t1)

	// update
	currObj := makePass(t, "route1.test.local", r1, initObj, true, false)
	// no-op
	makePass(t, "route1.test.local", r1, currObj, false, false)

	// another caller changes the status, we should change it back
	findIngressForRoute(currObj, "test").Host = "route1.other.local"
	currObj = makePass(t, "route1.test.local", r1, currObj, true, false)

	// if we observe a single change to our ingress, record it but still update
	otherObj := currObj.DeepCopy()
	ingress := findIngressForRoute(otherObj, "test")
	ingress.Host = "route1.other1.local"
	t1.Changed(contentionKey(otherObj.UID), ingress)
	if t1.IsChangeContended(contentionKey(otherObj.UID), nowFn().Time, ingress) {
		t.Fatal("change shouldn't be contended yet")
	}
	currObj = makePass(t, "route1.test.local", r1, otherObj, true, false)

	// updating the route sets us back to candidate, but if we observe our own write
	// we stay in candidate
	ingress = findIngressForRoute(currObj, "test").DeepCopy()
	t1.Changed(contentionKey(currObj.UID), ingress)
	if t1.IsChangeContended(contentionKey(currObj.UID), nowFn().Time, ingress) {
		t.Fatal("change should not be contended")
	}
	makePass(t, "route1.test.local", r1, currObj, false, false)

	// updating the route sets us back to candidate, and if we detect another change to
	// ingress we will go into conflict, even with our original write
	ingress = ingressChangeWithNewHost(currObj, "test", "route1.other2.local")
	t1.Changed(contentionKey(currObj.UID), ingress)
	if !t1.IsChangeContended(contentionKey(currObj.UID), nowFn().Time, ingress) {
		t.Fatal("change should be contended")
	}
	makePass(t, "route1.test.local", r1, currObj, false, false)

	// another contending write occurs, but the tracker isn't flushed so
	// we stay contended
	ingress = ingressChangeWithNewHost(currObj, "test", "route1.other3.local")
	t1.Changed(contentionKey(currObj.UID), ingress)
	t1.flush()
	if !t1.IsChangeContended(contentionKey(currObj.UID), nowFn().Time, ingress) {
		t.Fatal("change should be contended")
	}
	makePass(t, "route1.test.local", r1, currObj, false, false)

	// after the interval expires, we no longer contend
	now = metav1.Time{Time: now.Add(3 * time.Minute)}
	nowFn = func() metav1.Time { return now }
	t1.flush()
	findIngressForRoute(currObj, "test").Host = "route1.other.local"
	currObj = makePass(t, "route1.test.local", r1, currObj, true, false)

	// multiple changes to host name don't cause contention
	currObj = makePass(t, "route2.test.local", r1, currObj, true, false)
	currObj = makePass(t, "route3.test.local", r1, currObj, true, false)
	t1.Changed(contentionKey(currObj.UID), findIngressForRoute(currObj, "test"))
	currObj = makePass(t, "route4.test.local", r1, currObj, true, false)
	t1.Changed(contentionKey(currObj.UID), findIngressForRoute(currObj, "test"))
	currObj = makePass(t, "route5.test.local", r1, currObj, true, false)
	t1.Changed(contentionKey(currObj.UID), findIngressForRoute(currObj, "test"))
	t1.Changed(contentionKey(currObj.UID), findIngressForRoute(currObj, "test"))
	currObj = makePass(t, "route6.test.local", r1, currObj, true, false)
}

// TestRouterContentionOnCondition tests router contention logic for adding/updating/removing a route condition.
// This test validates the ingressEqual function is working correctly for comparing conditions.
func TestRouterContentionOnCondition(t *testing.T) {
	now := metav1.Now()
	notNow := metav1.Time{Time: now.Add(3 * time.Minute)}

	testCases := []struct {
		name             string
		key              string
		conditions       []routev1.RouteIngressCondition
		updateConditions []routev1.RouteIngressCondition
		expectContend    bool
	}{
		{
			name: "no change to condition does not cause contention",
			conditions: []routev1.RouteIngressCondition{{
				Type:               routev1.RouteUnservableInFutureVersions,
				Status:             kapi.ConditionTrue,
				Reason:             "foo",
				Message:            "foo",
				LastTransitionTime: &now,
			}},
			updateConditions: []routev1.RouteIngressCondition{{
				Type:               routev1.RouteUnservableInFutureVersions,
				Status:             kapi.ConditionTrue,
				Reason:             "foo",
				Message:            "foo",
				LastTransitionTime: &now,
			}},
			expectContend: false,
		},
		{
			name: "adding condition causes contention",
			updateConditions: []routev1.RouteIngressCondition{{
				Type:               routev1.RouteUnservableInFutureVersions,
				Status:             kapi.ConditionTrue,
				Reason:             "foo",
				Message:            "foo",
				LastTransitionTime: &now,
			}},
			expectContend: true,
		},
		{
			name: "removing condition causes contention",
			conditions: []routev1.RouteIngressCondition{{
				Type:               routev1.RouteUnservableInFutureVersions,
				Status:             kapi.ConditionTrue,
				Reason:             "foo",
				Message:            "foo",
				LastTransitionTime: &now,
			}},
			expectContend: true,
		},
		{
			name: "changing condition type causes contention",
			conditions: []routev1.RouteIngressCondition{{
				Type:               routev1.RouteUnservableInFutureVersions,
				Status:             kapi.ConditionTrue,
				Reason:             "foo",
				Message:            "foo",
				LastTransitionTime: &now,
			}},
			updateConditions: []routev1.RouteIngressCondition{{
				Type:               "NewConditionType",
				Status:             kapi.ConditionTrue,
				Reason:             "foo",
				Message:            "foo",
				LastTransitionTime: &now,
			}},
			expectContend: true,
		},
		{
			name: "changing condition status causes contention",
			conditions: []routev1.RouteIngressCondition{{
				Type:               routev1.RouteUnservableInFutureVersions,
				Status:             kapi.ConditionTrue,
				Reason:             "foo",
				Message:            "foo",
				LastTransitionTime: &now,
			}},
			updateConditions: []routev1.RouteIngressCondition{{
				Type:               routev1.RouteUnservableInFutureVersions,
				Status:             kapi.ConditionFalse,
				Reason:             "foo",
				Message:            "foo",
				LastTransitionTime: &now,
			}},
			expectContend: true,
		},
		{
			name: "changing condition status with empty reason and message causes contention",
			conditions: []routev1.RouteIngressCondition{{
				Type:               routev1.RouteAdmitted,
				Status:             kapi.ConditionTrue,
				LastTransitionTime: &notNow,
			}},
			updateConditions: []routev1.RouteIngressCondition{{
				Type:               routev1.RouteAdmitted,
				Status:             kapi.ConditionFalse,
				Reason:             "foo",
				Message:            "foo",
				LastTransitionTime: &now,
			}},
			expectContend: true,
		},
		{
			name: "changing condition reason causes contention",
			conditions: []routev1.RouteIngressCondition{{
				Type:               routev1.RouteUnservableInFutureVersions,
				Status:             kapi.ConditionTrue,
				Reason:             "foo",
				Message:            "foo",
				LastTransitionTime: &now,
			}},
			updateConditions: []routev1.RouteIngressCondition{{
				Type:               routev1.RouteUnservableInFutureVersions,
				Status:             kapi.ConditionTrue,
				Reason:             "bar",
				Message:            "foo",
				LastTransitionTime: &now,
			}},
			expectContend: true,
		},
		{
			name: "changing condition message causes contention",
			conditions: []routev1.RouteIngressCondition{{
				Type:               routev1.RouteUnservableInFutureVersions,
				Status:             kapi.ConditionTrue,
				Reason:             "foo",
				Message:            "foo",
				LastTransitionTime: &now,
			}},
			updateConditions: []routev1.RouteIngressCondition{{
				Type:               routev1.RouteUnservableInFutureVersions,
				Status:             kapi.ConditionTrue,
				Reason:             "foo",
				Message:            "bar",
				LastTransitionTime: &now,
			}},
			expectContend: true,
		},
		{
			name: "changing condition transition time does not cause contention",
			conditions: []routev1.RouteIngressCondition{{
				Type:               routev1.RouteUnservableInFutureVersions,
				Status:             kapi.ConditionTrue,
				Reason:             "foo",
				Message:            "foo",
				LastTransitionTime: &now,
			}},
			updateConditions: []routev1.RouteIngressCondition{{
				Type:               routev1.RouteUnservableInFutureVersions,
				Status:             kapi.ConditionTrue,
				Reason:             "foo",
				Message:            "foo",
				LastTransitionTime: &notNow,
			}},
			expectContend: false,
		},
		{
			name: "same conditions, but different order does not cause contention",
			conditions: []routev1.RouteIngressCondition{
				{
					Type:               routev1.RouteUnservableInFutureVersions,
					Status:             kapi.ConditionTrue,
					Reason:             "foo",
					Message:            "foo",
					LastTransitionTime: &now,
				},
				{
					Type:               "ConditionTest",
					Status:             kapi.ConditionTrue,
					Reason:             "bar",
					Message:            "bar",
					LastTransitionTime: &now,
				},
			},
			updateConditions: []routev1.RouteIngressCondition{
				{
					Type:               "ConditionTest",
					Status:             kapi.ConditionTrue,
					Reason:             "bar",
					Message:            "bar",
					LastTransitionTime: &now,
				},
				{
					Type:               routev1.RouteUnservableInFutureVersions,
					Status:             kapi.ConditionTrue,
					Reason:             "foo",
					Message:            "foo",
					LastTransitionTime: &now,
				},
			},
			expectContend: false,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			stopCh := make(chan struct{})
			defer close(stopCh)

			// NB: contention period is 1 minute
			i1 := &fakeInformer{}
			t1 := NewSimpleContentionTracker(i1, "test", time.Minute)

			// Provide the initial condition.
			routeIngress := routev1.RouteIngress{}
			routeIngress.Conditions = tc.conditions

			// The key value doesn't really matter, just as long as it doesn't change.
			key := contentionKey("uid1")

			// This is the initial "change". This should never be contended.
			t1.Changed(key, routeIngress.DeepCopy())
			if t1.IsChangeContended(key, nowFn().Time, nil) {
				t.Fatal("expected initial change NOT to be contended")
			}

			// Now update the condition.
			routeIngress.Conditions = tc.updateConditions

			// Do another change with the new RouteIngress.
			t1.Changed(key, routeIngress.DeepCopy())
			if tc.expectContend && !t1.IsChangeContended(key, nowFn().Time, nil) {
				t.Fatal("expected change to be contended")
			} else if !tc.expectContend && t1.IsChangeContended(key, nowFn().Time, nil) {
				t.Fatal("expected change NOT to be contended")
			}

		})
	}
}

// Benchmark_ingressConditionsEqual benchmarks the ingressConditionEqual function. Efficiency is crucial for
// this function as it directly impacts the performance of the contention tracker, potentially delaying the
// detection of contentions.
func Benchmark_ingressConditionsEqual(b *testing.B) {
	now := metav1.Now()
	notNow := metav1.Time{Time: now.Add(3 * time.Minute)}
	admittedCondition := routev1.RouteIngressCondition{
		Type:               routev1.RouteAdmitted,
		Status:             kapi.ConditionTrue,
		Reason:             "foo",
		Message:            "foo",
		LastTransitionTime: &now,
	}
	unservableCondition := routev1.RouteIngressCondition{
		Type:               routev1.RouteUnservableInFutureVersions,
		Status:             kapi.ConditionFalse,
		Reason:             "bar",
		Message:            "bar",
		LastTransitionTime: &notNow,
	}
	testCases := []struct {
		name  string
		condA []routev1.RouteIngressCondition
		condB []routev1.RouteIngressCondition
	}{
		{
			name: "single",
			condA: []routev1.RouteIngressCondition{
				admittedCondition,
			},
			condB: []routev1.RouteIngressCondition{
				unservableCondition,
			},
		},
		{
			name: "mismatch_length",
			condA: []routev1.RouteIngressCondition{
				admittedCondition,
			},
			condB: []routev1.RouteIngressCondition{
				unservableCondition,
				admittedCondition,
			},
		},
		{
			name: "double",
			condA: []routev1.RouteIngressCondition{
				admittedCondition,
				unservableCondition,
			},
			condB: []routev1.RouteIngressCondition{
				unservableCondition,
				admittedCondition,
			},
		},
	}
	for _, tc := range testCases {
		b.ResetTimer()
		b.Run(tc.name, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				ingressConditionsEqual(tc.condA, tc.condB)
			}
		})
	}
}

// TestStatusUnservableInFutureVersions tests the StatusAdmitter's functionality for handling routes that are unservable
// in future version of OpenShift.
func TestStatusUnservableInFutureVersions(t *testing.T) {
	unservableInFutureVersionsTrueCondition := routev1.RouteIngressCondition{
		Type:    routev1.RouteUnservableInFutureVersions,
		Status:  corev1.ConditionTrue,
		Reason:  "UpgradeRouteValidationFailed",
		Message: "next version of OpenShift does not support SHA1",
	}
	testCases := []struct {
		name                       string
		routerName                 string
		unservableInFutureVersions bool
		route                      *routev1.Route
		expectedRoute              *routev1.Route
	}{
		{
			name:       "not unservableInFutureVersions should have no condition",
			routerName: "test",
			route: &routev1.Route{
				ObjectMeta: metav1.ObjectMeta{Name: "route1", Namespace: "default", UID: types.UID("uid1")},
				Spec:       routev1.RouteSpec{Host: "route1.test.local"},
			},
			expectedRoute: nil,
		},
		{
			name:                       "add unservableInFutureVersions condition",
			routerName:                 "test",
			unservableInFutureVersions: true,
			route: &routev1.Route{
				ObjectMeta: metav1.ObjectMeta{Name: "route1", Namespace: "default", UID: types.UID("uid1")},
				Spec:       routev1.RouteSpec{Host: "route1.test.local"},
			},
			expectedRoute: &routev1.Route{
				ObjectMeta: metav1.ObjectMeta{Name: "route1", Namespace: "default", UID: types.UID("uid1")},
				Spec:       routev1.RouteSpec{Host: "route1.test.local"},
				Status: routev1.RouteStatus{Ingress: []routev1.RouteIngress{{
					Host:       "route1.test.local",
					RouterName: "test",
					Conditions: []routev1.RouteIngressCondition{unservableInFutureVersionsTrueCondition},
				},
				}},
			},
		},
		{
			name:       "remove unservableInFutureVersions condition if not unservableInFutureVersions",
			routerName: "test",
			route: &routev1.Route{
				ObjectMeta: metav1.ObjectMeta{Name: "route1", Namespace: "default", UID: types.UID("uid1")},
				Spec:       routev1.RouteSpec{Host: "route1.test.local"},
				Status: routev1.RouteStatus{Ingress: []routev1.RouteIngress{{
					Host:       "route1.test.local",
					RouterName: "test",
					Conditions: []routev1.RouteIngressCondition{unservableInFutureVersionsTrueCondition},
				}},
				},
			},
			expectedRoute: &routev1.Route{
				ObjectMeta: metav1.ObjectMeta{Name: "route1", Namespace: "default", UID: types.UID("uid1")},
				Spec:       routev1.RouteSpec{Host: "route1.test.local"},
				Status: routev1.RouteStatus{Ingress: []routev1.RouteIngress{{
					Host:       "route1.test.local",
					RouterName: "test",
				}},
				},
			},
		},
		{
			name:       "remove unservableInFutureVersions condition if not unservableInFutureVersions with another status",
			routerName: "test",
			route: &routev1.Route{
				ObjectMeta: metav1.ObjectMeta{Name: "route1", Namespace: "default", UID: types.UID("uid1")},
				Spec:       routev1.RouteSpec{Host: "route1.test.local"},
				Status: routev1.RouteStatus{Ingress: []routev1.RouteIngress{{
					Host:       "route1.test.local",
					RouterName: "test",
					Conditions: []routev1.RouteIngressCondition{
						{
							Type:    "AnotherType",
							Status:  corev1.ConditionTrue,
							Reason:  "AnotherStatusReason",
							Message: "a message for AnotherStatusReason",
						},
						unservableInFutureVersionsTrueCondition,
					},
				}},
				},
			},
			expectedRoute: &routev1.Route{
				ObjectMeta: metav1.ObjectMeta{Name: "route1", Namespace: "default", UID: types.UID("uid1")},
				Spec:       routev1.RouteSpec{Host: "route1.test.local"},
				Status: routev1.RouteStatus{Ingress: []routev1.RouteIngress{{
					Host:       "route1.test.local",
					RouterName: "test",
					Conditions: []routev1.RouteIngressCondition{{
						Type:    "AnotherType",
						Status:  corev1.ConditionTrue,
						Reason:  "AnotherStatusReason",
						Message: "a message for AnotherStatusReason",
					}},
				}},
				},
			},
		},
		{
			name:                       "add unservableInFutureVersions condition with existing status condition",
			routerName:                 "test",
			unservableInFutureVersions: true,
			route: &routev1.Route{
				ObjectMeta: metav1.ObjectMeta{Name: "route1", Namespace: "default", UID: types.UID("uid1")},
				Spec:       routev1.RouteSpec{Host: "route1.test.local"},
				Status: routev1.RouteStatus{Ingress: []routev1.RouteIngress{{
					Host:       "route1.test.local",
					RouterName: "test",
					Conditions: []routev1.RouteIngressCondition{{
						Type:    "AnotherType",
						Status:  corev1.ConditionTrue,
						Reason:  "AnotherStatusReason",
						Message: "a message for AnotherStatusReason",
					}},
				}},
				},
			},
			expectedRoute: &routev1.Route{
				ObjectMeta: metav1.ObjectMeta{Name: "route1", Namespace: "default", UID: types.UID("uid1")},
				Spec:       routev1.RouteSpec{Host: "route1.test.local"},
				Status: routev1.RouteStatus{Ingress: []routev1.RouteIngress{{
					Host:       "route1.test.local",
					RouterName: "test",
					Conditions: []routev1.RouteIngressCondition{
						{
							Type:    "AnotherType",
							Status:  corev1.ConditionTrue,
							Reason:  "AnotherStatusReason",
							Message: "a message for AnotherStatusReason",
						},
						unservableInFutureVersionsTrueCondition,
					},
				}},
				},
			},
		},
		{
			name:                       "update incorrect unservableInFutureVersions condition",
			routerName:                 "test",
			unservableInFutureVersions: true,
			route: &routev1.Route{
				ObjectMeta: metav1.ObjectMeta{Name: "route1", Namespace: "default", UID: types.UID("uid1")},
				Spec:       routev1.RouteSpec{Host: "route1.test.local"},
				Status: routev1.RouteStatus{Ingress: []routev1.RouteIngress{{
					Host:       "route1.test.local",
					RouterName: "test",
					Conditions: []routev1.RouteIngressCondition{{
						Type:    routev1.RouteUnservableInFutureVersions,
						Status:  corev1.ConditionTrue,
						Reason:  "wrong reason",
						Message: "wrong message",
					}},
				}},
				},
			},
			expectedRoute: &routev1.Route{
				ObjectMeta: metav1.ObjectMeta{Name: "route1", Namespace: "default", UID: types.UID("uid1")},
				Spec:       routev1.RouteSpec{Host: "route1.test.local"},
				Status: routev1.RouteStatus{Ingress: []routev1.RouteIngress{{
					Host:       "route1.test.local",
					RouterName: "test",
					Conditions: []routev1.RouteIngressCondition{
						unservableInFutureVersionsTrueCondition,
					},
				}},
				},
			},
		},
		{
			name:                       "no update for incorrect host name with unservableInFutureVersions condition",
			routerName:                 "test",
			unservableInFutureVersions: true,
			route: &routev1.Route{
				ObjectMeta: metav1.ObjectMeta{Name: "route1", Namespace: "default", UID: types.UID("uid1")},
				Spec:       routev1.RouteSpec{Host: "route1.test.local"},
				Status: routev1.RouteStatus{Ingress: []routev1.RouteIngress{{
					Host:       "foo.test.local",
					RouterName: "test",
					Conditions: []routev1.RouteIngressCondition{
						unservableInFutureVersionsTrueCondition,
					},
				}},
				},
			},
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			now := nowFn()
			nowFn = func() metav1.Time { return now }
			p := &fakePlugin{}
			c := fake.NewSimpleClientset(tc.route)
			tracker := &fakeTracker{}
			lister := &routeLister{items: []*routev1.Route{tc.route}}
			admitter := NewStatusAdmitter(p, c.RouteV1(), lister, tc.routerName, "", noopLease{}, tracker)
			if tc.unservableInFutureVersions {
				admitter.RecordRouteUnservableInFutureVersions(tc.route, unservableInFutureVersionsTrueCondition.Reason, unservableInFutureVersionsTrueCondition.Message)
			} else {
				admitter.RecordRouteUnservableInFutureVersionsClear(tc.route)
			}

			// If expected route is nil, then assume we expect nothing to happen.
			if tc.expectedRoute == nil {
				if len(c.Actions()) != 0 {
					t.Fatalf("expected 0 actions, but got %d: %#v", len(c.Actions()), c.Actions())
				}
			} else {
				if len(c.Actions()) != 1 {
					t.Fatalf("expected 1 actions, but got %d: %#v", len(c.Actions()), c.Actions())
				}
				action := c.Actions()[0]
				if action.GetVerb() != "update" || action.GetResource().Resource != "routes" || action.GetSubresource() != "status" {
					t.Fatalf("unexpected action: %#v", action)
				}
				obj := c.Actions()[0].(clientgotesting.UpdateAction).GetObject().(*routev1.Route)

				// Compare expected route, but ignore LastTransitionTime since that is generated
				cmpOpts := []cmp.Option{
					cmpopts.EquateEmpty(),
					cmpopts.IgnoreFields(routev1.RouteIngressCondition{}, "LastTransitionTime"),
				}
				if diff := cmp.Diff(tc.expectedRoute, obj, cmpOpts...); len(diff) > 0 {
					t.Errorf("mismatched routes (-want +got):\n%s", diff)
				}
			}
		})
	}
}

// Test_recordIngressCondition tests recordIngressCondition. While it may appear like overkill, testing the functions
// that invoke these helpers is insufficient. There are certain logic paths that can only be exposed by testing
// directly, such as scenarios where a status is admitted by another router.
func Test_recordIngressCondition(t *testing.T) {
	admittedTrueCondition := routev1.RouteIngressCondition{
		Type:    routev1.RouteAdmitted,
		Status:  corev1.ConditionTrue,
		Reason:  "Test",
		Message: "test",
	}
	unservableInFutureVersionsTrueCondition := routev1.RouteIngressCondition{
		Type:    routev1.RouteUnservableInFutureVersions,
		Status:  corev1.ConditionTrue,
		Reason:  "Test",
		Message: "test",
	}
	testCases := []struct {
		name                    string
		route                   *routev1.Route
		routerName              string
		routerCanonicalHostname string
		condition               routev1.RouteIngressCondition
		expectedRoute           *routev1.Route
		expectCreated           bool
		expectChanged           bool
	}{
		{
			name:                    "add new ingress with condition",
			routerName:              "foo",
			routerCanonicalHostname: "router-foo.foo.local",
			route: &routev1.Route{
				Spec:   routev1.RouteSpec{Host: "foo.foo.local"},
				Status: routev1.RouteStatus{},
			},
			condition: admittedTrueCondition,
			expectedRoute: &routev1.Route{
				Spec: routev1.RouteSpec{Host: "foo.foo.local"},
				Status: routev1.RouteStatus{Ingress: []routev1.RouteIngress{
					{
						Host:                    "foo.foo.local",
						RouterCanonicalHostname: "router-foo.foo.local",
						RouterName:              "foo",
						Conditions:              []routev1.RouteIngressCondition{admittedTrueCondition},
					},
				}},
			},
			expectChanged: true,
			expectCreated: true,
		},
		{
			name:                    "add new condition to existing ingress with incorrect value",
			routerName:              "foo",
			routerCanonicalHostname: "router-foo.foo.local",
			route: &routev1.Route{
				Spec: routev1.RouteSpec{Host: "foo.foo.local"},
				Status: routev1.RouteStatus{Ingress: []routev1.RouteIngress{
					{
						RouterCanonicalHostname: "router1.not-foo.local",
						RouterName:              "foo",
					},
				}},
			},
			condition: admittedTrueCondition,
			expectedRoute: &routev1.Route{
				Spec: routev1.RouteSpec{Host: "foo.foo.local"},
				Status: routev1.RouteStatus{Ingress: []routev1.RouteIngress{
					{
						Host:                    "foo.foo.local",
						RouterName:              "foo",
						RouterCanonicalHostname: "router-foo.foo.local",
						Conditions:              []routev1.RouteIngressCondition{admittedTrueCondition},
					},
				}},
			},
			expectChanged: true,
			expectCreated: false,
		},
		{
			name:                    "add new condition, but it already exists, therefore no-op",
			routerName:              "foo",
			routerCanonicalHostname: "router-foo.foo.local",
			route: &routev1.Route{
				Spec: routev1.RouteSpec{Host: "foo.foo.local"},
				Status: routev1.RouteStatus{Ingress: []routev1.RouteIngress{
					{
						Host:                    "foo.foo.local",
						RouterName:              "foo",
						RouterCanonicalHostname: "router-foo.foo.local",
						Conditions:              []routev1.RouteIngressCondition{admittedTrueCondition},
					},
				}},
			},
			condition: admittedTrueCondition,
			expectedRoute: &routev1.Route{
				Spec: routev1.RouteSpec{Host: "foo.foo.local"},
				Status: routev1.RouteStatus{Ingress: []routev1.RouteIngress{
					{
						Host:                    "foo.foo.local",
						RouterName:              "foo",
						RouterCanonicalHostname: "router-foo.foo.local",
						Conditions:              []routev1.RouteIngressCondition{admittedTrueCondition},
					},
				}},
			},
			expectChanged: false,
			expectCreated: false,
		},
		{
			name:                    "add new condition to existing ingress with existing condition",
			routerName:              "foo",
			routerCanonicalHostname: "router-foo.foo.local",
			route: &routev1.Route{
				Spec: routev1.RouteSpec{Host: "foo.foo.local"},
				Status: routev1.RouteStatus{Ingress: []routev1.RouteIngress{
					{
						Host:       "route1.foo.local",
						RouterName: "foo",
						Conditions: []routev1.RouteIngressCondition{unservableInFutureVersionsTrueCondition},
					},
				}},
			},
			condition: admittedTrueCondition,
			expectedRoute: &routev1.Route{
				Spec: routev1.RouteSpec{Host: "foo.foo.local"},
				Status: routev1.RouteStatus{Ingress: []routev1.RouteIngress{
					{
						Host:                    "foo.foo.local",
						RouterName:              "foo",
						RouterCanonicalHostname: "router-foo.foo.local",
						Conditions: []routev1.RouteIngressCondition{
							unservableInFutureVersionsTrueCondition,
							admittedTrueCondition,
						},
					},
				}},
			},
			expectChanged: true,
			expectCreated: false,
		},
		{
			name:                    "add new condition, but another router's ingress exists",
			routerName:              "foo",
			routerCanonicalHostname: "router-foo.foo.local",
			route: &routev1.Route{
				Spec: routev1.RouteSpec{Host: "foo.foo.local"},
				Status: routev1.RouteStatus{Ingress: []routev1.RouteIngress{
					{
						Host:       "foo.foo.local",
						RouterName: "bar",
						Conditions: []routev1.RouteIngressCondition{unservableInFutureVersionsTrueCondition},
					},
				}},
			},
			condition: admittedTrueCondition,
			expectedRoute: &routev1.Route{
				Spec: routev1.RouteSpec{Host: "foo.foo.local"},
				Status: routev1.RouteStatus{Ingress: []routev1.RouteIngress{
					{
						Host:       "foo.foo.local",
						RouterName: "bar",
						Conditions: []routev1.RouteIngressCondition{unservableInFutureVersionsTrueCondition},
					},
					{
						Host:                    "foo.foo.local",
						RouterCanonicalHostname: "router-foo.foo.local",
						RouterName:              "foo",
						Conditions:              []routev1.RouteIngressCondition{admittedTrueCondition},
					},
				}},
			},
			expectChanged: true,
			expectCreated: true,
		},
		{
			name:                    "add new condition, but ingress slice is empty",
			routerName:              "foo",
			routerCanonicalHostname: "router-foo.foo.local",
			route: &routev1.Route{
				Spec:   routev1.RouteSpec{Host: "foo.foo.local"},
				Status: routev1.RouteStatus{Ingress: []routev1.RouteIngress{}},
			},
			condition: admittedTrueCondition,
			expectedRoute: &routev1.Route{
				Spec: routev1.RouteSpec{Host: "foo.foo.local"},
				Status: routev1.RouteStatus{Ingress: []routev1.RouteIngress{
					{
						Host:                    "foo.foo.local",
						RouterCanonicalHostname: "router-foo.foo.local",
						RouterName:              "foo",
						Conditions:              []routev1.RouteIngressCondition{admittedTrueCondition},
					},
				}},
			},
			expectChanged: true,
			expectCreated: true,
		},
		{
			name:                    "add new condition, but condition slice is empty",
			routerName:              "foo",
			routerCanonicalHostname: "router-foo.foo.local",
			route: &routev1.Route{
				Spec: routev1.RouteSpec{Host: "foo.foo.local"},
				Status: routev1.RouteStatus{Ingress: []routev1.RouteIngress{
					{
						Host:                    "foo.foo.local",
						RouterCanonicalHostname: "router-foo.foo.local",
						RouterName:              "foo",
						Conditions:              []routev1.RouteIngressCondition{},
					},
				}},
			},
			condition: admittedTrueCondition,
			expectedRoute: &routev1.Route{
				Spec: routev1.RouteSpec{Host: "foo.foo.local"},
				Status: routev1.RouteStatus{Ingress: []routev1.RouteIngress{
					{
						Host:                    "foo.foo.local",
						RouterCanonicalHostname: "router-foo.foo.local",
						RouterName:              "foo",
						Conditions:              []routev1.RouteIngressCondition{admittedTrueCondition},
					},
				}},
			},
			expectChanged: true,
			expectCreated: false,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			baselineIngressState := findIngressForRoute(tc.route, tc.routerName).DeepCopy()

			changed, created, _, latest, original := recordIngressCondition(tc.route, tc.routerName, tc.routerCanonicalHostname, tc.condition)

			// Compare expected route, but ignore LastTransitionTime since that is generated
			cmpOpts := []cmp.Option{
				cmpopts.EquateEmpty(),
				cmpopts.IgnoreFields(routev1.RouteIngressCondition{}, "LastTransitionTime"),
			}
			if diff := cmp.Diff(tc.expectedRoute, tc.route, cmpOpts...); len(diff) > 0 {
				t.Errorf("mismatched routes (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(findIngressForRoute(tc.route, tc.routerName), latest, cmpOpts...); len(diff) > 0 {
				t.Errorf("expected latest to match route ingress (-want +got):\n%s", diff)
			}
			if tc.expectCreated != created {
				t.Errorf("expected created=%t, but got created=%t", tc.expectCreated, created)
			}
			if tc.expectChanged != changed {
				t.Errorf("expected changed=%t, but got changed=%t", tc.expectChanged, changed)
			}
			if diff := cmp.Diff(baselineIngressState, original, cmpOpts...); len(diff) > 0 {
				t.Errorf("expected original to match baseline ingress from route.Status (-want +got):\n%s", diff)
			}
		})
	}
}

// Test_removeIngressCondition tests removeIngressCondition. While it may appear like overkill, testing the functions
// that invoke these helpers is insufficient. There are certain logic paths that can only be exposed by testing
// directly, such as scenarios where a status is admitted by another router.
func Test_removeIngressCondition(t *testing.T) {
	admittedTrueCondition := routev1.RouteIngressCondition{
		Type:    routev1.RouteAdmitted,
		Status:  corev1.ConditionTrue,
		Reason:  "Test",
		Message: "test",
	}
	unservableInFutureVersionsTrueCondition := routev1.RouteIngressCondition{
		Type:    routev1.RouteUnservableInFutureVersions,
		Status:  corev1.ConditionTrue,
		Reason:  "Test",
		Message: "test",
	}
	testCases := []struct {
		name          string
		route         *routev1.Route
		routerName    string
		conditionType routev1.RouteIngressConditionType
		expectedRoute *routev1.Route
		expectChanged bool
	}{
		{
			name:       "condition not found",
			routerName: "foo",
			route: &routev1.Route{
				Spec: routev1.RouteSpec{Host: "foo.foo.local"},
				Status: routev1.RouteStatus{Ingress: []routev1.RouteIngress{{
					Host:                    "foo.foo.local",
					RouterName:              "foo",
					RouterCanonicalHostname: "router-foo.foo.local",
					Conditions:              []routev1.RouteIngressCondition{admittedTrueCondition}},
				}},
			},
			conditionType: routev1.RouteUnservableInFutureVersions,
			expectedRoute: &routev1.Route{
				Spec: routev1.RouteSpec{Host: "foo.foo.local"},
				Status: routev1.RouteStatus{Ingress: []routev1.RouteIngress{{
					Host:                    "foo.foo.local",
					RouterName:              "foo",
					RouterCanonicalHostname: "router-foo.foo.local",
					Conditions:              []routev1.RouteIngressCondition{admittedTrueCondition}},
				}},
			},
		},
		{
			name:       "ingress not found",
			routerName: "foo",
			route: &routev1.Route{
				Spec: routev1.RouteSpec{Host: "foo.foo.local"},
				Status: routev1.RouteStatus{Ingress: []routev1.RouteIngress{{
					Host:                    "bar.bar.local",
					RouterName:              "bar",
					RouterCanonicalHostname: "router-bar.bar.local",
					Conditions:              []routev1.RouteIngressCondition{unservableInFutureVersionsTrueCondition}},
				}},
			},
			conditionType: routev1.RouteUnservableInFutureVersions,
			expectedRoute: &routev1.Route{
				Spec: routev1.RouteSpec{Host: "foo.foo.local"},
				Status: routev1.RouteStatus{Ingress: []routev1.RouteIngress{{
					Host:                    "bar.bar.local",
					RouterName:              "bar",
					RouterCanonicalHostname: "router-bar.bar.local",
					Conditions:              []routev1.RouteIngressCondition{unservableInFutureVersionsTrueCondition}},
				}},
			},
		},
		{
			name:       "remove condition found",
			routerName: "foo",
			route: &routev1.Route{
				Spec: routev1.RouteSpec{Host: "foo.foo.local"},
				Status: routev1.RouteStatus{Ingress: []routev1.RouteIngress{{
					Host:                    "foo.foo.local",
					RouterName:              "foo",
					RouterCanonicalHostname: "router-foo.foo.local",
					Conditions: []routev1.RouteIngressCondition{
						admittedTrueCondition,
						unservableInFutureVersionsTrueCondition,
					}},
				}},
			},
			conditionType: routev1.RouteUnservableInFutureVersions,
			expectedRoute: &routev1.Route{
				Spec: routev1.RouteSpec{Host: "foo.foo.local"},
				Status: routev1.RouteStatus{Ingress: []routev1.RouteIngress{{
					Host:                    "foo.foo.local",
					RouterName:              "foo",
					RouterCanonicalHostname: "router-foo.foo.local",
					Conditions:              []routev1.RouteIngressCondition{admittedTrueCondition}},
				}},
			},
			expectChanged: true,
		},
		{
			name:       "remove condition found with other ingresses",
			routerName: "foo",
			route: &routev1.Route{
				Spec: routev1.RouteSpec{Host: "foo.foo.local"},
				Status: routev1.RouteStatus{Ingress: []routev1.RouteIngress{
					{
						Host:                    "bar.bar.local",
						RouterName:              "bar",
						RouterCanonicalHostname: "router-bar.foo.local",
						Conditions: []routev1.RouteIngressCondition{
							admittedTrueCondition,
							unservableInFutureVersionsTrueCondition,
						},
					},
					{
						Host:                    "foo.foo.local",
						RouterName:              "foo",
						RouterCanonicalHostname: "router-foo.foo.local",
						Conditions: []routev1.RouteIngressCondition{
							admittedTrueCondition,
							unservableInFutureVersionsTrueCondition,
						},
					},
				}},
			},
			conditionType: routev1.RouteUnservableInFutureVersions,
			expectedRoute: &routev1.Route{
				Spec: routev1.RouteSpec{Host: "foo.foo.local"},
				Status: routev1.RouteStatus{Ingress: []routev1.RouteIngress{
					{
						Host:                    "bar.bar.local",
						RouterName:              "bar",
						RouterCanonicalHostname: "router-bar.foo.local",
						Conditions: []routev1.RouteIngressCondition{
							admittedTrueCondition,
							unservableInFutureVersionsTrueCondition,
						},
					},
					{
						Host:                    "foo.foo.local",
						RouterName:              "foo",
						RouterCanonicalHostname: "router-foo.foo.local",
						Conditions:              []routev1.RouteIngressCondition{admittedTrueCondition},
					},
				}},
			},
			expectChanged: true,
		},
		{
			name:       "remove condition not found with other ingresses",
			routerName: "foo",
			route: &routev1.Route{
				Spec: routev1.RouteSpec{Host: "foo.foo.local"},
				Status: routev1.RouteStatus{Ingress: []routev1.RouteIngress{
					{
						Host:                    "bar.bar.local",
						RouterName:              "bar",
						RouterCanonicalHostname: "router-bar.foo.local",
						Conditions: []routev1.RouteIngressCondition{
							admittedTrueCondition,
							unservableInFutureVersionsTrueCondition,
						},
					},
					{
						Host:                    "foo.foo.local",
						RouterName:              "foo",
						RouterCanonicalHostname: "router-foo.foo.local",
						Conditions: []routev1.RouteIngressCondition{
							admittedTrueCondition,
						},
					},
				}},
			},
			conditionType: routev1.RouteUnservableInFutureVersions,
			expectedRoute: &routev1.Route{
				Spec: routev1.RouteSpec{Host: "foo.foo.local"},
				Status: routev1.RouteStatus{Ingress: []routev1.RouteIngress{
					{
						Host:                    "bar.bar.local",
						RouterName:              "bar",
						RouterCanonicalHostname: "router-bar.foo.local",
						Conditions: []routev1.RouteIngressCondition{
							admittedTrueCondition,
							unservableInFutureVersionsTrueCondition,
						},
					},
					{
						Host:                    "foo.foo.local",
						RouterName:              "foo",
						RouterCanonicalHostname: "router-foo.foo.local",
						Conditions:              []routev1.RouteIngressCondition{admittedTrueCondition},
					},
				}},
			},
			expectChanged: false,
		},
		{
			name:       "duplicate ingress conditions",
			routerName: "foo",
			route: &routev1.Route{
				Spec: routev1.RouteSpec{Host: "foo.foo.local"},
				Status: routev1.RouteStatus{Ingress: []routev1.RouteIngress{
					{
						Host:                    "foo.foo.local",
						RouterName:              "foo",
						RouterCanonicalHostname: "router-foo.foo.local",
						Conditions: []routev1.RouteIngressCondition{
							admittedTrueCondition,
							unservableInFutureVersionsTrueCondition,
							unservableInFutureVersionsTrueCondition,
						},
					},
				}},
			},
			conditionType: routev1.RouteUnservableInFutureVersions,
			expectedRoute: &routev1.Route{
				Spec: routev1.RouteSpec{Host: "foo.foo.local"},
				Status: routev1.RouteStatus{Ingress: []routev1.RouteIngress{
					{
						Host:                    "foo.foo.local",
						RouterName:              "foo",
						RouterCanonicalHostname: "router-foo.foo.local",
						Conditions: []routev1.RouteIngressCondition{
							admittedTrueCondition,
						},
					},
				}},
			},
			expectChanged: true,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			baselineIngressState := findIngressForRoute(tc.route, tc.routerName).DeepCopy()
			changed, _, latest, original := removeIngressCondition(tc.route, tc.routerName, tc.conditionType)

			// Compare expected route, but ignore LastTransitionTime since that is generated
			cmpOpts := []cmp.Option{
				cmpopts.EquateEmpty(),
				cmpopts.IgnoreFields(routev1.RouteIngressCondition{}, "LastTransitionTime"),
			}
			if diff := cmp.Diff(tc.expectedRoute, tc.route, cmpOpts...); len(diff) > 0 {
				t.Errorf("mismatched routes (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(findIngressForRoute(tc.route, tc.routerName), latest, cmpOpts...); len(diff) > 0 {
				t.Errorf("expected latest to match route ingress (-want +got):\n%s", diff)
			}
			if tc.expectChanged != changed {
				t.Errorf("expected changed=%t, but got changed=%t", tc.expectChanged, changed)
			}
			if diff := cmp.Diff(baselineIngressState, original, cmpOpts...); len(diff) > 0 {
				t.Errorf("expected baselineIngressState to match original output from removeIngressCondition (-want +got):\n%s", diff)
			}
		})
	}
}

func ingressChangeWithNewHost(route *routev1.Route, routerName, newHost string) *routev1.RouteIngress {
	ingress := findIngressForRoute(route, routerName).DeepCopy()
	ingress.Host = newHost
	return ingress
}

type fakeInformer struct {
	handlers []cache.ResourceEventHandler
}

func (i *fakeInformer) Update(old, obj interface{}) {
	for _, h := range i.handlers {
		h.OnUpdate(old, obj)
	}
}

func (i *fakeInformer) AddEventHandler(handler cache.ResourceEventHandler) (cache.ResourceEventHandlerRegistration, error) {
	i.handlers = append(i.handlers, handler)
	return nil, nil
}

func (i *fakeInformer) AddEventHandlerWithOptions(handler cache.ResourceEventHandler, options cache.HandlerOptions) (cache.ResourceEventHandlerRegistration, error) {
	panic("not implemented")
}

func (i *fakeInformer) AddEventHandlerWithResyncPeriod(handler cache.ResourceEventHandler, resyncPeriod time.Duration) (cache.ResourceEventHandlerRegistration, error) {
	panic("not implemented")
}

func (i *fakeInformer) RemoveEventHandler(handler cache.ResourceEventHandlerRegistration) error {
	panic("not implemented")
}

func (i *fakeInformer) SetWatchErrorHandler(handler cache.WatchErrorHandler) error {
	panic("not implemented")
}

func (i *fakeInformer) GetStore() cache.Store {
	panic("not implemented")
}

func (i *fakeInformer) GetController() cache.Controller {
	panic("not implemented")
}

func (i *fakeInformer) Run(stopCh <-chan struct{}) {
	panic("not implemented")
}

func (i *fakeInformer) RunWithContext(ctx context.Context) {
	panic("not implemented")
}

func (i *fakeInformer) HasSynced() bool {
	panic("not implemented")
}

func (i *fakeInformer) LastSyncResourceVersion() string {
	panic("not implemented")
}

func (i *fakeInformer) SetWatchErrorHandlerWithContext(handler cache.WatchErrorHandlerWithContext) error {
	panic("not implemented")
}

func (i *fakeInformer) SetTransform(handler cache.TransformFunc) error {
	panic("not implemented")
}

func (i *fakeInformer) IsStopped() bool {
	panic("not implemented")
}
