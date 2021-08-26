// Package haproxy is inspired by https://github.com/prometheus/haproxy_exporter
package haproxy

import (
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"math"
	"net"
	"net/http"
	_ "net/http/pprof"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	utilruntime "k8s.io/apimachinery/pkg/util/runtime"

	logf "github.com/openshift/router/log"
)

var log = logf.Logger.WithName("metrics_haproxy")

const (
	namespace = "haproxy" // For Prometheus metrics.

	// HAProxy 1.4
	// # pxname,svname,qcur,qmax,scur,smax,slim,stot,bin,bout,dreq,dresp,ereq,econ,eresp,wretr,wredis,status,weight,act,bck,chkfail,chkdown,lastchg,downtime,qlimit,pid,iid,sid,throttle,lbtot,tracked,type,rate,rate_lim,rate_max,check_status,check_code,check_duration,hrsp_1xx,hrsp_2xx,hrsp_3xx,hrsp_4xx,hrsp_5xx,hrsp_other,hanafail,req_rate,req_rate_max,req_tot,cli_abrt,srv_abrt,
	// HAProxy 1.5
	// These columns are part of the stable API for HAProxy and are documented here: https://cbonte.github.io/haproxy-dconv/1.5/configuration.html#9.1
	// pxname,svname,qcur,qmax,scur,smax,slim,stot,bin,bout,dreq,dresp,ereq,econ,eresp,wretr,wredis,status,weight,act,bck,chkfail,chkdown,lastchg,downtime,qlimit,pid,iid,sid,throttle,lbtot,tracked,type,rate,rate_lim,rate_max,check_status,check_code,check_duration,hrsp_1xx,hrsp_2xx,hrsp_3xx,hrsp_4xx,hrsp_5xx,hrsp_other,hanafail,req_rate,req_rate_max,req_tot,cli_abrt,srv_abrt,comp_in,comp_out,comp_byp,comp_rsp,lastsess,last_chk,last_agt,qtime,ctime,rtime,ttime
	expectedCsvFieldCount = 52
	statusField           = 17

	frontendType = "0"
	backendType  = "1"
	serverType   = "2"
	listenerType = "3"
)

var (
	frontendLabelNames = []string{"frontend"}
	backendLabelNames  = []string{"backend", "namespace", "route"}
	serverLabelNames   = []string{"server", "namespace", "route", "pod", "service"}
)

func newFrontendMetric(metricName string, docString string, constLabels prometheus.Labels) *prometheus.GaugeVec {
	return prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace:   namespace,
			Name:        "frontend_" + metricName,
			Help:        docString,
			ConstLabels: constLabels,
		},
		frontendLabelNames,
	)
}

func newBackendMetric(metricName string, docString string, constLabels prometheus.Labels) *prometheus.GaugeVec {
	return prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace:   namespace,
			Name:        "backend_" + metricName,
			Help:        docString,
			ConstLabels: constLabels,
		},
		backendLabelNames,
	)
}

func newServerMetric(metricName string, docString string, constLabels prometheus.Labels) *prometheus.GaugeVec {
	return prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace:   namespace,
			Name:        "server_" + metricName,
			Help:        docString,
			ConstLabels: constLabels,
		},
		serverLabelNames,
	)
}

type metrics map[int]*prometheus.GaugeVec

