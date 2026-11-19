package haproxy

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	routev1 "github.com/openshift/api/route/v1"

	templaterouter "github.com/openshift/router/pkg/router/template"
)

func TestReplaceRouteEndpoints(t *testing.T) {
	ep1 := templaterouter.Endpoint{ID: "ep1", IP: "10.0.1.11", Port: "8080", Weight: 1}
	ep2 := templaterouter.Endpoint{ID: "ep2", IP: "10.0.1.12", Port: "8080", Weight: 1}
	ep3 := templaterouter.Endpoint{ID: "ep3", IP: "10.0.1.13", Port: "8080", Weight: 1}

	ep2weight2 := ep2
	ep2weight2.Weight = 2

	ep2h2 := ep2
	ep2h2.AppProtocol = "h2c"

	testCases := map[string]struct {
		oldEndpoints    []templaterouter.Endpoint
		newEndpoints    []templaterouter.Endpoint
		activeEndpoints int
		cmdCustomResp   map[int]string
		cmdErrors       map[int]error
		expectedCmds    []string
		expectedError   string
	}{
		"no endpoints": {
			// e.g. a passthrough service whose weight is zero, which has no backend servers.
			expectedCmds: nil,
		},
		"adding one endpoint": {
			newEndpoints: []templaterouter.Endpoint{ep1},
			expectedCmds: []string{
				"add server be_http:service1/ep1 10.0.1.11:8080 weight 1 check inter 5000ms",
				"set server be_http:service1/ep1 state ready",
				"disable health be_http:service1/ep1",
				"set server be_http:service1/ep1 health up",
			},
		},
		"adding one endpoint having more than one service": {
			newEndpoints: []templaterouter.Endpoint{ep1},
			// two activeEndpoints: one endpoint from this service, another one from the second service,
			// so health check should be enabled.
			activeEndpoints: 2,
			expectedCmds: []string{
				"add server be_http:service1/ep1 10.0.1.11:8080 weight 1 check inter 5000ms",
				"set server be_http:service1/ep1 state ready",
				"enable health be_http:service1/ep1",
			},
		},
		"adding two endpoints": {
			newEndpoints: []templaterouter.Endpoint{ep1, ep2},
			expectedCmds: []string{
				"add server be_http:service1/ep1 10.0.1.11:8080 weight 1 check inter 5000ms",
				"set server be_http:service1/ep1 state ready",
				"add server be_http:service1/ep2 10.0.1.12:8080 weight 1 check inter 5000ms",
				"set server be_http:service1/ep2 state ready",
				"enable health be_http:service1/ep1",
				"enable health be_http:service1/ep2",
			},
		},
		"scaling out to two": {
			oldEndpoints: []templaterouter.Endpoint{ep1},
			newEndpoints: []templaterouter.Endpoint{ep1, ep2},
			expectedCmds: []string{
				"add server be_http:service1/ep2 10.0.1.12:8080 weight 1 check inter 5000ms",
				"set server be_http:service1/ep2 state ready",
				"enable health be_http:service1/ep1",
				"enable health be_http:service1/ep2",
			},
		},
		"scaling out to two and first server failing to enable health": {
			oldEndpoints: []templaterouter.Endpoint{ep1},
			newEndpoints: []templaterouter.Endpoint{ep1, ep2},
			cmdCustomResp: map[int]string{
				2: "Some unknown enable health check error",
			},
			expectedError: `unexpected response from haproxy: Some unknown enable health check error`,
			expectedCmds: []string{
				"add server be_http:service1/ep2 10.0.1.12:8080 weight 1 check inter 5000ms",
				"set server be_http:service1/ep2 state ready",
				"enable health be_http:service1/ep1",
				"enable health be_http:service1/ep2",
			},
		},
		"scaling out to two and first server not having health check configured": {
			oldEndpoints: []templaterouter.Endpoint{ep1},
			newEndpoints: []templaterouter.Endpoint{ep1, ep2},
			cmdCustomResp: map[int]string{
				2: "Health check was not configured on this server",
				3: "1\n#\nep1\nep2",
			},
			expectedCmds: []string{
				"add server be_http:service1/ep2 10.0.1.12:8080 weight 1 check inter 5000ms",
				"set server be_http:service1/ep2 state ready",
				"enable health be_http:service1/ep1",
				"show servers state be_http:service1",
				"set server be_http:service1/ep1 state maint",
				"del server be_http:service1/ep1",
				"add server be_http:service1/ep1 10.0.1.11:8080 weight 1 check inter 5000ms",
				"set server be_http:service1/ep1 state ready",
				"enable health be_http:service1/ep2",
				"enable health be_http:service1/ep1",
			},
		},
		"scaling out to three": {
			oldEndpoints: []templaterouter.Endpoint{ep1, ep2},
			newEndpoints: []templaterouter.Endpoint{ep1, ep2, ep3},
			expectedCmds: []string{
				"add server be_http:service1/ep3 10.0.1.13:8080 weight 1 check inter 5000ms",
				"set server be_http:service1/ep3 state ready",
				"enable health be_http:service1/ep3",
			},
		},
		"scaling in to two": {
			oldEndpoints: []templaterouter.Endpoint{ep1, ep2, ep3},
			newEndpoints: []templaterouter.Endpoint{ep1, ep2},
			expectedCmds: []string{
				"set server be_http:service1/ep3 state maint",
				"del server be_http:service1/ep3",
			},
		},
		"scaling in to one": {
			oldEndpoints: []templaterouter.Endpoint{ep1, ep2},
			newEndpoints: []templaterouter.Endpoint{ep1},
			expectedCmds: []string{
				"set server be_http:service1/ep2 state maint",
				"del server be_http:service1/ep2",
				"disable health be_http:service1/ep1",
				"set server be_http:service1/ep1 health up",
			},
		},
		"scaling in to one, deleted server not found": {
			// e.g. the router state is out of sync with haproxy and the server was already removed.
			oldEndpoints: []templaterouter.Endpoint{ep1, ep2},
			newEndpoints: []templaterouter.Endpoint{ep1},
			cmdCustomResp: map[int]string{
				0: "No such server.",
			},
			expectedCmds: []string{
				"set server be_http:service1/ep2 state maint",
				"disable health be_http:service1/ep1",
				"set server be_http:service1/ep1 health up",
			},
		},
		"scaling in to one failing on transport error": {
			oldEndpoints: []templaterouter.Endpoint{ep1, ep2},
			newEndpoints: []templaterouter.Endpoint{ep1},
			cmdErrors:    map[int]error{0: errors.New("connection refused")},
			expectedCmds: []string{
				"set server be_http:service1/ep2 state maint",
			},
			expectedError: "error deleting backend server ep2: connection refused",
		},
		"scaling in to one having more than one service": {
			oldEndpoints: []templaterouter.Endpoint{ep1, ep2},
			newEndpoints: []templaterouter.Endpoint{ep1},
			// two activeEndpoints: one endpoint from this service, another one from the second service,
			// so health check shouldn't be disabled.
			activeEndpoints: 2,
			expectedCmds: []string{
				"set server be_http:service1/ep2 state maint",
				"del server be_http:service1/ep2",
			},
		},
		"changing weight": {
			oldEndpoints: []templaterouter.Endpoint{ep1, ep2},
			newEndpoints: []templaterouter.Endpoint{ep1, ep2weight2},
			expectedCmds: []string{
				"set server be_http:service1/ep2 weight 2",
			},
		},
		"changing backend protocol": {
			oldEndpoints: []templaterouter.Endpoint{ep1, ep2},
			newEndpoints: []templaterouter.Endpoint{ep1, ep2h2},
			cmdCustomResp: map[int]string{
				0: "1\n#\nep1\nep2",
			},
			expectedCmds: []string{
				"show servers state be_http:service1",
				"set server be_http:service1/ep2 state maint",
				"del server be_http:service1/ep2",
				"add server be_http:service1/ep2 10.0.1.12:8080 weight 1 proto h2 check inter 5000ms",
				"set server be_http:service1/ep2 state ready",
				"enable health be_http:service1/ep2",
			},
		},
	}

	for name, test := range testCases {
		t.Run(name, func(t *testing.T) {
			if test.activeEndpoints == 0 {
				test.activeEndpoints = len(test.newEndpoints)
			}
			require.GreaterOrEqual(t, test.activeEndpoints, len(test.newEndpoints), "activeEndpoints should be 0 (zero) or >=len(newEndpoints)")

			client := &fakeClient{cmdCustomResp: test.cmdCustomResp, cmdErrors: test.cmdErrors}
			cm, _, _ := newTestConfigManager(t, client, 0)
			err := cm.ReplaceRouteEndpoints(testRouteID, &templaterouter.ServiceUnit{}, test.oldEndpoints, test.newEndpoints, test.activeEndpoints)
			if test.expectedError == "" {
				assert.NoError(t, err)
			} else {
				assert.EqualError(t, err, strings.TrimSpace(test.expectedError))
			}
			assert.Equal(t, test.expectedCmds, client.executedCmds)
		})
	}
}

