package metrics

import (
	"bufio"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"net/url"
	"time"

	"k8s.io/apiserver/pkg/server/healthz"

	"github.com/openshift/router/pkg/router/crl"
	"github.com/openshift/router/pkg/router/metrics/probehttp"
	templateplugin "github.com/openshift/router/pkg/router/template"
)

var (
	errBackend      = fmt.Errorf("backend reported failure")
	errShuttingDown = fmt.Errorf("process is terminating")
)

// ProcessRunning returns a healthz check that returns true as long as the provided
// stopCh is not closed.
func ProcessRunning(stopCh <-chan struct{}) healthz.HealthChecker {
	return healthz.NamedCheck("process-running", func(r *http.Request) error {
		select {
		case <-stopCh:
			return errShuttingDown
		default:
			return nil
		}
		return nil
	})
}

// HTTPBackendAvailable returns a healthz check that verifies a backend responds to a GET to
// the provided URL with 2xx or 3xx response.
func HTTPBackendAvailable(u *url.URL) healthz.HealthChecker {
	p := probehttp.New()
	return healthz.NamedCheck("backend-http", func(r *http.Request) error {
		result, _, err := p.Probe(u, nil, 2*time.Second)
		if err != nil {
			return err
		}
		if result != probehttp.Success {
			return errBackend
		}
		return nil
	})
}

// HasSynced returns a healthz check that verifies the router has been synced at least
// once.
// routerPtr is a pointer because it may not yet be defined (there's a chicken-and-egg problem
// with when the health checker and router object are set up).
func HasSynced(routerPtr **templateplugin.TemplatePlugin) (healthz.HealthChecker, error) {
	if routerPtr == nil {
		return nil, fmt.Errorf("Nil routerPtr passed to HasSynced")
	}

	return healthz.NamedCheck("has-synced", func(r *http.Request) error {
		if *routerPtr == nil || !(*routerPtr).Router.SyncedAtLeastOnce() {
			return fmt.Errorf("Router not synced")
		}
		return nil
	}), nil
}

func ControllerLive() healthz.HealthChecker {
	return healthz.NamedCheck("controller", func(r *http.Request) error {
		return nil
	})

}

func CRLsUpdated() healthz.HealthChecker {
	return healthz.NamedCheck("crls-updated", func(r *http.Request) error {
		if !crl.GetCRLsUpdated() {
			return fmt.Errorf("missing CRLs")
		}
		return nil
	})
}

// ProxyProtocolHTTPBackendAvailable returns a healthz check that verifies a backend supporting
// the HAProxy PROXY protocol responds to a GET to the provided URL with 2xx or 3xx response.
func ProxyProtocolHTTPBackendAvailable(u *url.URL) healthz.HealthChecker {
	dialer := &net.Dialer{
		Timeout:   2 * time.Second,
		DualStack: true,
	}
	return healthz.NamedCheck("backend-proxy-http", func(r *http.Request) error {
		conn, err := dialer.Dial("tcp", u.Host)
		if err != nil {
			return err
		}
		conn.SetDeadline(time.Now().Add(2 * time.Second))
		br := bufio.NewReader(conn)
		if _, err := conn.Write([]byte("PROXY UNKNOWN\r\n")); err != nil {
			return err
		}
		req := &http.Request{Method: "GET", URL: u, Proto: "HTTP/1.1", ProtoMajor: 1, ProtoMinor: 1}
		if err := req.Write(conn); err != nil {
			return err
		}
		res, err := http.ReadResponse(br, req)
		if err != nil {
			return err
		}

		// read full body
		defer res.Body.Close()
		if _, err := io.Copy(ioutil.Discard, res.Body); err != nil {
			log.V(4).Info("error discarding probe body contents", "error", err)
		}

		if res.StatusCode < http.StatusOK && res.StatusCode >= http.StatusBadRequest {
			log.V(4).Info("probe failed", "url", u.String(), "response", res)
			return errBackend
		}
		log.V(4).Info("probe succeeded", "url", u.String(), "response", res)
		return nil
	})
}