func (m metrics) Names() []int {
	keys := make([]int, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Ints(keys)
	return keys
}

type metricID struct {
	proxyType  string
	proxyName  string
	serverName string
}

// counterValuesByMetric is used to track values across reloads
type counterValuesByMetric map[metricID][]int64

// defaultSelectedMetrics is the list of metrics included by default. These metrics are a subset
// of the metrics exposed by haproxy_exporter by default for performance reasons.
var defaultSelectedMetrics = []int{2, 4, 5, 7, 8, 9, 13, 14, 17, 21, 24, 33, 35, 39, 40, 41, 42, 43, 44, 58, 59, 60, 79, 85}

// defaultCounterMetrics is the list of metrics that are counters and should be preserved across
// restarts. Only add metrics to this list if they are a counter.
var defaultCounterMetrics = []int{7, 8, 9, 13, 14, 21, 24, 39, 40, 41, 42, 43, 44, 79, 85}

// Exporter collects HAProxy stats from the given URI and exports them using
// the prometheus metrics package.
type Exporter struct {
	opts  PrometheusOptions
	mutex sync.RWMutex
	fetch func() (io.ReadCloser, error)

	// lastScrape is the time the last scrape was invoked if at all
	lastScrape *time.Time
	// scrapeInterval is a calculated value based on the number of rows returned by HAProxy
	scrapeInterval time.Duration

	// serverLimited is true when above opts.ServerThreshold, and no server metrics will be
	// reported. Instead, the full set of backend metrics will be reported instead.
	serverLimited bool
	// reducedBackendExports is the list of metrics that are not redundant with servers - when
	// server metrics are being reported only these backendExports are shown.
	reducedBackendExports map[int]struct{}

	up, nextScrapeInterval                         prometheus.Gauge
	totalScrapes, csvParseFailures                 prometheus.Counter
	serverThresholdCurrent, serverThresholdLimit   prometheus.Gauge
	frontendMetrics, backendMetrics, serverMetrics map[int]*prometheus.GaugeVec

	// counterValues is added to the value specific haproxy frontend, backend, or server counter
	// metrics. This allows metrics to be tracked across restarts. This map is updated whenever CollectNow
	// is invoked.
	counterValues counterValuesByMetric
	// counterIndices records the index in the packed array of metrics (under counterValuesByMetric).
	// A 0 indicates the field index is not a counter, a positive integer is the index in the packed
	// array. The first element in the packed array is always zero to avoid having to do index math.
	// Example: Given counter fields 2, 4, and 5, the counterIndices array should be:
	//          []byte{0, 0, 1, 0, 2, 3}
	//          indicating that position 1 in the packed array is where counter field 2 is stored.
	counterIndices []byte
	// counterIndexSize the number of counters for each remembered counterValues
	counterIndexSize int
}

// NewExporter returns an initialized Exporter. baseScrapeInterval is how often to scrape per 1000 entries
// (servers, backends, and frontends).
func NewExporter(opts PrometheusOptions) (*Exporter, error) {
	u, err := url.Parse(opts.ScrapeURI)
	if err != nil {
		return nil, err
	}

	var fetch func() (io.ReadCloser, error)
	switch u.Scheme {
	case "http", "https", "file":
		fetch = fetchHTTP(opts.ScrapeURI, opts.Timeout)
	case "unix":
		fetch = fetchUnix(u, opts.Timeout)
	default:
		return nil, fmt.Errorf("unsupported scheme: %q", u.Scheme)
	}

	// use a compact table representation of previous counter values
	if len(defaultCounterMetrics) >= math.MaxUint8 {
		return nil, fmt.Errorf("too many default counter metrics")
	}
	sort.Ints(defaultCounterMetrics)
	var counterIndexSize int
	counterIndices := make([]byte, defaultCounterMetrics[len(defaultCounterMetrics)-1]+1)
	for _, m := range defaultCounterMetrics {
		counterIndexSize++
		counterIndices[m] = byte(counterIndexSize)
	}

	return &Exporter{
		opts:  opts,
		fetch: fetch,
		up: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "up",
			Help:      "Was the last scrape of haproxy successful.",
		}),
		totalScrapes: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "exporter_total_scrapes",
			Help:      "Current total HAProxy scrapes.",
		}),
		serverThresholdCurrent: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace:   namespace,
			Name:        "exporter_server_threshold",
			Help:        "Number of servers tracked and the current threshold value.",
			ConstLabels: prometheus.Labels{"type": "current"},
		}),
		serverThresholdLimit: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace:   namespace,
			Name:        "exporter_server_threshold",
			Help:        "Number of servers tracked and the current threshold value.",
			ConstLabels: prometheus.Labels{"type": "limit"},
		}),
		nextScrapeInterval: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "exporter_scrape_interval",
			Help:      "The time in seconds before another scrape is allowed, proportional to size of data.",
		}),
		csvParseFailures: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "exporter_csv_parse_failures",
			Help:      "Number of errors while parsing CSV.",
		}),
		frontendMetrics: filterMetrics(opts.ExportedMetrics, metrics{
			4:  newFrontendMetric("current_sessions", "Current number of active sessions.", nil),
			5:  newFrontendMetric("max_sessions", "Maximum observed number of active sessions.", nil),
			6:  newFrontendMetric("limit_sessions", "Configured session limit.", nil),
			8:  newFrontendMetric("bytes_in_total", "Current total of incoming bytes.", nil),
			9:  newFrontendMetric("bytes_out_total", "Current total of outgoing bytes.", nil),
			10: newFrontendMetric("requests_denied_total", "Total of requests denied for security.", nil),
			12: newFrontendMetric("request_errors_total", "Total of request errors.", nil),
			33: newFrontendMetric("current_session_rate", "Current number of sessions per second over last elapsed second.", nil),
			34: newFrontendMetric("limit_session_rate", "Configured limit on new sessions per second.", nil),
			35: newFrontendMetric("max_session_rate", "Maximum observed number of sessions per second.", nil),
			39: newFrontendMetric("http_responses_total", "Total of HTTP responses.", prometheus.Labels{"code": "1xx"}),
			40: newFrontendMetric("http_responses_total", "Total of HTTP responses.", prometheus.Labels{"code": "2xx"}),
			41: newFrontendMetric("http_responses_total", "Total of HTTP responses.", prometheus.Labels{"code": "3xx"}),
			42: newFrontendMetric("http_responses_total", "Total of HTTP responses.", prometheus.Labels{"code": "4xx"}),
			43: newFrontendMetric("http_responses_total", "Total of HTTP responses.", prometheus.Labels{"code": "5xx"}),
			44: newFrontendMetric("http_responses_total", "Total of HTTP responses.", prometheus.Labels{"code": "other"}),
			48: newFrontendMetric("http_requests_total", "Total HTTP requests.", nil),
			79: newFrontendMetric("connections_total", "Total number of connections.", nil),
		}),
		reducedBackendExports: map[int]struct{}{2: {}, 3: {}, 7: {}, 17: {}},
		backendMetrics: filterMetrics(opts.ExportedMetrics, metrics{
			2:  newBackendMetric("current_queue", "Current number of queued requests not assigned to any server.", nil),
			3:  newBackendMetric("max_queue", "Maximum observed number of queued requests not assigned to any server.", nil),
			4:  newBackendMetric("current_sessions", "Current number of active sessions.", nil),
			5:  newBackendMetric("max_sessions", "Maximum observed number of active sessions.", nil),
			6:  newBackendMetric("limit_sessions", "Configured session limit.", nil),
			7:  newBackendMetric("connections_total", "Total number of connections.", nil),
			8:  newBackendMetric("bytes_in_total", "Current total of incoming bytes.", nil),
			9:  newBackendMetric("bytes_out_total", "Current total of outgoing bytes.", nil),
			13: newBackendMetric("connection_errors_total", "Total of connection errors.", nil),
			14: newBackendMetric("response_errors_total", "Total of response errors.", nil),
			15: newBackendMetric("retry_warnings_total", "Total of retry warnings.", nil),
			16: newBackendMetric("redispatch_warnings_total", "Total of redispatch warnings.", nil),
			17: newBackendMetric("up", "Current health status of the backend (1 = UP, 0 = DOWN).", nil),
			18: newBackendMetric("weight", "Total weight of the servers in the backend.", nil),
			33: newBackendMetric("current_session_rate", "Current number of sessions per second over last elapsed second.", nil),
			35: newBackendMetric("max_session_rate", "Maximum number of sessions per second.", nil),
			39: newBackendMetric("http_responses_total", "Total of HTTP responses.", prometheus.Labels{"code": "1xx"}),
			40: newBackendMetric("http_responses_total", "Total of HTTP responses.", prometheus.Labels{"code": "2xx"}),
			41: newBackendMetric("http_responses_total", "Total of HTTP responses.", prometheus.Labels{"code": "3xx"}),
			42: newBackendMetric("http_responses_total", "Total of HTTP responses.", prometheus.Labels{"code": "4xx"}),
			43: newBackendMetric("http_responses_total", "Total of HTTP responses.", prometheus.Labels{"code": "5xx"}),
			44: newBackendMetric("http_responses_total", "Total of HTTP responses.", prometheus.Labels{"code": "other"}),
			58: newBackendMetric("http_average_queue_latency_milliseconds", "Average latency to be dequeued of the last 1024 requests in milliseconds.", nil),
			59: newBackendMetric("http_average_connect_latency_milliseconds", "Average connect latency of the last 1024 requests in milliseconds.", nil),
			60: newBackendMetric("http_average_response_latency_milliseconds", "Average response latency of the last 1024 requests in milliseconds.", nil),
			85: newBackendMetric("connections_reused_total", "Total number of connections reused.", nil),
		}),
		serverMetrics: filterMetrics(opts.ExportedMetrics, metrics{
			2:  newServerMetric("current_queue", "Current number of queued requests assigned to this server.", nil),
			3:  newServerMetric("max_queue", "Maximum observed number of queued requests assigned to this server.", nil),
			4:  newServerMetric("current_sessions", "Current number of active sessions.", nil),
			5:  newServerMetric("max_sessions", "Maximum observed number of active sessions.", nil),
			6:  newServerMetric("limit_sessions", "Configured session limit.", nil),
			7:  newServerMetric("connections_total", "Total number of connections.", nil),
			8:  newServerMetric("bytes_in_total", "Current total of incoming bytes.", nil),
			9:  newServerMetric("bytes_out_total", "Current total of outgoing bytes.", nil),
			13: newServerMetric("connection_errors_total", "Total of connection errors.", nil),
			14: newServerMetric("response_errors_total", "Total of response errors.", nil),
			15: newServerMetric("retry_warnings_total", "Total of retry warnings.", nil),
			16: newServerMetric("redispatch_warnings_total", "Total of redispatch warnings.", nil),
			17: newServerMetric("up", "Current health status of the server (1 = UP, 0 = DOWN).", nil),
			18: newServerMetric("weight", "Current weight of the server.", nil),
			21: newServerMetric("check_failures_total", "Total number of failed health checks.", nil),
			24: newServerMetric("downtime_seconds_total", "Total downtime in seconds.", nil),
			33: newServerMetric("current_session_rate", "Current number of sessions per second over last elapsed second.", nil),
			35: newServerMetric("max_session_rate", "Maximum observed number of sessions per second.", nil),
			38: newServerMetric("check_duration_milliseconds", "Previously run health check duration, in milliseconds", nil),
			39: newServerMetric("http_responses_total", "Total of HTTP responses.", prometheus.Labels{"code": "1xx"}),
			40: newServerMetric("http_responses_total", "Total of HTTP responses.", prometheus.Labels{"code": "2xx"}),
			41: newServerMetric("http_responses_total", "Total of HTTP responses.", prometheus.Labels{"code": "3xx"}),
			42: newServerMetric("http_responses_total", "Total of HTTP responses.", prometheus.Labels{"code": "4xx"}),
			43: newServerMetric("http_responses_total", "Total of HTTP responses.", prometheus.Labels{"code": "5xx"}),
			44: newServerMetric("http_responses_total", "Total of HTTP responses.", prometheus.Labels{"code": "other"}),
			58: newServerMetric("http_average_queue_latency_milliseconds", "Average latency to be dequeued of the last 1024 requests in milliseconds.", nil),
			59: newServerMetric("http_average_connect_latency_milliseconds", "Average connect latency of the last 1024 requests in milliseconds.", nil),
			60: newServerMetric("http_average_response_latency_milliseconds", "Average response latency of the last 1024 requests in milliseconds.", nil),
			85: newServerMetric("connections_reused_total", "Total number of connections reused.", nil),
		}),
		counterIndices:   counterIndices,
		counterIndexSize: counterIndexSize + 1,
	}, nil
}