func TestRemoveRouteEndpoints(t *testing.T) {
	testCases := map[string]struct {
		endpoints     []templaterouter.Endpoint
		cmdCustomResp map[int]string
		expectedCmds  []string
		expectedError string
	}{
		"single endpoint": {
			endpoints: []templaterouter.Endpoint{{ID: "ep1"}},
			expectedCmds: []string{
				"set server be_http:service1/ep1 state maint",
				"del server be_http:service1/ep1",
			},
		},
		"two endpoints failing first": {
			endpoints: []templaterouter.Endpoint{{ID: "ep1"}, {ID: "ep2"}},
			cmdCustomResp: map[int]string{
				0: "some error setting maintenance",
			},
			expectedCmds: []string{
				"set server be_http:service1/ep1 state maint",
				"set server be_http:service1/ep2 state maint",
				"del server be_http:service1/ep2",
			},
			expectedError: `error deleting server ep1: unexpected response from haproxy: some error setting maintenance`,
		},
		"two endpoints failing both": {
			endpoints: []templaterouter.Endpoint{{ID: "ep1"}, {ID: "ep2"}},
			cmdCustomResp: map[int]string{
				0: "some error setting maintenance on endpoint1",
				1: "some error setting maintenance on endpoint2",
			},
			expectedCmds: []string{
				"set server be_http:service1/ep1 state maint",
				"set server be_http:service1/ep2 state maint",
			},
			expectedError: `
error deleting server ep1: unexpected response from haproxy: some error setting maintenance on endpoint1
error deleting server ep2: unexpected response from haproxy: some error setting maintenance on endpoint2
`,
		},
		"two endpoints failing first to delete": {
			endpoints: []templaterouter.Endpoint{{ID: "ep1"}, {ID: "ep2"}},
			cmdCustomResp: map[int]string{
				1: "some error deleting server", // failing the first delete
			},
			expectedCmds: []string{
				"set server be_http:service1/ep1 state maint",
				"del server be_http:service1/ep1",
				"set server be_http:service1/ep2 state maint",
				"del server be_http:service1/ep2",
			},
			expectedError: "", // should have no error
		},
	}

	for name, test := range testCases {
		t.Run(name, func(t *testing.T) {
			client := &fakeClient{cmdCustomResp: test.cmdCustomResp}
			cm, _, _ := newTestConfigManager(t, client, 0)
			err := cm.RemoveRouteEndpoints(testRouteID, test.endpoints)
			if test.expectedError == "" {
				require.NoError(t, err)
			} else {
				require.EqualError(t, err, strings.TrimSpace(test.expectedError))
			}
			require.Equal(t, test.expectedCmds, client.executedCmds)
		})
	}
}

