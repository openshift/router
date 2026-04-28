package haproxy

import (
	"testing"

	routev1 "github.com/openshift/api/route/v1"
	templaterouter "github.com/openshift/router/pkg/router/template"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/utils/ptr"
)

func TestBackendDynamicUpdate(t *testing.T) {
	type cmd string
	const (
		cmdAdd           cmd = "add"
		cmdDel           cmd = "del"
		cmdUpdate        cmd = "update"
		cmdEnableHealth  cmd = "enable-health"
		cmdDisableHealth cmd = "disable-health"
	)

	testCases := map[string]struct {
		cmd             cmd
		backendName     *templaterouter.ServiceAliasConfigKey // default: "route1"
		endpointID      *string                               // default: "server1"
		ip              *string                               // default: "10.0.1.11"
		port            *string                               // default: "9000"
		weight          *int32                                // default: 1
		workingDir      *string                               // default: "tmp"
		publicHostname  string
		serviceHostname string
		tlsTermination  routev1.TLSTerminationType
		verifyHostname  bool
		appProtocol     string
		certificates    map[string]templaterouter.Certificate
		annotations     map[string]string
		envvars         []string
		defaultCA       string
		cmdCustomResp   []string // 1:1 to `cmdExpected`, trailing empty items can be omited.
		errExpected     string
		removedExpected bool
		cmdExpected     []string
	}{
		//
		// adding
		"should add insecure server": {
			cmd: cmdAdd,
			cmdExpected: []string{
				"add server route1/server1 10.0.1.11:9000 weight 1 check inter 5000ms",
				"set server route1/server1 state ready",
			},
		},
		"should add insecure server with zero weight": {
			cmd:    cmdAdd,
			weight: ptr.To[int32](0),
			cmdExpected: []string{
				"add server route1/server1 10.0.1.11:9000 weight 0 check inter 5000ms",
				"set server route1/server1 state drain",
			},
		},
		"should add insecure server with custom health check": {
			cmd: cmdAdd,
			annotations: map[string]string{
				"router.openshift.io/haproxy.health.check.interval": "10s",
			},
			cmdExpected: []string{
				"add server route1/server1 10.0.1.11:9000 weight 1 check inter 10s",
				"set server route1/server1 state ready",
			},
		},
		"should add insecure server with custom invalid health check": {
			cmd: cmdAdd,
			annotations: map[string]string{
				"router.openshift.io/haproxy.health.check.interval": "1z",
			},
			cmdExpected: []string{
				"add server route1/server1 10.0.1.11:9000 weight 1 check inter 5000ms",
				"set server route1/server1 state ready",
			},
		},
		"should add insecure server with custom maxconn": {
			cmd: cmdAdd,
			annotations: map[string]string{
				"haproxy.router.openshift.io/pod-concurrent-connections": "100",
			},
			cmdExpected: []string{
				"add server route1/server1 10.0.1.11:9000 weight 1 check inter 5000ms maxconn 100",
				"set server route1/server1 state ready",
			},
		},
		"should add passthrough server": {
			cmd:            cmdAdd,
			tlsTermination: routev1.TLSTerminationPassthrough,
			cmdExpected: []string{
				"add server route1/server1 10.0.1.11:9000 weight 1 check inter 5000ms",
				"set server route1/server1 state ready",
			},
		},
		"should add edge termination server": {
			cmd:            cmdAdd,
			tlsTermination: routev1.TLSTerminationEdge,
			cmdExpected: []string{
				"add server route1/server1 10.0.1.11:9000 weight 1 check inter 5000ms",
				"set server route1/server1 state ready",
			},
		},
		"should add edge termination h2 server": {
			cmd:            cmdAdd,
			tlsTermination: routev1.TLSTerminationEdge,
			appProtocol:    "h2c",
			cmdExpected: []string{
				"add server route1/server1 10.0.1.11:9000 weight 1 proto h2 check inter 5000ms",
				"set server route1/server1 state ready",
			},
		},
		"should add reencrypt termination server": {
			cmd:            cmdAdd,
			tlsTermination: routev1.TLSTerminationReencrypt,
			cmdExpected: []string{
				"add server route1/server1 10.0.1.11:9000 weight 1 ssl alpn h2,http/1.1 verify none check inter 5000ms",
				"set server route1/server1 state ready",
			},
		},
		"should add reencrypt termination server with verify host": {
			cmd:             cmdAdd,
			tlsTermination:  routev1.TLSTerminationReencrypt,
			verifyHostname:  true,
			serviceHostname: "route1.default.svc",
			cmdExpected: []string{
				"add server route1/server1 10.0.1.11:9000 weight 1 ssl alpn h2,http/1.1 verifyhost route1.default.svc verify none check inter 5000ms",
				"set server route1/server1 state ready",
			},
		},
		"should add reencrypt termination server with default ca": {
			cmd:            cmdAdd,
			tlsTermination: routev1.TLSTerminationReencrypt,
			defaultCA:      "/tmp/default-ca.pem",
			cmdExpected: []string{
				"add server route1/server1 10.0.1.11:9000 weight 1 ssl alpn h2,http/1.1 verify required ca-file /tmp/default-ca.pem check inter 5000ms",
				"set server route1/server1 state ready",
			},
		},
		"should add reencrypt termination server with custom certificate": {
			cmd:            cmdAdd,
			tlsTermination: routev1.TLSTerminationReencrypt,
			publicHostname: "route1.local",
			certificates: map[string]templaterouter.Certificate{
				"route1.local_pod": {
					ID:         "default:route1",
					Contents:   "-----BEGIN CERTIFICATE-----\nzzz\n-----END CERTIFICATE-----",
					PrivateKey: "-----BEGIN PRIVATE KEY-----\nzzz\n-----END PRIVATE KEY-----",
				},
			},
			cmdExpected: []string{
				"add server route1/server1 10.0.1.11:9000 weight 1 ssl alpn h2,http/1.1 verify required ca-file /tmp/router/cacerts/default:route1.pem check inter 5000ms",
				"set server route1/server1 state ready",
			},
		},
		"should retry adding insecure server": {
			cmd:           cmdAdd,
			cmdCustomResp: []string{"Already exists a server ..."},
			cmdExpected: []string{
				"add server route1/server1 10.0.1.11:9000 weight 1 check inter 5000ms",
				"del server route1/server1",
				"add server route1/server1 10.0.1.11:9000 weight 1 check inter 5000ms",
				"set server route1/server1 state ready",
			},
		},
		"should fail if failing to add insecure server": {
			cmd:           cmdAdd,
			cmdCustomResp: []string{"Some unknown adding error."},
			errExpected:   "unexpected response from haproxy: Some unknown adding error.",
			cmdExpected: []string{
				"add server route1/server1 10.0.1.11:9000 weight 1 check inter 5000ms",
			},
		},

		//
		// updating
		"should update server": {
			cmd:    cmdUpdate,
			weight: ptr.To[int32](10),
			cmdExpected: []string{
				"set server route1/server1 addr 10.0.1.11 port 9000",
				"set server route1/server1 weight 10",
			},
		},
		"should update passthrough server with weight 100%": {
			cmd:            cmdUpdate,
			tlsTermination: routev1.TLSTerminationPassthrough,
			weight:         ptr.To[int32](10),
			cmdExpected: []string{
				"set server route1/server1 addr 10.0.1.11 port 9000",
				"set server route1/server1 weight 100%",
			},
		},
		"should fail if failing to update server": {
			cmd:           cmdUpdate,
			cmdCustomResp: []string{"Some unknown updating error."},
			errExpected:   "unexpected response from haproxy: Some unknown updating error.",
			cmdExpected: []string{
				"set server route1/server1 addr 10.0.1.11 port 9000",
			},
		},

		//
		// health check
		"should enable health check": {
			cmd: cmdEnableHealth,
			cmdExpected: []string{
				"enable health route1/server1",
			},
		},
		"should fail if failing to enable health check": {
			cmd:           cmdEnableHealth,
			cmdCustomResp: []string{"Some unknown set health error."},
			errExpected:   "unexpected response from haproxy: Some unknown set health error.",
			cmdExpected: []string{
				"enable health route1/server1",
			},
		},
		"should disable health check": {
			cmd: cmdDisableHealth,
			cmdExpected: []string{
				"disable health route1/server1",
			},
		},
		"should fail if failing to disable health check": {
			cmd:           cmdDisableHealth,
			cmdCustomResp: []string{"Some unknown set health error."},
			errExpected:   "unexpected response from haproxy: Some unknown set health error.",
			cmdExpected: []string{
				"disable health route1/server1",
			},
		},

		//
		// Deleting
		"should delete server": {
			cmd: cmdDel,
			cmdExpected: []string{
				"set server route1/server1 state maint",
				"del server route1/server1",
			},
			removedExpected: true,
		},
		"should fail to delete if failing to disable server": {
			cmd:             cmdDel,
			cmdCustomResp:   []string{"Some unknown set server error."},
			errExpected:     "unexpected response from haproxy: Some unknown set server error.",
			removedExpected: false,
			cmdExpected: []string{
				"set server route1/server1 state maint",
			},
		},
		"should succeed delete if failing to remove and succeeding to disable": {
			cmd: cmdDel,
			cmdCustomResp: []string{
				"",                               // first cmd
				"Some unknown del server error.", // second cmd
			},
			removedExpected: false,
			cmdExpected: []string{
				"set server route1/server1 state maint",
				"del server route1/server1",
			},
		},
	}

	for name, test := range testCases {
		t.Run(name, func(t *testing.T) {
			backendName := ptr.Deref(test.backendName, "route1")
			endpointID := ptr.Deref(test.endpointID, "server1")
			ip := ptr.Deref(test.ip, "10.0.1.11")
			port := ptr.Deref(test.port, "9000")
			weight := ptr.Deref(test.weight, 1)
			workingDir := ptr.Deref(test.workingDir, "/tmp")

			cfg := &templaterouter.ServiceAliasConfig{
				TLSTermination:        test.tlsTermination,
				VerifyServiceHostname: test.verifyHostname,
				Host:                  test.publicHostname,
				Certificates:          test.certificates,
				Annotations:           test.annotations,
			}
			svc := &templaterouter.ServiceUnit{
				Hostname: test.serviceHostname,
			}
			ep := templaterouter.Endpoint{
				ID:          endpointID,
				IP:          ip,
				Port:        port,
				AppProtocol: test.appProtocol,
			}
			isPassthrough := test.tlsTermination == routev1.TLSTerminationPassthrough
			client := &fakeClient{cmdCustomResp: test.cmdCustomResp}

			b := newBackend(backendName, client)

			var removed bool
			var err error
			switch test.cmd {
			case cmdAdd:
				err = b.AddServer(cfg, svc, ep, weight, workingDir, test.defaultCA)
			case cmdDel:
				removed, err = b.DeleteServer(ep)
			case cmdUpdate:
				err = b.UpdateServer(ep, weight, isPassthrough)
			case cmdEnableHealth:
				err = b.EnableHealthCheck(ep)
			case cmdDisableHealth:
				err = b.DisableHealthCheck(ep)
			default:
				t.Errorf("invalid cmd: %s", test.cmd)
			}

			if test.errExpected != "" {
				require.EqualError(t, err, test.errExpected)
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, test.removedExpected, removed)
			assert.Equal(t, test.cmdExpected, client.executedCmds)
		})
	}

}

type fakeClient struct {
	cmdCustomResp []string
	executedCmds  []string
	respCount     int
}

func (c *fakeClient) RunCommand(cmd string, _ Converter) ([]byte, error) {
	return c.Execute(cmd)
}

func (c *fakeClient) Execute(cmd string) ([]byte, error) {
	c.executedCmds = append(c.executedCmds, cmd)

	var response string
	if len(c.cmdCustomResp) > c.respCount {
		response = c.cmdCustomResp[c.respCount] + "\n"
		c.respCount++
	}
	response += "\n"

	return []byte(response), nil
}