// Describe describes all the metrics ever exported by the HAProxy exporter. It
// implements prometheus.Collector.
func (e *Exporter) Describe(ch chan<- *prometheus.Desc) {
	for _, m := range e.frontendMetrics {
		m.Describe(ch)
	}
	for _, m := range e.backendMetrics {
		m.Describe(ch)
	}
	for _, m := range e.serverMetrics {
		m.Describe(ch)
	}
	ch <- e.up.Desc()
	ch <- e.totalScrapes.Desc()
	ch <- e.nextScrapeInterval.Desc()
	ch <- e.serverThresholdCurrent.Desc()
	ch <- e.serverThresholdLimit.Desc()
	ch <- e.csvParseFailures.Desc()
}

// Collect fetches the stats from configured HAProxy location and delivers them
// as Prometheus metrics. It implements prometheus.Collector. It will not collect
// metrics more often than interval.
func (e *Exporter) Collect(ch chan<- prometheus.Metric) {
	e.mutex.Lock()
	defer e.mutex.Unlock()

	now := time.Now()
	switch {
	case e.lastScrape != nil && e.lastScrape.Add(e.scrapeInterval).After(now):
		// do nothing, return the most recently scraped metrics
		log.V(6).Info("will not scrape HAProxy metrics more often than interval", "interval", e.scrapeInterval)
	default:
		e.lastScrape = &now
		e.resetMetrics()
		e.scrape(false)
	}

	ch <- e.up
	ch <- e.totalScrapes
	ch <- e.nextScrapeInterval
	ch <- e.serverThresholdCurrent
	ch <- e.serverThresholdLimit
	ch <- e.csvParseFailures
	e.collectMetrics(ch)
}

