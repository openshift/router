package haproxy

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	routev1 "github.com/openshift/api/route/v1"

	templaterouter "github.com/openshift/router/pkg/router/template"
	haproxytesting "github.com/openshift/router/pkg/router/template/configmanager/haproxy/testing"
)

// fakeRouter is a template router that only records Commit() calls. Any
// other method panics, since the config manager is not expected to call them.
type fakeRouter struct {
	templaterouter.RouterInterface

	lock    sync.Mutex
	commits []bool
}

// Commit records the force argument of every call.
func (r *fakeRouter) Commit(force bool) {
	r.lock.Lock()
	defer r.lock.Unlock()
	r.commits = append(r.commits, force)
}

// Commits returns a copy of the force argument of all the Commit() calls.
func (r *fakeRouter) Commits() []bool {
	r.lock.Lock()
	defer r.lock.Unlock()
	return append([]bool{}, r.commits...)
}

// TestCommitInterval verifies that dynamic changes schedule a forced commit
// of the router, and that a router reload cancels a pending commit.
func TestCommitInterval(t *testing.T) {
	const routeID = templaterouter.ServiceAliasConfigKey("default:example-route")

	ep1 := templaterouter.Endpoint{ID: "ep1", IP: "10.0.1.11", Port: "8080", NoHealthCheck: true}
	ep2 := templaterouter.Endpoint{ID: "ep2", IP: "10.0.1.12", Port: "8080", NoHealthCheck: true}

	// Every step is a change made on the config manager. A long commit
	// interval is used, so the timer never fires on its own and the test
	// can assert whether a commit is pending.
	type step func(cm *haproxyConfigManager)
	removeRoute := func(cm *haproxyConfigManager) {
		_ = cm.RemoveRoute(routeID, &routev1.Route{})
	}
	removeEndpoints := func(cm *haproxyConfigManager) {
		_ = cm.RemoveRouteEndpoints(routeID, []templaterouter.Endpoint{ep1})
	}
	addEndpoints := func(cm *haproxyConfigManager) {
		_ = cm.ReplaceRouteEndpoints(routeID, &templaterouter.ServiceUnit{}, []templaterouter.Endpoint{ep1}, []templaterouter.Endpoint{ep1, ep2}, 2)
	}
	unchangedEndpoints := func(cm *haproxyConfigManager) {
		_ = cm.ReplaceRouteEndpoints(routeID, &templaterouter.ServiceUnit{}, []templaterouter.Endpoint{ep1}, []templaterouter.Endpoint{ep1}, 1)
	}
	reloadEnd := func(cm *haproxyConfigManager) {
		cm.Notify(templaterouter.RouterEventReloadStart)
		cm.Notify(templaterouter.RouterEventReloadEnd)
	}

	testCases := map[string]struct {
		steps           []step
		pendingExpected bool
	}{
		"no changes do not schedule a commit": {
			pendingExpected: false,
		},
		"route removal schedules a commit": {
			steps:           []step{removeRoute},
			pendingExpected: true,
		},
		"endpoints removal schedules a commit": {
			steps:           []step{removeEndpoints},
			pendingExpected: true,
		},
		"endpoints replacement schedules a commit": {
			steps:           []step{addEndpoints},
			pendingExpected: true,
		},
		"unchanged endpoints do not schedule a commit": {
			steps:           []step{unchangedEndpoints},
			pendingExpected: false,
		},
		"reload cancels a pending commit": {
			steps:           []step{removeEndpoints, reloadEnd},
			pendingExpected: false,
		},
		"change after a reload schedules a new commit": {
			steps:           []step{removeEndpoints, reloadEnd, addEndpoints},
			pendingExpected: true,
		},
	}

	for name, test := range testCases {
		t.Run(name, func(t *testing.T) {
			cm, _ := newTestConfigManager(t, time.Hour)
			for _, s := range test.steps {
				s(cm)
			}

			cm.lock.Lock()
			pending := cm.commitTimer != nil
			cm.lock.Unlock()
			assert.Equal(t, test.pendingExpected, pending)

			// Stop the timer, if any, so it does not outlive the test.
			reloadEnd(cm)
		})
	}
}

// TestCommitIntervalForcesCommit verifies that the commit timer forces a
// commit of the router once, coalescing all the changes made in the interval,
// and that a new change after that schedules a new commit.
func TestCommitIntervalForcesCommit(t *testing.T) {
	const routeID = templaterouter.ServiceAliasConfigKey("default:example-route")
	ep1 := templaterouter.Endpoint{ID: "ep1", IP: "10.0.1.11", Port: "8080"}

	cm, router := newTestConfigManager(t, time.Second)

	_ = cm.RemoveRouteEndpoints(routeID, []templaterouter.Endpoint{ep1})
	_ = cm.RemoveRouteEndpoints(routeID, []templaterouter.Endpoint{ep1})
	assert.Empty(t, router.Commits(), "commit should wait for the commit interval")

	require.Eventually(t, func() bool { return len(router.Commits()) == 1 }, 10*time.Second, 100*time.Millisecond)
	assert.Equal(t, []bool{true}, router.Commits())

	_ = cm.RemoveRouteEndpoints(routeID, []templaterouter.Endpoint{ep1})
	require.Eventually(t, func() bool { return len(router.Commits()) == 2 }, 10*time.Second, 100*time.Millisecond)
	assert.Equal(t, []bool{true, true}, router.Commits())
}

// newTestConfigManager returns a config manager connected to a fake haproxy,
// with an edge route registered on the backend that the fake haproxy serves.
func newTestConfigManager(t *testing.T, commitInterval time.Duration) (*haproxyConfigManager, *fakeRouter) {
	server := haproxytesting.StartFakeServerForTest(t)
	t.Cleanup(server.Stop)

	router := &fakeRouter{}
	cm := NewHAProxyConfigManager(templaterouter.ConfigManagerOptions{
		ConnectionInfo: server.SocketFile(),
		CommitInterval: commitInterval,
		WorkingDir:     t.TempDir(),
	})
	cm.Initialize(router, "")

	route := &routev1.Route{
		ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "example-route"},
		Spec: routev1.RouteSpec{
			Host: "example.com",
			TLS:  &routev1.TLSConfig{Termination: routev1.TLSTerminationEdge},
		},
	}
	cm.Register("default:example-route", &templaterouter.ServiceAliasConfig{}, route)

	return cm, router
}
