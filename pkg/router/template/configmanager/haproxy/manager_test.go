package haproxy

import (
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

	commits []bool
}

// Commit records the force argument of every call.
func (r *fakeRouter) Commit(force bool) {
	r.commits = append(r.commits, force)
}

// fakeTimers replaces time.AfterFunc, so tests decide when timers fire.
type fakeTimers struct {
	timers    []*time.Timer
	funcs     []func()
	durations []time.Duration
}

// AfterFunc records f, and returns a real timer that never fires on its own.
// The timer is only used to track whether it was stopped.
func (ft *fakeTimers) AfterFunc(d time.Duration, f func()) *time.Timer {
	timer := time.AfterFunc(24*time.Hour, func() {})
	ft.timers = append(ft.timers, timer)
	ft.funcs = append(ft.funcs, f)
	ft.durations = append(ft.durations, d)
	return timer
}

// Fire emulates the passing of time, calling the function of all the timers
// that are still pending, and have not been stopped.
func (ft *fakeTimers) Fire() {
	for i, timer := range ft.timers {
		if timer.Stop() {
			ft.funcs[i]()
		}
	}
}

// Stop stops all the timers.
func (ft *fakeTimers) Stop() {
	for _, timer := range ft.timers {
		timer.Stop()
	}
}

// TestCommitInterval verifies that dynamic changes schedule a forced commit
// of the router, that changes are coalesced into a single commit, and that a
// router reload cancels a pending commit.
func TestCommitInterval(t *testing.T) {
	const commitInterval = 10 * time.Minute

	ep1 := templaterouter.Endpoint{ID: "ep1", IP: "10.0.1.11", Port: "8080", NoHealthCheck: true}
	ep2 := templaterouter.Endpoint{ID: "ep2", IP: "10.0.1.12", Port: "8080", NoHealthCheck: true}

	type step func(t *testing.T, cm *haproxyConfigManager, timers *fakeTimers)
	removeRoute := func(t *testing.T, cm *haproxyConfigManager, _ *fakeTimers) {
		require.NoError(t, cm.RemoveRoute(testRouteID, &routev1.Route{}))
	}
	removeEndpoints := func(t *testing.T, cm *haproxyConfigManager, _ *fakeTimers) {
		require.NoError(t, cm.RemoveRouteEndpoints(testRouteID, []templaterouter.Endpoint{ep1}))
	}
	addEndpoints := func(t *testing.T, cm *haproxyConfigManager, _ *fakeTimers) {
		require.NoError(t, cm.ReplaceRouteEndpoints(testRouteID, &templaterouter.ServiceUnit{}, []templaterouter.Endpoint{ep1}, []templaterouter.Endpoint{ep1, ep2}, 2))
	}
	unchangedEndpoints := func(t *testing.T, cm *haproxyConfigManager, _ *fakeTimers) {
		require.NoError(t, cm.ReplaceRouteEndpoints(testRouteID, &templaterouter.ServiceUnit{}, []templaterouter.Endpoint{ep1}, []templaterouter.Endpoint{ep1}, 1))
	}
	reload := func(_ *testing.T, cm *haproxyConfigManager, _ *fakeTimers) {
		cm.Notify(templaterouter.RouterEventReloadStart)
		cm.Notify(templaterouter.RouterEventReloadEnd)
	}
	elapse := func(_ *testing.T, _ *haproxyConfigManager, timers *fakeTimers) {
		timers.Fire()
	}

	testCases := map[string]struct {
		steps           []step
		commitsExpected []bool
	}{
		"no changes do not commit": {
			steps: []step{elapse},
		},
		"route removal commits": {
			steps:           []step{removeRoute, elapse},
			commitsExpected: []bool{true},
		},
		"endpoints removal commits": {
			steps:           []step{removeEndpoints, elapse},
			commitsExpected: []bool{true},
		},
		"endpoints replacement commits": {
			steps:           []step{addEndpoints, elapse},
			commitsExpected: []bool{true},
		},
		"unchanged endpoints do not commit": {
			steps: []step{unchangedEndpoints, elapse},
		},
		"commit waits for the interval": {
			steps: []step{removeEndpoints},
		},
		"changes in the same interval are coalesced": {
			steps:           []step{removeEndpoints, addEndpoints, removeRoute, elapse, elapse},
			commitsExpected: []bool{true},
		},
		"changes after a commit schedule a new commit": {
			steps:           []step{removeEndpoints, elapse, addEndpoints, elapse},
			commitsExpected: []bool{true, true},
		},
		"reload cancels a pending commit": {
			steps: []step{removeEndpoints, reload, elapse},
		},
		"changes after a reload schedule a new commit": {
			steps:           []step{removeEndpoints, reload, addEndpoints, elapse},
			commitsExpected: []bool{true},
		},
	}

	for name, test := range testCases {
		t.Run(name, func(t *testing.T) {
			cm, router := newTestConfigManager(t, commitInterval)
			timers := &fakeTimers{}
			t.Cleanup(timers.Stop)
			cm.afterFunc = timers.AfterFunc

			for _, s := range test.steps {
				s(t, cm, timers)
			}

			assert.Equal(t, test.commitsExpected, router.commits)
			for _, d := range timers.durations {
				assert.Equal(t, commitInterval, d)
			}
		})
	}
}

// testRouteID is the id of the route registered by newTestConfigManager().
const testRouteID = templaterouter.ServiceAliasConfigKey("default:example-route")

// newTestConfigManager returns a config manager connected to a fake haproxy,
// with an edge route registered on a backend that the fake haproxy serves.
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
	cm.Register(testRouteID, &templaterouter.ServiceAliasConfig{}, route)

	return cm, router
}