// CollectNow performs a collection immediately. The next Collect() will report the metrics.
func (e *Exporter) CollectNow() {
	e.mutex.Lock()
	defer e.mutex.Unlock()

	now := time.Now()

	e.resetMetrics()
	e.scrape(true)
	e.lastScrape = &now
}

func fetchHTTP(uri string, timeout time.Duration) func() (io.ReadCloser, error) {
	client := http.Client{
		Timeout: timeout,
	}

	return func() (io.ReadCloser, error) {
		resp, err := client.Get(uri)
		if err != nil {
			return nil, err
		}
		if !(resp.StatusCode >= 200 && resp.StatusCode < 300) {
			resp.Body.Close()
			return nil, fmt.Errorf("HTTP status %d", resp.StatusCode)
		}
		return resp.Body, nil
	}
}

func fetchUnix(u *url.URL, timeout time.Duration) func() (io.ReadCloser, error) {
	return func() (io.ReadCloser, error) {
		f, err := net.DialTimeout("unix", u.Path, timeout)
		if err != nil {
			return nil, err
		}
		if err := f.SetDeadline(time.Now().Add(timeout)); err != nil {
			f.Close()
			return nil, err
		}
		cmd := "show stat\n"
		n, err := io.WriteString(f, cmd)
		if err != nil {
			f.Close()
			return nil, err
		}
		if n != len(cmd) {
			f.Close()
			return nil, errors.New("write error")
		}
		return f, nil
	}
}