func TestRemoveRoute(t *testing.T) {
	ep1 := templaterouter.Endpoint{ID: "ep1"}
	ep2 := templaterouter.Endpoint{ID: "ep2"}
	testCases := map[string]struct {
		endpoints     []templaterouter.Endpoint
		cmdCustomResp map[int]string
		expectedCmds  []string
		expectedError string
	}{
		"no endpoints": {},
		"two endpoints": {
			endpoints: []templaterouter.Endpoint{ep1, ep2},
			expectedCmds: []string{
				"set server be_http:service1/ep1 state maint",
				"del server be_http:service1/ep1",
				"set server be_http:service1/ep2 state maint",
				"del server be_http:service1/ep2",
			},
		},
		"two endpoints, first not found": {
			endpoints: []templaterouter.Endpoint{ep1, ep2},
			cmdCustomResp: map[int]string{
				0: "No such server.",
			},
			expectedCmds: []string{
				"set server be_http:service1/ep1 state maint",
				"set server be_http:service1/ep2 state maint",
				"del server be_http:service1/ep2",
			},
		},
		"two endpoints failing both": {
			endpoints: []templaterouter.Endpoint{ep1, ep2},
			cmdCustomResp: map[int]string{
				0: "some error setting maintenance on endpoint1",
				1: "some error setting maintenance on endpoint2",
			},
			expectedCmds: []string{
				"set server be_http:service1/ep1 state maint",
				"set server be_http:service1/ep2 state maint",
			},
			expectedError: `
error deleting server ep1: unexpected response from haproxy: some error setting maintenance on endpoint1
error deleting server ep2: unexpected response from haproxy: some error setting maintenance on endpoint2
`,
		},
	}

	for name, test := range testCases {
		t.Run(name, func(t *testing.T) {
			client := &fakeClient{cmdCustomResp: test.cmdCustomResp}
			cm, _, _ := newTestConfigManager(t, client, 0)
			err := cm.RemoveRoute(testRouteID, &routev1.Route{}, test.endpoints)
			if test.expectedError == "" {
				require.NoError(t, err)
			} else {
				require.EqualError(t, err, strings.TrimSpace(test.expectedError))
			}
			require.Equal(t, test.expectedCmds, client.executedCmds)

			// The route should be unregistered, even on failures.
			require.EqualError(t, cm.RemoveRoute(testRouteID, &routev1.Route{}, nil), "route id service1 was not registered")
		})
	}
}

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
		require.NoError(t, cm.RemoveRoute(testRouteID, &routev1.Route{}, []templaterouter.Endpoint{ep1}))
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
			cm, router, timers := newTestConfigManager(t, &fakeClient{}, commitInterval)

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

// testRouteID is the id of the route registered by newTestConfigManager(). Its
// route has no TLS termination, so the haproxy backend is `be_http:service1`.
const testRouteID = templaterouter.ServiceAliasConfigKey("service1")

// newTestConfigManager returns a config manager that sends haproxy commands to client, with a
// route registered as testRouteID. Commits are sent to the returned fakeRouter, and they are
// scheduled using the returned fakeTimers, so they only happen when a test fires the timers.
func newTestConfigManager(t *testing.T, client HAProxyClient, commitInterval time.Duration) (*haproxyConfigManager, *fakeRouter, *fakeTimers) {
	router := &fakeRouter{}
	timers := &fakeTimers{}
	t.Cleanup(timers.Stop)

	cm := NewHAProxyConfigManager(templaterouter.ConfigManagerOptions{CommitInterval: commitInterval})
	cm.Initialize(router, "")
	cm.client = client
	cm.afterFunc = timers.AfterFunc
	cm.Register(testRouteID, &templaterouter.ServiceAliasConfig{}, &routev1.Route{})

	return cm, router, timers
}
