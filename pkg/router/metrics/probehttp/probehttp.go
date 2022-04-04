/*
Copyright 2015 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// package probehttp is vendored from k8s.io/kubernetes/pkg/probe and
// k8s.io/kubernetes/pkg/probe/http
package probehttp

import (
	"crypto/tls"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"net/url"
	"time"

	utilnet "k8s.io/apimachinery/pkg/util/net"
	"k8s.io/client-go/pkg/version"

	logf "github.com/openshift/router/log"
)

var log = logf.Logger.WithName("metrics_probehttp")

type unixDialer struct {
	socketPath string

	net.Dialer
}

type Result string

const (
	Success Result = "success"
	Failure Result = "failure"
	Unknown Result = "unknown"
)

// overriding net.Dialer.Dial to force unix socket connection
func (d *unixDialer) Dial(_, _ string) (net.Conn, error) {
	return d.Dialer.Dial("unix", d.socketPath)
}

func New() HTTPProber {
	tlsConfig := &tls.Config{InsecureSkipVerify: true}
	return NewWithTLSConfig(tlsConfig)
}

func NewWithSocket(socketPath string) HTTPProber {
	return httpProber{&http.Transport{
		Dial:              (&unixDialer{socketPath: socketPath}).Dial,
		DisableKeepAlives: true,
	}}
}

// NewWithTLSConfig takes tls config as parameter.
func NewWithTLSConfig(config *tls.Config) HTTPProber {
	transport := utilnet.SetTransportDefaults(&http.Transport{TLSClientConfig: config, DisableKeepAlives: true})
	return httpProber{transport}
}

type HTTPProber interface {
	Probe(url *url.URL, headers http.Header, timeout time.Duration) (Result, string, error)
}

type httpProber struct {
	transport *http.Transport
}

// Probe returns a ProbeRunner capable of running an http check.
func (pr httpProber) Probe(url *url.URL, headers http.Header, timeout time.Duration) (Result, string, error) {
	return DoHTTPProbe(url, headers, &http.Client{Timeout: timeout, Transport: pr.transport})
}

type HTTPGetInterface interface {
	Do(req *http.Request) (*http.Response, error)
}

// DoHTTPProbe checks if a GET request to the url succeeds.
// If the HTTP response code is successful (i.e. 400 > code >= 200), it returns Success.
// If the HTTP response code is unsuccessful or HTTP communication fails, it returns Failure.
// This is exported because some other packages may want to do direct HTTP probes.
func DoHTTPProbe(url *url.URL, headers http.Header, client HTTPGetInterface) (Result, string, error) {
	req, err := http.NewRequest("GET", url.String(), nil)
	if err != nil {
		// Convert errors into failures to catch timeouts.
		return Failure, err.Error(), nil
	}
	if _, ok := headers["User-Agent"]; !ok {
		if headers == nil {
			headers = http.Header{}
		}
		// explicitly set User-Agent so it's not set to default Go value
		v := version.Get()
		headers.Set("User-Agent", fmt.Sprintf("router-probe/%s.%s", v.Major, v.Minor))
	}
	req.Header = headers
	if headers.Get("Host") != "" {
		req.Host = headers.Get("Host")
	}
	res, err := client.Do(req)
	if err != nil {
		// Convert errors into failures to catch timeouts.
		return Failure, err.Error(), nil
	}
	defer res.Body.Close()
	b, err := ioutil.ReadAll(res.Body)
	if err != nil {
		return Failure, "", err
	}
	body := string(b)
	if res.StatusCode >= http.StatusOK && res.StatusCode < http.StatusBadRequest {
		log.V(4).Info("probe succeeded", "url", url.String(), "response", *res)
		return Success, body, nil
	}
	log.V(4).Info("probe failed", "url", url.String(), "headers", headers, "body", body)
	return Failure, fmt.Sprintf("HTTP probe failed with statuscode: %d", res.StatusCode), nil
}