func (e *Exporter) scrape(record bool) {
	e.totalScrapes.Inc()

	var updatedValues counterValuesByMetric
	if record {
		updatedValues = make(counterValuesByMetric)
	}

	body, err := e.fetch()
	if err != nil {
		e.up.Set(0)
		utilruntime.HandleError(fmt.Errorf("can't scrape HAProxy: %v", err))
		return
	}
	defer body.Close()
	e.up.Set(1)

	reader := csv.NewReader(body)
	reader.TrailingComma = true
	reader.Comment = '#'

	rows, servers := 0, 0
loop:
	for {
		row, err := reader.Read()
		switch err {
		case nil:
		case io.EOF:
			break loop
		default:
			if _, ok := err.(*csv.ParseError); ok {
				utilruntime.HandleError(fmt.Errorf("can't read CSV: %v", err))
				e.csvParseFailures.Inc()
				continue loop
			}
			utilruntime.HandleError(fmt.Errorf("unexpected error while reading CSV: %v", err))
			e.up.Set(0)
			break loop
		}

		// consider ourselves broken, and refuse to parse anything else
		if len(row) < expectedCsvFieldCount {
			utilruntime.HandleError(fmt.Errorf("wrong CSV field count in metrics row %d: %d vs. %d", rows, len(row), expectedCsvFieldCount))
			e.csvParseFailures.Inc()
			return
		}

		if rows == 0 && len(e.counterIndices) < len(row) {
			e.counterIndices = append(e.counterIndices, make([]byte, len(row)-len(e.counterIndices))...)
		}

		// If we exceed the server threshold, ignore the rest of the servers because we will be
		// displaying only backends and frontends.
		if row[32] == serverType {
			servers++
			if servers > e.opts.ServerThreshold {
				continue
			}
		}

		rows++
		e.parseRow(row, updatedValues)
	}

	// swap the counter values
	if record {
		e.counterValues = updatedValues
	}

	e.serverLimited = servers > e.opts.ServerThreshold
	e.serverThresholdCurrent.Set(float64(servers))
	e.serverThresholdLimit.Set(float64(e.opts.ServerThreshold))

	e.scrapeInterval = time.Duration(((float32(rows) / 1000) + 1) * float32(e.opts.BaseScrapeInterval))
	e.nextScrapeInterval.Set(float64(e.scrapeInterval / time.Second))
}

func (e *Exporter) resetMetrics() {
	for _, m := range e.frontendMetrics {
		m.Reset()
	}
	for _, m := range e.backendMetrics {
		m.Reset()
	}
	for _, m := range e.serverMetrics {
		m.Reset()
	}
}

func (e *Exporter) collectMetrics(metrics chan<- prometheus.Metric) {
	for _, m := range e.frontendMetrics {
		m.Collect(metrics)
	}
	for _, m := range e.backendMetrics {
		m.Collect(metrics)
	}
	if !e.serverLimited {
		for _, m := range e.serverMetrics {
			m.Collect(metrics)
		}
	}
}

// parseRow identifies which metrics to capture for a given row based on type and the value of pxname and svname. If the
// proxy and server names match our conventions they are labelled to the given route, service, or pod - if they don't match
// then a generic label set is applied. If updatedValues is non-nil then the map will be populated with the updated counter state
// for each metric.
func (e *Exporter) parseRow(csvRow []string, updatedValues counterValuesByMetric) {
	pxname, svname, typ := csvRow[0], csvRow[1], csvRow[32]

	switch typ {
	case frontendType:
		e.exportAndRecordRow(e.frontendMetrics, metricID{proxyType: serverType, proxyName: pxname}, updatedValues, csvRow, pxname)
	case backendType:
		if mode, value, ok := knownBackendSegment(pxname); ok {
			if namespace, name, ok := parseNameSegment(value); ok {
				e.exportAndRecordRow(e.backendMetrics, metricID{proxyType: serverType, proxyName: pxname}, updatedValues, csvRow, mode, namespace, name)
				return
			}
		}
		e.exportAndRecordRow(e.backendMetrics, metricID{proxyType: serverType, proxyName: pxname}, updatedValues, csvRow, "other/"+pxname, "", "")
	case serverType:
		pod, service, server, _ := knownServerSegment(svname)

		if _, value, ok := knownBackendSegment(pxname); ok {
			if namespace, name, ok := parseNameSegment(value); ok {
				e.exportAndRecordRow(e.serverMetrics, metricID{serverType, pxname, svname}, updatedValues, csvRow, server, namespace, name, pod, service)
				return
			}
		}
		e.exportAndRecordRow(e.serverMetrics, metricID{proxyType: serverType, serverName: svname}, updatedValues, csvRow, server, "", "", pod, service)
	}
}

// knownServerSegment takes a server name that has a known prefix and returns
// the pod, service, and simpler service name label for that type. If the prefix does not
// match false is returned.
func knownServerSegment(value string) (string, string, string, bool) {
	if i := strings.Index(value, ":"); i != -1 {
		switch value[:i] {
		case "ept":
			if service, remainder, ok := parseNameSegment(value[i+1:]); ok {
				if _, server, ok := parseNameSegment(remainder); ok {
					return "", service, server, true
				}
			}
		case "pod":
			if pod, remainder, ok := parseNameSegment(value[i+1:]); ok {
				if service, remainder, ok := parseNameSegment(remainder); ok {
					if _, server, ok := parseNameSegment(remainder); ok {
						return pod, service, server, true
					}
				}
			}
		}
	}
	return "", "", value, false
}

// knownBackendSegment takes a backend name that has a known prefix and returns
// the preferred metric label for that type and the remainder. If the prefix does not
// match false is returned.
func knownBackendSegment(value string) (string, string, bool) {
	if i := strings.Index(value, ":"); i != -1 {
		switch value[:i] {
		case "be_http":
			return "http", value[i+1:], true
		case "be_edge_http":
			return "https-edge", value[i+1:], true
		case "be_secure":
			return "https", value[i+1:], true
		case "be_tcp":
			return "tcp", value[i+1:], true
		}
	}
	return "", "", false
}

// parseNameSegment splits a name by the first colon and returns both segments. If
// no colon is found it returns false.
func parseNameSegment(value string) (string, string, bool) {
	i := strings.Index(value, ":")
	if i == -1 {
		return "", "", false
	}
	return value[:i], value[i+1:], true
}

func parseStatusField(value string) int64 {
	switch value {
	case "UP", "UP 1/3", "UP 2/3", "OPEN", "no check":
		return 1
	case "DOWN", "DOWN 1/2", "NOLB", "MAINT":
		return 0
	}
	return 0
}

// exportAndRecordRow parses the provided csvRow labels for the specified metrics and then updates their value given labels. If updatedValues is
// non-nil the current value of the metric will be written back to rowID. This allows baseline values to be recorded and used across restarts of
// HAProxy.
func (e *Exporter) exportAndRecordRow(metrics metrics, rowID metricID, updatedValues counterValuesByMetric, csvRow []string, labels ...string) {
	var updatedBaseValues []int64
	baseValues := e.counterValues[rowID]
	if updatedValues != nil {
		updatedBaseValues = baseValues
		if updatedBaseValues == nil {
			updatedBaseValues = make([]int64, e.counterIndexSize)
		}
		updatedValues[rowID] = updatedBaseValues
	}

	exportCSVFields(e.csvParseFailures, metrics, baseValues, updatedBaseValues, e.counterIndices, csvRow, labels)
}

// exportCSVFields iterates over the returned CSV values and sets the appropriate metric in the map. Empty values or parse errors result in
// no metric being scraped. If updatedBaseValues is not nil then it is updated with the latest value of the metric. If baseValues is nil
// or the field is not a counter (according to counterIndices) then no value is incremented
func exportCSVFields(csvParseFailures prometheus.Counter, metrics metrics, baseValues, updatedBaseValues []int64, counterIndices []byte, csvRow []string, labels []string) {
	for fieldIdx, metric := range metrics {
		valueStr := csvRow[fieldIdx]
		if valueStr == "" {
			continue
		}

		// the stored position of the previous value
		storedIdx := counterIndices[fieldIdx]

		var value int64
		switch fieldIdx {
		case statusField:
			value = parseStatusField(valueStr)
		default:
			var err error
			value, err = strconv.ParseInt(valueStr, 10, 64)
			if err != nil {
				utilruntime.HandleError(fmt.Errorf("can't parse CSV field value %s: %v", valueStr, err))
				csvParseFailures.Inc()
				continue
			}
			if baseValues != nil && storedIdx > 0 {
				value += baseValues[storedIdx]
			}
		}
		if updatedBaseValues != nil && storedIdx > 0 {
			updatedBaseValues[storedIdx] = value
		}

		metric.WithLabelValues(labels...).Set(float64(value))
	}
}

// filterMetrics returns the set of server metrics specified by the export array.
func filterMetrics(export []int, available metrics) metrics {
	metrics := map[int]*prometheus.GaugeVec{}
	if len(export) == 0 {
		return metrics
	}

	selected := map[int]struct{}{}
	for _, f := range export {
		selected[f] = struct{}{}
	}

	for field, metric := range available {
		if _, ok := selected[field]; ok {
			metrics[field] = metric
		}
	}
	return metrics
}

type PrometheusOptions struct {
	// ScrapeURI is the URI to access HAProxy under. Defaults to the unix domain socket.
	ScrapeURI string
	// PidFile is optional and will collect process metrics.
	PidFile string
	// Timeout is the maximum interval to wait for stats.
	Timeout time.Duration
	// BaseScrapeInterval is the minimum time to wait between stat calls per 1000 servers.
	BaseScrapeInterval time.Duration
	// ServerThreshold is the maximum number of servers that can be reported before switching
	// to only using backend metrics. This reduces metrics load when there is a very large set
	// of endpoints.
	ServerThreshold int
	// ExportedMetrics is a list of HAProxy stats to export.
	ExportedMetrics []int
}

// NewPrometheusCollector starts collectors for prometheus metrics from the
// provided HAProxy stats port. It will start goroutines. Use the default
// prometheus handler to access these metrics.
func NewPrometheusCollector(opts PrometheusOptions) (*Exporter, error) {
	opts = defaultOptions(opts)
	exporter, err := NewExporter(opts)
	if err != nil {
		return nil, err
	}
	if err := prometheus.Register(exporter); err != nil {
		return nil, err
	}

	// TODO: register a version collector?

	if len(opts.PidFile) > 0 {
		procExporter := prometheus.NewProcessCollector(prometheus.ProcessCollectorOpts{
			Namespace: namespace,
			PidFn: func() (int, error) {
				content, err := ioutil.ReadFile(opts.PidFile)
				if err != nil {
					return 0, fmt.Errorf("can't read haproxy pid file: %s", err)
				}
				value, err := strconv.Atoi(strings.TrimSpace(string(content)))
				if err != nil {
					return 0, fmt.Errorf("can't parse haproxy pid file: %s", err)
				}
				return value, nil
			},
		})
		if err := prometheus.Register(procExporter); err != nil {
			return nil, err
		}
	}

	return exporter, nil
}

func defaultOptions(opts PrometheusOptions) PrometheusOptions {
	if len(opts.ScrapeURI) == 0 {
		opts.ScrapeURI = "unix:///var/lib/haproxy/run/haproxy.sock"
	}
	if len(opts.PidFile) == 0 {
		opts.PidFile = "/var/lib/haproxy/run/haproxy.pid"
	}
	if opts.Timeout == 0 {
		opts.Timeout = 5 * time.Second
	}
	if opts.BaseScrapeInterval == 0 {
		opts.BaseScrapeInterval = 5 * time.Second
	}
	if len(opts.ExportedMetrics) == 0 {
		opts.ExportedMetrics = defaultSelectedMetrics
	}
	if opts.ServerThreshold == 0 {
		opts.ServerThreshold = 500
	}
	return opts
}
