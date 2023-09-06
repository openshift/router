package templaterouter

import (
	"bytes"
	"crypto/md5"
	"encoding/pem"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"text/template"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/prometheus/client_golang/prometheus"

	"k8s.io/apimachinery/pkg/util/sets"

	routev1 "github.com/openshift/api/route/v1"

	logf "github.com/openshift/router/log"
	"github.com/openshift/router/pkg/router/crl"
	"github.com/openshift/router/pkg/router/template/limiter"
)

var log = logf.Logger.WithName("template")

const (
	ProtocolHTTP  = "http"
	ProtocolHTTPS = "https"
	ProtocolTLS   = "tls"

	certDir         = "router/certs"
	caCertDir       = "router/cacerts"
	defaultCertName = "default"

	whitelistDir = "router/whitelists"

	caCertPostfix   = "_ca"
	destCertPostfix = "_pod"

	// '-' is not used because namespace can contain dashes
	// '_' is not used as this could be part of the name in the future
	// '/' is not safe to use in names of router config files
	routeKeySeparator = ":"
)

// templateRouter is a backend-agnostic router implementation
// that generates configuration files via a set of templates
// and manages the backend process with a reload script.
type templateRouter struct {
	// the directory to write router output to
	dir              string
	templates        map[string]*template.Template
	reloadScriptPath string
	reloadFn         func(shutdown bool) error
	reloadInterval   time.Duration
	reloadCallbacks  []func()
	state            map[ServiceAliasConfigKey]ServiceAliasConfig
	serviceUnits     map[ServiceUnitKey]ServiceUnit
	certManager      certificateManager
	// defaultCertificate is a concatenated certificate(s), their keys, and their CAs that should be used by the underlying
	// implementation as the default certificate if no certificate is resolved by the normal matching mechanisms.  This is
	// usually a wildcard certificate for a cloud domain such as *.mypaas.com to allow applications to create app.mypaas.com
	// as secure routes without having to provide their own certificates
	defaultCertificate string
	// if the default certificate is populated then this will be filled in so it can be passed to the templates
	defaultCertificatePath string
	// if the default certificate is in a secret this will be filled in so it can be passed to the templates
	defaultCertificateDir string
	// defaultDestinationCAPath is a path to a CA bundle that should be used by the underlying implementation as the default
	// destination CA if no certificate is resolved by the normal matching mechanisms. This is usually the service serving
	// certificate CA (/var/run/secrets/kubernetes.io/serviceaccount/serving_ca.crt) that the infrastructure uses to
	// generate certificates for services by name.
	defaultDestinationCAPath string
	// if the router can expose statistics it should expose them with this user for auth
	statsUser string
	// if the router can expose statistics it should expose them with this password for auth
	statsPassword string
	// if the router can expose statistics it should expose them with this port
	statsPort int
	// if the router should allow wildcard routes.
	allowWildcardRoutes bool
	// rateLimitedCommitFunction is a rate limited commit (persist state + refresh the backend)
	// function that coalesces and controls how often the router is reloaded.
	rateLimitedCommitFunction *limiter.CoalescingSerializingRateLimiter
	// lock is a mutex used to prevent concurrent router reloads.
	lock sync.Mutex
	// If true, haproxy should only bind ports when it has route and endpoint state
	bindPortsAfterSync bool
	// whether the router state has been read from the api at least once
	synced bool
	// whether a state change has occurred
	stateChanged bool
	// metricReload tracks reloads
	metricReload prometheus.Summary
	// metricReloadFailure tracks reload failures
	metricReloadFailure prometheus.Gauge
	// metricWriteConfig tracks writing config
	metricWriteConfig prometheus.Summary
	// dynamicConfigManager configures route changes dynamically on the
	// underlying router.
	dynamicConfigManager ConfigManager
	// dynamicallyConfigured indicates whether all the [state] changes
	// were also successfully applied via the dynamic config manager.
	dynamicallyConfigured bool
	// captureHTTPRequestHeaders specifies HTTP request headers
	// that should be captured for logging.
	captureHTTPRequestHeaders []CaptureHTTPHeader
	// captureHTTPResponseHeaders specifies HTTP response headers
	// that should be captured for logging.
	captureHTTPResponseHeaders []CaptureHTTPHeader
	// captureHTTPCookie specifies an HTTP cookie that should be
	// captured for logging.
	captureHTTPCookie *CaptureHTTPCookie
	// httpHeaderNameCaseAdjustments specifies HTTP header name case adjustments.
	httpHeaderNameCaseAdjustments []HTTPHeaderNameCaseAdjustment
	// haveClientCA specifies if the user provided their own CA for client auth in mTLS
	haveClientCA bool
	// haveCRLs specifies if the crl file has been generated for client auth
	haveCRLs bool
}

// templateRouterCfg holds all configuration items required to initialize the template router
type templateRouterCfg struct {
	dir                           string
	templates                     map[string]*template.Template
	reloadScriptPath              string
	reloadFn                      func(shutdown bool) error
	reloadInterval                time.Duration
	reloadCallbacks               []func()
	defaultCertificate            string
	defaultCertificatePath        string
	defaultCertificateDir         string
	defaultDestinationCAPath      string
	statsUser                     string
	statsPassword                 string
	statsPort                     int
	allowWildcardRoutes           bool
	includeUDP                    bool
	bindPortsAfterSync            bool
	dynamicConfigManager          ConfigManager
	captureHTTPRequestHeaders     []CaptureHTTPHeader
	captureHTTPResponseHeaders    []CaptureHTTPHeader
	captureHTTPCookie             *CaptureHTTPCookie
	httpHeaderNameCaseAdjustments []HTTPHeaderNameCaseAdjustment
}

// templateConfig is a subset of the templateRouter information that should be passed to the template for generating
// the correct configuration.
type templateData struct {
	// the directory that files will be written to, defaults to /var/lib/containers/router
	WorkingDir string
	// the routes
	State map[ServiceAliasConfigKey](ServiceAliasConfig)
	// the service lookup
	ServiceUnits map[ServiceUnitKey]ServiceUnit
	// full path and file name to the default certificate
	DefaultCertificate string
	// full path and file name to the default destination certificate
	DefaultDestinationCA string
	//username to expose stats with (if the template supports it)
	StatsUser string
	//password to expose stats with (if the template supports it)
	StatsPassword string
	//port to expose stats with (if the template supports it)
	StatsPort int
	// whether the router should bind the default ports
	BindPorts bool
	// The dynamic configuration manager if "configured".
	DynamicConfigManager ConfigManager
	// DisableHTTP2 on the frontend and the backend when set "true"
	DisableHTTP2 bool
	// CaptureHTTPRequestHeaders specifies HTTP request headers
	// that should be captured for logging.
	CaptureHTTPRequestHeaders []CaptureHTTPHeader
	// CaptureHTTPResponseHeaders specifies HTTP response headers
	// that should be captured for logging.
	CaptureHTTPResponseHeaders []CaptureHTTPHeader
	// CaptureHTTPCookie specifies an HTTP cookie that should be
	// captured for logging.
	CaptureHTTPCookie *CaptureHTTPCookie
	// HTTPHeaderNameCaseAdjustments specifies HTTP header name adjustments
	// performed on HTTP headers.
	HTTPHeaderNameCaseAdjustments []HTTPHeaderNameCaseAdjustment
	// HaveClientCA specifies if the user provided their own CA for client auth in mTLS
	HaveClientCA bool
	// HaveCRLs specifies if the crl file is present
	HaveCRLs bool
}

func newTemplateRouter(cfg templateRouterCfg) (*templateRouter, error) {
	dir := cfg.dir

	log.V(2).Info("creating a new template router", "writeDir", dir)
	certManagerConfig := &certificateManagerConfig{
		certKeyFunc:     generateCertKey,
		caCertKeyFunc:   generateCACertKey,
		destCertKeyFunc: generateDestCertKey,
		certDir:         filepath.Join(dir, certDir),
		caCertDir:       filepath.Join(dir, caCertDir),
	}
	certManager, err := newSimpleCertificateManager(certManagerConfig, newSimpleCertificateWriter())
	if err != nil {
		return nil, err
	}

	metricsReload := prometheus.NewSummary(prometheus.SummaryOpts{
		Namespace: "template_router",
		Name:      "reload_seconds",
		Help:      "Measures the time spent reloading the router in seconds.",
	})
	prometheus.MustRegister(metricsReload)
	metricReloadFailure := prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: "template_router",
		Name:      "reload_failure",
		Help:      "Metric to track the status of the most recent HAProxy reload",
	})
	prometheus.MustRegister(metricReloadFailure)
	metricWriteConfig := prometheus.NewSummary(prometheus.SummaryOpts{
		Namespace: "template_router",
		Name:      "write_config_seconds",
		Help:      "Measures the time spent writing out the router configuration to disk in seconds.",
	})
	prometheus.MustRegister(metricWriteConfig)

	router := &templateRouter{
		dir:                           dir,
		templates:                     cfg.templates,
		reloadScriptPath:              cfg.reloadScriptPath,
		reloadInterval:                cfg.reloadInterval,
		reloadCallbacks:               cfg.reloadCallbacks,
		reloadFn:                      cfg.reloadFn,
		state:                         make(map[ServiceAliasConfigKey]ServiceAliasConfig),
		serviceUnits:                  make(map[ServiceUnitKey]ServiceUnit),
		certManager:                   certManager,
		defaultCertificate:            cfg.defaultCertificate,
		defaultCertificatePath:        cfg.defaultCertificatePath,
		defaultCertificateDir:         cfg.defaultCertificateDir,
		defaultDestinationCAPath:      cfg.defaultDestinationCAPath,
		statsUser:                     cfg.statsUser,
		statsPassword:                 cfg.statsPassword,
		statsPort:                     cfg.statsPort,
		allowWildcardRoutes:           cfg.allowWildcardRoutes,
		bindPortsAfterSync:            cfg.bindPortsAfterSync,
		dynamicConfigManager:          cfg.dynamicConfigManager,
		captureHTTPRequestHeaders:     cfg.captureHTTPRequestHeaders,
		captureHTTPResponseHeaders:    cfg.captureHTTPResponseHeaders,
		captureHTTPCookie:             cfg.captureHTTPCookie,
		httpHeaderNameCaseAdjustments: cfg.httpHeaderNameCaseAdjustments,

		metricReload:        metricsReload,
		metricReloadFailure: metricReloadFailure,
		metricWriteConfig:   metricWriteConfig,

		rateLimitedCommitFunction: nil,
	}

	router.EnableRateLimiter(cfg.reloadInterval, router.commitAndReload)

	if err := router.writeDefaultCert(); err != nil {
		return nil, err
	}
	if err := router.watchMutualTLSCert(); err != nil {
		return nil, err
	}
	if router.dynamicConfigManager != nil {
		log.V(0).Info("initializing dynamic config manager ... ")
		router.dynamicConfigManager.Initialize(router, router.defaultCertificatePath)
	}

	return router, nil
}

func (r *templateRouter) EnableRateLimiter(interval time.Duration, handlerFunc limiter.HandlerFunc) {
	r.rateLimitedCommitFunction = limiter.NewCoalescingSerializingRateLimiter(interval, handlerFunc)
	log.V(2).Info("router will coalesce reloads within an interval of each other", "interval", interval.String())
}

// secretToPem composes a PEM file at the output directory from an input private key and crt file.
func secretToPem(secPath, outName string) error {
	// The secret, when present, is mounted on /etc/pki/tls/private
	// The secret has two components crt.tls and key.tls
	// When the default cert is provided by the admin it is a pem
	//   tls.crt is the supplied pem and tls.key is the key
	//   extracted from the pem
	// When the admin does not provide a default cert, the secret
	//   is created via the service annotation. In this case
	//   tls.crt is the cert and tls.key is the key
	//   The crt and key are concatenated to form the needed pem

	var fileCrtName = filepath.Join(secPath, "tls.crt")
	var fileKeyName = filepath.Join(secPath, "tls.key")
	pemBlock, err := ioutil.ReadFile(fileCrtName)
	if err != nil {
		return err
	}
	if len(pemBlock) > 0 && pemBlock[len(pemBlock)-1] != byte('\n') {
		pemBlock = append(pemBlock, byte('\n'))
	}
	keys, err := privateKeysFromPEM(pemBlock)
	if err != nil {
		return err
	}
	if len(keys) == 0 {
		// Try to get the key from the tls.key file
		keyBlock, err := ioutil.ReadFile(fileKeyName)
		if err != nil {
			return err
		}
		if len(keyBlock) > 0 && keyBlock[len(keyBlock)-1] != byte('\n') {
			keyBlock = append(keyBlock, byte('\n'))
		}
		pemBlock = append(pemBlock, keyBlock...)
	}
	return ioutil.WriteFile(outName, pemBlock, 0444)
}

// watchVolumeMountDir adds a watcher on path, which should be a secret or
// configmap volume mount, and calls reloadFn when a change is detected.
func (r *templateRouter) watchVolumeMountDir(path string, reloadFn func()) error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}

	// Suppose path has the value "/etc/pki/private".  We need to know when
	// the content of any file in /etc/pki/private is updated, but the files
	// are really symlinks with a path that includes another symlink.  For
	// example, a secret might have a tls.crt file:
	//
	//     tls.crt -> ..data/tls.crt
	//     ..data -> ..YY_mm_dd-HH_MM_SS.NN
	//
	// where YY_mm_dd-HH_MM_SS.NN is a timestamp for the current version of
	// the secret that contains tls.crt.  When the secret is updated, the
	// ..data symlink changes.  fsnotify may follow symlinks when the watch
	// is added (see <https://github.com/fsnotify/fsnotify/issues/199>),
	// which means that using the symlink when adding the watch would fail
	// to tell us when the ..data symlink were changed to point to a new
	// version of the secret.  Instead, we watch the directory that contains
	// ..data, and when we get an event, we re-evaluate the symlink to see
	// whether it has changed.
	if err := watcher.Add(path); err != nil {
		return err
	}
	log.V(0).Info("watching for changes", "path", path)

	dataPath := filepath.Join(path, "..data")
	currentDataPath, err := filepath.EvalSymlinks(dataPath)
	if err != nil {
		return err
	}
	go func() {
		for {
			select {
			case event, ok := <-watcher.Events:
				if !ok {
					log.V(0).Info("fsnotify channel closed")
					return
				}

				newDataPath, err := filepath.EvalSymlinks(dataPath)
				if err != nil {
					log.Error(err, "failed to resolve symlink", "path", dataPath)
					continue
				}
				if newDataPath == currentDataPath {
					continue
				}
				currentDataPath = newDataPath

				log.V(0).Info("got watch event from fsnotify", "operation", event.Op.String(), "path", event.Name)
				reloadFn()
			case err, ok := <-watcher.Errors:
				if !ok {
					log.V(0).Info("fsnotify channel closed")
					return
				}
				log.Error(err, "received error from fsnotify")
			}
		}
	}()
	return nil
}

// writeDefaultCert ensures that the default certificate in pem format is in a file
// and the file name is set in r.defaultCertificatePath
func (r *templateRouter) writeDefaultCert() error {
	dir := filepath.Join(r.dir, certDir)
	outPath := filepath.Join(dir, fmt.Sprintf("%s.pem", defaultCertName))
	if len(r.defaultCertificate) == 0 {
		// There is no default cert. There may be a path or a secret...
		if len(r.defaultCertificatePath) != 0 {
			// Just use the provided path
			return nil
		}
		if err := secretToPem(r.defaultCertificateDir, outPath); err != nil {
			log.Error(err, "failed to write default cert")
			// no pem file, no default cert, use cert from container
			log.V(0).Info("using default cert from router container image")
		} else {
			r.defaultCertificatePath = outPath
		}
		reloadFn := func() {
			log.V(0).Info("updating default certificate", "path", outPath)
			os.Remove(outPath)
			if err := secretToPem(r.defaultCertificateDir, outPath); err != nil {
				log.Error(err, "failed to update default certificate", "path", outPath)
				return
			}
			log.V(0).Info("reloading to get updated default certificate")
			r.rateLimitedCommitFunction.RegisterChange()
		}
		if err := r.watchVolumeMountDir(r.defaultCertificateDir, reloadFn); err != nil {
			log.V(0).Info("failed to establish watch on certificate directory", "error", err)
			return nil
		}

		return nil
	}

	// write out the default cert (pem format)
	log.V(2).Info("writing default certificate", "dir", dir)
	if err := r.certManager.CertificateWriter().WriteCertificate(dir, defaultCertName, []byte(r.defaultCertificate)); err != nil {
		return err
	}
	r.defaultCertificatePath = outPath
	return nil
}

// watchMutualTLSCert watches the directory containing the certificates for
// mutual TLS and reloads the router if the directory contents change.
func (r *templateRouter) watchMutualTLSCert() error {
	caPath := os.Getenv("ROUTER_MUTUAL_TLS_AUTH_CA")
	if len(caPath) != 0 {
		r.haveClientCA = true
		if err := crl.InitMTLSDirectory(caPath); err != nil {
			return err
		}
		haveCRLs, err := crl.CABundleHasCRLs(caPath)
		if err != nil {
			log.V(0).Error(err, "failed to parse CA Bundle", "path", caPath)
			return err
		}
		r.haveCRLs = haveCRLs
		caUpdateChannel := make(chan struct{})
		crlReloadFn := func(haveCRLs bool) {
			r.haveCRLs = haveCRLs
			log.V(0).Info("reloading to get updated client CA CRL", "name", crl.CRLFilename, "have CRLs", haveCRLs)
			r.rateLimitedCommitFunction.RegisterChange()
		}
		crl.ManageCRLs(caPath, caUpdateChannel, crlReloadFn)
		caReloadFn := func() {
			// Send signal to CRL management goroutine that client CA has been changed
			caUpdateChannel <- struct{}{}
		}
		if err := r.watchVolumeMountDir(filepath.Dir(caPath), caReloadFn); err != nil {
			log.V(0).Error(err, "failed to establish watch on mTLS certificate directory")
			return nil
		}
	}
	return nil
}

// Commit applies the changes made to the router configuration - persists
// the state and refresh the backend. This is all done in the background
// so that we can rate limit + coalesce multiple changes.
// Note: If this is changed FakeCommit() in fake.go should also be updated
func (r *templateRouter) Commit() {
	r.lock.Lock()

	if !r.synced {
		log.V(4).Info("router state synchronized for the first time")
		r.synced = true
		r.stateChanged = true
		r.dynamicallyConfigured = false
	}

	needsCommit := r.stateChanged && !r.dynamicallyConfigured
	r.lock.Unlock()

	if needsCommit {
		r.rateLimitedCommitFunction.RegisterChange()
	}
}

// commitAndReload refreshes the backend and persists the router state.
func (r *templateRouter) commitAndReload() error {
	// only state changes must be done under the lock
	if err := func() error {
		r.lock.Lock()
		defer r.lock.Unlock()

		r.stateChanged = false
		if r.dynamicConfigManager != nil {
			r.dynamicallyConfigured = true
			r.dynamicConfigManager.Notify(RouterEventReloadStart)
		}

		log.V(4).Info("writing the router config")
		reloadStart := time.Now()
		err := r.writeConfig()
		r.metricWriteConfig.Observe(float64(time.Now().Sub(reloadStart)) / float64(time.Second))
		log.V(4).Info("writeConfig", "duration", time.Now().Sub(reloadStart).String())
		return err
	}(); err != nil {
		return err
	}

	for i, fn := range r.reloadCallbacks {
		log.V(4).Info("calling reload function", "fn", i)
		fn()
	}

	log.V(4).Info("reloading the router")
	reloadStart := time.Now()
	err := r.reloadRouter(false)
	r.metricReload.Observe(float64(time.Now().Sub(reloadStart)) / float64(time.Second))
	if err != nil {
		if r.dynamicConfigManager != nil {
			r.dynamicConfigManager.Notify(RouterEventReloadError)
		}
		// Set the metricReloadFailure metric to true when a reload fails.
		r.metricReloadFailure.Set(float64(1))
		return err
	}

	// Set the metricReloadFailure metric to false when a reload succeeds.
	r.metricReloadFailure.Set(float64(0))

	if r.dynamicConfigManager != nil {
		r.dynamicConfigManager.Notify(RouterEventReloadEnd)
	}

	return nil
}

// writeConfig writes the config to disk
// Must be called while holding r.lock
func (r *templateRouter) writeConfig() error {
	//write out any certificate files that don't exist
	for k, cfg := range r.state {
		cfg := cfg // avoid implicit memory aliasing (gosec G601)
		if err := r.writeCertificates(&cfg); err != nil {
			return fmt.Errorf("error writing certificates for %s: %v", k, err)
		}

		// calculate the server weight for the endpoints in each service
		// called here to make sure we have the actual number of endpoints.
		cfg.ServiceUnitNames = r.calculateServiceWeights(cfg.ServiceUnits, cfg.PreferPort)

		// Calculate the number of active endpoints for the route.
		cfg.ActiveEndpoints = r.getActiveEndpoints(cfg.ServiceUnits, cfg.PreferPort)

		cfg.Status = ServiceAliasConfigStatusSaved
		r.state[k] = cfg
	}

	log.V(4).Info("committing router certificate manager changes...")
	if err := r.certManager.Commit(); err != nil {
		return fmt.Errorf("error committing certificate changes: %v", err)
	}

	log.V(4).Info("router certificate manager config committed")

	disableHTTP2, _ := strconv.ParseBool(os.Getenv("ROUTER_DISABLE_HTTP2"))

	for name, template := range r.templates {
		filename := filepath.Join(r.dir, name)
		if err := os.MkdirAll(filepath.Dir(filename), 0777); err != nil {
			return fmt.Errorf("error creating path %q: %v", filepath.Dir(filename), err)
		}
		file, err := os.Create(filename)
		if err != nil {
			return fmt.Errorf("error creating config file %s: %v", filename, err)
		}

		data := templateData{
			WorkingDir:                    r.dir,
			State:                         r.state,
			ServiceUnits:                  r.serviceUnits,
			DefaultCertificate:            r.defaultCertificatePath,
			DefaultDestinationCA:          r.defaultDestinationCAPath,
			StatsUser:                     r.statsUser,
			StatsPassword:                 r.statsPassword,
			StatsPort:                     r.statsPort,
			BindPorts:                     !r.bindPortsAfterSync || r.synced,
			DynamicConfigManager:          r.dynamicConfigManager,
			DisableHTTP2:                  disableHTTP2,
			CaptureHTTPRequestHeaders:     r.captureHTTPRequestHeaders,
			CaptureHTTPResponseHeaders:    r.captureHTTPResponseHeaders,
			CaptureHTTPCookie:             r.captureHTTPCookie,
			HTTPHeaderNameCaseAdjustments: r.httpHeaderNameCaseAdjustments,
			HaveClientCA:                  r.haveClientCA,
			HaveCRLs:                      r.haveCRLs,
		}
		if err := template.Execute(file, data); err != nil {
			file.Close()
			return fmt.Errorf("error executing template for file %s: %v", filename, err)
		}
		file.Close()
	}

	return nil
}

// writeCertificates attempts to write certificates only if the cfg requires it see shouldWriteCerts
// for details
func (r *templateRouter) writeCertificates(cfg *ServiceAliasConfig) error {
	if r.shouldWriteCerts(cfg) {
		return r.certManager.WriteCertificatesForConfig(cfg)
	}
	return nil
}

// reloadRouter executes the router's reload script.
func (r *templateRouter) reloadRouter(shutdown bool) error {
	if r.reloadFn != nil {
		return r.reloadFn(shutdown)
	}
	cmd := exec.Command(r.reloadScriptPath)
	if shutdown {
		cmd.Env = append(os.Environ(), "ROUTER_SHUTDOWN=true")
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("error reloading router: %v\n%s", err, string(out))
	}
	log.V(0).Info("router reloaded", "output", string(out))
	return nil
}

func (r *templateRouter) FilterNamespaces(namespaces sets.String) {
	r.lock.Lock()
	defer r.lock.Unlock()

	if len(namespaces) == 0 {
		r.state = make(map[ServiceAliasConfigKey]ServiceAliasConfig)
		r.serviceUnits = make(map[ServiceUnitKey]ServiceUnit)
		r.stateChanged = true
	}
	for key, service := range r.serviceUnits {
		// TODO: the id of a service unit should be defined inside this class, not passed in from the outside
		//   remove the leak of the abstraction when we refactor this code
		ns, _ := getPartsFromEndpointsKey(key)
		if namespaces.Has(ns) {
			continue
		}
		r.deleteServiceUnitInternal(key, service)
	}

	for k := range r.state {
		ns, _ := getPartsFromRouteKey(k)
		if namespaces.Has(ns) {
			continue
		}
		delete(r.state, k)
		r.stateChanged = true
	}

	if r.stateChanged {
		r.dynamicallyConfigured = false
	}
}

// CreateServiceUnit creates a new service named with the given id.
func (r *templateRouter) CreateServiceUnit(id ServiceUnitKey) {
	r.lock.Lock()
	defer r.lock.Unlock()

	r.createServiceUnitInternal(id)
}

// createServiceUnitInternal creates a new service named with the given id -
// internal lockless form, caller needs to ensure lock acquisition [and
// release].
func (r *templateRouter) createServiceUnitInternal(id ServiceUnitKey) {
	namespace, name := getPartsFromEndpointsKey(id)
	service := ServiceUnit{
		Name:          string(id),
		Hostname:      fmt.Sprintf("%s.%s.svc", name, namespace),
		EndpointTable: []Endpoint{},

		ServiceAliasAssociations: make(map[ServiceAliasConfigKey]bool),
	}

	r.serviceUnits[id] = service
}

// findMatchingServiceUnit finds the service with the given id - internal
// lockless form, caller needs to ensure lock acquisition [and release].
func (r *templateRouter) findMatchingServiceUnit(id ServiceUnitKey) (ServiceUnit, bool) {
	v, ok := r.serviceUnits[id]
	return v, ok
}

// FindServiceUnit finds the service with the given id.
func (r *templateRouter) FindServiceUnit(id ServiceUnitKey) (ServiceUnit, bool) {
	r.lock.Lock()
	defer r.lock.Unlock()

	return r.findMatchingServiceUnit(id)
}

// DeleteServiceUnit deletes the service with the given id.
func (r *templateRouter) DeleteServiceUnit(id ServiceUnitKey) {
	r.lock.Lock()
	defer r.lock.Unlock()

	service, ok := r.findMatchingServiceUnit(id)
	if !ok {
		return
	}

	r.deleteServiceUnitInternal(id, service)
}

// deleteServiceUnitInternal deletes the service with the given
// id. It differs from DeleteServiceUnit() as it assumes the
// caller has taken the lock.
func (r *templateRouter) deleteServiceUnitInternal(id ServiceUnitKey, service ServiceUnit) {
	delete(r.serviceUnits, id)
	if len(service.ServiceAliasAssociations) > 0 {
		r.stateChanged = true
	}
}

// addServiceAliasAssociation adds a reference to the backend in the ServiceUnit config.
func (r *templateRouter) addServiceAliasAssociation(id ServiceUnitKey, alias ServiceAliasConfigKey) {
	if serviceUnit, ok := r.findMatchingServiceUnit(id); ok {
		log.V(4).Info("associated service unit -> service alias", "id", id, "alias", alias)
		serviceUnit.ServiceAliasAssociations[alias] = true
	}
}

// removeServiceAliasAssociation removes the reference to the backend in the ServiceUnit config.
func (r *templateRouter) removeServiceAliasAssociation(id ServiceUnitKey, alias ServiceAliasConfigKey) {
	if serviceUnit, ok := r.findMatchingServiceUnit(id); ok {
		log.V(4).Info("removed association for service unit -> service alias", "id", id, "alias", alias)
		delete(serviceUnit.ServiceAliasAssociations, alias)
	}
}

// dynamicallyAddRoute attempts to dynamically add a route.
// Note: The config should have been synced at least once initially and
// the caller needs to acquire a lock [and release it].
func (r *templateRouter) dynamicallyAddRoute(backendKey ServiceAliasConfigKey, route *routev1.Route, backend *ServiceAliasConfig) bool {
	if r.dynamicConfigManager == nil {
		return false
	}

	log.V(4).Info("dynamically adding route backend", "backendKey", backendKey)
	r.dynamicConfigManager.Register(backendKey, route)

	// If no initial sync was done, don't try to dynamically add the
	// route as we will need a reload anyway.
	if !r.synced {
		return false
	}

	err := r.dynamicConfigManager.AddRoute(backendKey, backend.RoutingKeyName, route)
	if err != nil {
		log.V(4).Info("router will reload as the ConfigManager could not dynamically add route for backend", "backendKey", backendKey, "error", err)
		return false
	}

	// For each referenced service unit replace the route endpoints.
	oldEndpoints := []Endpoint{}

	// As the endpoints have changed, recalculate the weights.
	newWeights := r.calculateServiceWeights(backend.ServiceUnits, backend.PreferPort)
	for key := range backend.ServiceUnits {
		if service, ok := r.findMatchingServiceUnit(key); ok {
			newEndpoints := endpointsForAlias(*backend, service)
			log.V(4).Info("for new route backend, replacing endpoints for service", "backendKey", backendKey, "serviceKey", key, "newEndpoints", newEndpoints)

			weight, ok := newWeights[key]
			if !ok {
				weight = 0
			}
			if err := r.dynamicConfigManager.ReplaceRouteEndpoints(backendKey, oldEndpoints, newEndpoints, weight); err != nil {
				log.V(4).Info("router will reload as the ConfigManager could not dynamically replace endpoints for route backend",
					"backendKey", backendKey, "serviceKey", key, "error", err)
				return false
			}
		}
	}

	log.V(4).Info("dynamically added route backend", "backendKey", backendKey)
	return true
}

// dynamicallyRemoveRoute attempts to dynamically remove a route.
// Note: The config should have been synced at least once initially and
// the caller needs to acquire a lock [and release it].
func (r *templateRouter) dynamicallyRemoveRoute(backendKey ServiceAliasConfigKey, route *routev1.Route) bool {
	if r.dynamicConfigManager == nil || !r.synced {
		return false
	}

	log.V(4).Info("dynamically removing route backend", "backendKey", backendKey)

	if err := r.dynamicConfigManager.RemoveRoute(backendKey, route); err != nil {
		log.V(4).Info("router will reload as the ConfigManager could not dynamically remove route backend", "backendKey", backendKey, "error", err)
		return false
	}

	return true
}

// dynamicallyReplaceEndpoints attempts to dynamically replace endpoints
// on all the routes associated with a given service.
// Note: The config should have been synced at least once initially and
// the caller needs to acquire a lock [and release it].
func (r *templateRouter) dynamicallyReplaceEndpoints(id ServiceUnitKey, service ServiceUnit, oldEndpoints []Endpoint) bool {
	if r.dynamicConfigManager == nil || !r.synced {
		return false
	}

	log.V(4).Info("replacing endpoints dynamically for service", "service", id)

	// Update each of the routes that reference this service unit.
	for backendKey := range service.ServiceAliasAssociations {
		cfg, ok := r.state[backendKey]
		if !ok {
			log.V(4).Info("associated service alias not found in state, ignoring ...", "serviceAlias", backendKey)
			continue
		}

		newEndpoints := endpointsForAlias(cfg, service)

		// As the endpoints have changed, recalculate the weights.
		newWeights := r.calculateServiceWeights(cfg.ServiceUnits, cfg.PreferPort)

		// Get the weight for this service unit.
		weight, ok := newWeights[id]
		if !ok {
			weight = 0
		}

		log.V(4).Info("dynamically replacing endpoints for associated backend", "backendKey", backendKey, "newEndpoints", newEndpoints)
		if err := r.dynamicConfigManager.ReplaceRouteEndpoints(backendKey, oldEndpoints, newEndpoints, weight); err != nil {
			// Error dynamically modifying the config, so return false to cause a reload to happen.
			log.V(4).Info("router will reload as the ConfigManager could not dynamically replace endpoints for service", "service", id, "backendKey", backendKey, "weight", weight, "error", err)
			return false
		}
	}

	return true
}

// dynamicallyRemoveEndpoints attempts to dynamically remove endpoints on
// all the routes associated with a given service.
// Note: The config should have been synced at least once initially and
// the caller needs to acquire a lock [and release it].
func (r *templateRouter) dynamicallyRemoveEndpoints(service ServiceUnit, endpoints []Endpoint) bool {
	if r.dynamicConfigManager == nil || !r.synced {
		return false
	}

	log.V(4).Info("dynamically removing endpoints for service unit", "service", service.Name)

	for backendKey := range service.ServiceAliasAssociations {
		if _, ok := r.state[backendKey]; !ok {
			continue
		}

		log.V(4).Info("dynamically removing endpoints for associated backend", "backendKey", backendKey)
		if err := r.dynamicConfigManager.RemoveRouteEndpoints(backendKey, endpoints); err != nil {
			// Error dynamically modifying the config, so return false to cause a reload to happen.
			log.V(4).Info("router will reload as the ConfigManager could not dynamically remove endpoints for backend", "backendKey", backendKey, "error", err)
			return false
		}
	}

	return true
}

// DeleteEndpoints deletes the endpoints for the service with the given id.
func (r *templateRouter) DeleteEndpoints(id ServiceUnitKey) {
	r.lock.Lock()
	defer r.lock.Unlock()
	service, ok := r.findMatchingServiceUnit(id)
	if !ok {
		return
	}

	configChanged := r.dynamicallyRemoveEndpoints(service, service.EndpointTable)

	service.EndpointTable = []Endpoint{}

	r.serviceUnits[id] = service

	if len(service.ServiceAliasAssociations) > 0 {
		r.stateChanged = true
	}
	r.dynamicallyConfigured = r.dynamicallyConfigured && configChanged
}

// routeKey generates route key. This allows templates to use this key without having to create a separate method
func routeKey(route *routev1.Route) ServiceAliasConfigKey {
	return routeKeyFromParts(route.Namespace, route.Name)
}

func routeKeyFromParts(namespace, name string) ServiceAliasConfigKey {
	return ServiceAliasConfigKey(fmt.Sprintf("%s%s%s", namespace, routeKeySeparator, name))
}

func getPartsFromRouteKey(key ServiceAliasConfigKey) (string, string) {
	tokens := strings.SplitN(string(key), routeKeySeparator, 2)
	if len(tokens) != 2 {
		log.Error(nil, "expected separator not found in route key", "separator", routeKeySeparator, "key", key)
	}
	namespace := tokens[0]
	name := tokens[1]
	return namespace, name
}

// createServiceAliasConfig creates a ServiceAliasConfig from a route and the router state.
// The router state is not modified in the process, so referenced ServiceUnits may not exist.
func (r *templateRouter) createServiceAliasConfig(route *routev1.Route, backendKey ServiceAliasConfigKey) *ServiceAliasConfig {
	wantsWildcardSupport := (route.Spec.WildcardPolicy == routev1.WildcardPolicySubdomain)

	// The router config trumps what the route asks for/wants.
	wildcard := r.allowWildcardRoutes && wantsWildcardSupport

	// Get the service weights from each service in the route. Count the active
	// ones (with a non-zero weight)
	serviceUnits := getServiceUnits(route)
	activeServiceUnits := 0
	for _, weight := range serviceUnits {
		if weight > 0 {
			activeServiceUnits++
		}
	}

	config := ServiceAliasConfig{
		Name:               route.Name,
		Namespace:          route.Namespace,
		Host:               route.Spec.Host,
		Path:               route.Spec.Path,
		IsWildcard:         wildcard,
		Annotations:        route.Annotations,
		ServiceUnits:       serviceUnits,
		ActiveServiceUnits: activeServiceUnits,
	}

	if route.Spec.Port != nil {
		config.PreferPort = route.Spec.Port.TargetPort.String()
	}

	key := fmt.Sprintf("%s %s", config.TLSTermination, backendKey)
	config.RoutingKeyName = fmt.Sprintf("%x", md5.Sum([]byte(key)))

	tls := route.Spec.TLS
	if tls != nil && len(tls.Termination) > 0 {
		config.TLSTermination = tls.Termination

		config.InsecureEdgeTerminationPolicy = tls.InsecureEdgeTerminationPolicy

		if tls.Termination == routev1.TLSTerminationReencrypt && len(tls.DestinationCACertificate) == 0 && len(r.defaultDestinationCAPath) > 0 {
			config.VerifyServiceHostname = true
		}

		if tls.Termination != routev1.TLSTerminationPassthrough {
			config.Certificates = make(map[string]Certificate)

			if len(tls.Certificate) > 0 {
				certKey := generateCertKey(&config)
				cert := Certificate{
					ID:         string(backendKey),
					Contents:   tls.Certificate,
					PrivateKey: tls.Key,
				}

				config.Certificates[certKey] = cert
			}

			if len(tls.CACertificate) > 0 {
				caCertKey := generateCACertKey(&config)
				caCert := Certificate{
					ID:       string(backendKey),
					Contents: tls.CACertificate,
				}

				config.Certificates[caCertKey] = caCert
			}

			if len(tls.DestinationCACertificate) > 0 {
				destCertKey := generateDestCertKey(&config)
				destCert := Certificate{
					ID:       string(backendKey),
					Contents: tls.DestinationCACertificate,
				}

				config.Certificates[destCertKey] = destCert
			}
		}
	}

	return &config
}

// AddRoute adds the given route to the router state if the route
// hasn't been seen before or has changed since it was last seen.
func (r *templateRouter) AddRoute(route *routev1.Route) {
	backendKey := routeKey(route)

	newConfig := r.createServiceAliasConfig(route, backendKey)

	// We have to call the internal form of functions after this
	// because we are holding the state lock.
	r.lock.Lock()
	defer r.lock.Unlock()

	if existingConfig, exists := r.state[backendKey]; exists {
		if configsAreEqual(newConfig, &existingConfig) {
			return
		}

		log.V(4).Info("updating route", "namespace", route.Namespace, "name", route.Name)

		// Delete the route first, because modify is to be treated as delete+add
		r.removeRouteInternal(route)

		// TODO - clean up service units that are no longer
		// referenced.  This may be challenging if a service unit can
		// be referenced by more than one route, but the alternative
		// is having stale service units accumulate with the attendant
		// cost to router memory usage.
	} else {
		log.V(4).Info("adding route", "namespace", route.Namespace, "name", route.Name)
	}

	// Add service units referred to by the config
	for key := range newConfig.ServiceUnits {
		if _, ok := r.findMatchingServiceUnit(key); !ok {
			log.V(4).Info("creating new frontend", "key", key)
			r.createServiceUnitInternal(key)
		}
		r.addServiceAliasAssociation(key, backendKey)
	}

	configChanged := r.dynamicallyAddRoute(backendKey, route, newConfig)

	r.state[backendKey] = *newConfig
	r.stateChanged = true
	r.dynamicallyConfigured = r.dynamicallyConfigured && configChanged
}

// RemoveRoute removes the given route
func (r *templateRouter) RemoveRoute(route *routev1.Route) {
	r.lock.Lock()
	defer r.lock.Unlock()

	r.removeRouteInternal(route)
}

// removeRouteInternal removes the given route - internal
// lockless form, caller needs to ensure lock acquisition [and release].
func (r *templateRouter) removeRouteInternal(route *routev1.Route) {
	backendKey := routeKey(route)
	serviceAliasConfig, ok := r.state[backendKey]
	if !ok {
		return
	}

	configChanged := r.dynamicallyRemoveRoute(backendKey, route)

	for key := range serviceAliasConfig.ServiceUnits {
		r.removeServiceAliasAssociation(key, backendKey)
	}

	r.cleanUpServiceAliasConfig(&serviceAliasConfig)
	delete(r.state, backendKey)
	r.stateChanged = true
	r.dynamicallyConfigured = r.dynamicallyConfigured && configChanged
}

// numberOfEndpoints returns the number of endpoints
// If port parameter is non-empty string, then only endpoints matching port will be counted.
// Must be called while holding r.lock
func (r *templateRouter) numberOfEndpoints(id ServiceUnitKey, port string) int32 {
	var eps = 0
	svc, ok := r.findMatchingServiceUnit(id)
	if ok && len(svc.EndpointTable) > eps {
		if len(port) == 0 {
			eps = len(svc.EndpointTable)
		} else {
			for _, ep := range svc.EndpointTable {
				if ep.Port == port || ep.PortName == port {
					eps += 1
				}
			}
		}
	}
	return int32(eps)
}

// AddEndpoints adds new Endpoints for the given id.
func (r *templateRouter) AddEndpoints(id ServiceUnitKey, endpoints []Endpoint) {
	r.lock.Lock()
	defer r.lock.Unlock()
	frontend, _ := r.findMatchingServiceUnit(id)

	//only make the change if there is a difference
	if reflect.DeepEqual(frontend.EndpointTable, endpoints) {
		log.V(4).Info("ignoring change, endpoints are the same", "id", id)
		return
	}

	oldEndpoints := frontend.EndpointTable

	frontend.EndpointTable = endpoints
	r.serviceUnits[id] = frontend

	configChanged := r.dynamicallyReplaceEndpoints(id, frontend, oldEndpoints)
	if len(frontend.ServiceAliasAssociations) > 0 {
		r.stateChanged = true
	}
	r.dynamicallyConfigured = r.dynamicallyConfigured && configChanged
}

// cleanUpServiceAliasConfig performs any necessary steps to clean up a service alias config before deleting it from
// the router.  Right now the only clean up step is to remove any of the certificates on disk.
func (r *templateRouter) cleanUpServiceAliasConfig(cfg *ServiceAliasConfig) {
	err := r.certManager.DeleteCertificatesForConfig(cfg)
	if err != nil {
		log.Error(err, "error deleting certificates for route, the route will still be deleted but files may remain in the container", "host", cfg.Host)
	}
}

func cmpStrSlices(first []string, second []string) bool {
	if len(first) != len(second) {
		return false
	}
	for _, fi := range first {
		found := false
		for _, si := range second {
			if fi == si {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// shouldWriteCerts determines if the router should ask the cert manager to write out certificates
// it will return true if a route is edge or reencrypt and it has all the required (host/key) certificates
// defined.  If the route does not have the certificates defined it will log an info message if the
// router is configured with a default certificate and assume the route is meant to be a wildcard.  Otherwise
// it will log a warning.  The route will still be written but users may receive browser errors
// for a host/cert mismatch
func (r *templateRouter) shouldWriteCerts(cfg *ServiceAliasConfig) bool {

	// The cert is already written
	if cfg.Status == ServiceAliasConfigStatusSaved {
		return false
	}

	if cfg.Certificates == nil {
		return false
	}

	if cfg.TLSTermination == routev1.TLSTerminationEdge || cfg.TLSTermination == routev1.TLSTerminationReencrypt {
		if hasRequiredEdgeCerts(cfg) {
			return true
		}

		if cfg.TLSTermination == routev1.TLSTerminationReencrypt {
			if hasReencryptDestinationCACert(cfg) {
				log.V(4).Info("a reencrypt route does not have an edge certificate, using default router certificate", "host", cfg.Host)
				return true
			}
			if len(r.defaultDestinationCAPath) > 0 {
				log.V(4).Info("a reencrypt route does not have a destination CA, using default destination CA", "host", cfg.Host)
				return true
			}
		}

		msg := fmt.Sprintf("a %s terminated route with host %s does not have the required certificates.  The route will still be created but no certificates will be written",
			cfg.TLSTermination, cfg.Host)
		// if a default cert is configured we'll assume it is meant to be a wildcard and only log info
		// otherwise we'll consider this a warning
		if len(r.defaultCertificatePath) > 0 {
			log.V(4).Info(msg)
		} else {
			log.V(0).Info(msg)
		}
		return false
	}
	return false
}

// HasRoute indicates whether the given route is known to this router.
func (r *templateRouter) HasRoute(route *routev1.Route) bool {
	r.lock.Lock()
	defer r.lock.Unlock()
	key := routeKey(route)
	_, ok := r.state[key]
	return ok
}

// SyncedAtLeastOnce indicates whether the router has completed an initial sync.
func (r *templateRouter) SyncedAtLeastOnce() bool {
	r.lock.Lock()
	defer r.lock.Unlock()
	return r.synced
}

// hasRequiredEdgeCerts ensures that at least a host certificate and key are provided.
// a ca cert is not required because it may be something that is in the root cert chain
func hasRequiredEdgeCerts(cfg *ServiceAliasConfig) bool {
	certKey := generateCertKey(cfg)
	hostCert, ok := cfg.Certificates[certKey]
	return ok && len(hostCert.Contents) > 0 && len(hostCert.PrivateKey) > 0
}

// hasReencryptDestinationCACert checks whether a destination CA certificate has been provided.
func hasReencryptDestinationCACert(cfg *ServiceAliasConfig) bool {
	destCertKey := generateDestCertKey(cfg)
	destCACert, ok := cfg.Certificates[destCertKey]
	return ok && len(destCACert.Contents) > 0
}

func generateCertKey(config *ServiceAliasConfig) string {
	return config.Host
}

func generateCACertKey(config *ServiceAliasConfig) string {
	return config.Host + caCertPostfix
}

func generateDestCertKey(config *ServiceAliasConfig) string {
	return config.Host + destCertPostfix
}

// getServiceUnits returns a map of service keys to their weights.
// The requests are loadbalanced among the services referenced by the route.
// The weight (0-256, default 1) sets the relative proportions each
// service gets (weight/sum_of_weights) fraction of the requests.
// Default when service weight is omitted is 1.
// if weight < 0 or > 256 set to 0.
// When the weight is 0 no traffic goes to the service. If they are
// all 0 the request is returned with 503 response.
func getServiceUnits(route *routev1.Route) map[ServiceUnitKey]int32 {
	serviceUnits := make(map[ServiceUnitKey]int32)

	// get the weight and number of endpoints for each service
	key := endpointsKeyFromParts(route.Namespace, route.Spec.To.Name)
	serviceUnits[key] = getServiceUnitWeight(route.Spec.To.Weight)

	for _, svc := range route.Spec.AlternateBackends {
		key = endpointsKeyFromParts(route.Namespace, svc.Name)
		serviceUnits[key] = getServiceUnitWeight(svc.Weight)
	}

	return serviceUnits
}

// getServiceUnitWeight takes a reference to a weight and returns its value or the default.
// It also checks that it is in the correct range.
func getServiceUnitWeight(weightRef *int32) int32 {
	// Default to 1 if there is no weight
	var weight int32 = 1
	if weightRef != nil {
		weight = *weightRef
	}

	// Do a bounds check
	if weight < 0 {
		weight = 0
	} else if weight > 256 {
		weight = 256
	}

	return weight
}

// getActiveEndpoints calculates the number of endpoints that are not associated
// with service units with a zero weight and returns the count.
// The port parameter, if set, will only count endpoints matching that port.
func (r *templateRouter) getActiveEndpoints(serviceUnits map[ServiceUnitKey]int32, port string) int {
	var activeEndpoints int32 = 0

	for key, weight := range serviceUnits {
		if weight > 0 {
			activeEndpoints += r.numberOfEndpoints(key, port)
		}
	}

	return int(activeEndpoints)
}

// calculateServiceWeights returns a map of service keys to their weights.
// Each service gets (weight/sum_of_weights) fraction of the requests.
// For each service, the requests are distributed among the endpoints.
// Each endpoint gets weight/numberOfEndpoints portion of the requests.
// The largest weight per endpoint is scaled to 256 to permit better
// precision results.  The remainder are scaled using the same scale factor.
// Inaccuracies occur when converting float32 to int32 and when the scaled
// weight per endpoint is less than 1.0, the minimum.
// The above assumes roundRobin scheduling.
// The port parameter, if set, will only count endpoints matching that port.
func (r *templateRouter) calculateServiceWeights(serviceUnits map[ServiceUnitKey]int32, port string) map[ServiceUnitKey]int32 {
	serviceUnitNames := make(map[ServiceUnitKey]int32)

	// If there is only 1 service unit, then always set the weight 1 for all the endpoints.
	// Scaling the weight to 256 is redundant and causes haproxy to allocate more memory on startup.
	if len(serviceUnits) == 1 {
		for key := range serviceUnits {
			if r.numberOfEndpoints(key, port) > 0 {
				serviceUnitNames[key] = 1
			}
		}
		return serviceUnitNames
	}

	// portion of service weight for each endpoint
	epWeight := make(map[ServiceUnitKey]float32)
	// maximum endpoint weight
	var maxEpWeight float32 = 0.0

	// distribute service weight over the service's endpoints
	// to get weight per endpoint
	for key, units := range serviceUnits {
		numEp := r.numberOfEndpoints(key, port)
		if numEp > 0 {
			epWeight[key] = float32(units) / float32(numEp)
		}
		if epWeight[key] > maxEpWeight {
			maxEpWeight = epWeight[key]
		}
	}

	// Scale the weights to near the maximum (256).
	// This improves precision when scaling for the endpoints
	var scaleWeight float32 = 0.0
	if maxEpWeight > 0.0 {
		scaleWeight = 256.0 / maxEpWeight
	}

	// The weight assigned to the service is distributed among the endpoints
	// for example the if we have two services "A" with weight 20 and 2 endpoints
	// and "B" with  weight 10 and 4 endpoints the ultimate weights on
	// endpoints would work out as:
	// service "A" weight per endpoint 10.0
	// service "B" weight per endpoint 2.5
	// maximum endpoint weight is 10.0 so scale is 25.6
	// service "A" scaled endpoint weight 256.0 truncated to 256
	// service "B" scaled endpoint weight 64.0 truncated to 64
	// So, all service "A" endpoints get 256 and all service "B" endpoints get 64

	for key, weight := range epWeight {
		serviceUnitNames[key] = int32(weight * scaleWeight)
		if weight > 0.0 && serviceUnitNames[key] < 1 {
			serviceUnitNames[key] = 1
			numEp := r.numberOfEndpoints(key, port)
			log.V(4).Info("WARNING: Too many service endpoints to achieve desired weight for route.",
				"key", key, "maxEndpoints", int32(weight*float32(numEp)), "actualEndpoints", numEp)
		}
		log.V(6).Info(fmt.Sprintf("%s: weight %d  %f  %d", key, serviceUnits[key], weight, serviceUnitNames[key]))
	}

	return serviceUnitNames
}

// configsAreEqual determines whether the given service alias configs can be considered equal.
// This may be useful in determining whether a new service alias config is the same as an
// existing one or represents an update to its state.
func configsAreEqual(config1, config2 *ServiceAliasConfig) bool {
	return config1.Name == config2.Name &&
		config1.Namespace == config2.Namespace &&
		config1.Host == config2.Host &&
		config1.Path == config2.Path &&
		config1.TLSTermination == config2.TLSTermination &&
		reflect.DeepEqual(config1.Certificates, config2.Certificates) &&
		// Status isn't compared since whether certs have been written
		// to disk or not isn't relevant in determining whether a
		// route needs to be updated.
		config1.PreferPort == config2.PreferPort &&
		config1.InsecureEdgeTerminationPolicy == config2.InsecureEdgeTerminationPolicy &&
		config1.RoutingKeyName == config2.RoutingKeyName &&
		config1.IsWildcard == config2.IsWildcard &&
		config1.VerifyServiceHostname == config2.VerifyServiceHostname &&
		reflect.DeepEqual(config1.Annotations, config2.Annotations) &&
		reflect.DeepEqual(config1.ServiceUnits, config2.ServiceUnits)
}

// privateKeysFromPEM extracts all blocks recognized as private keys into an output PEM encoded byte array,
// or returns an error. If there are no private keys it will return an empty byte buffer.
func privateKeysFromPEM(pemCerts []byte) ([]byte, error) {
	buf := &bytes.Buffer{}
	for len(pemCerts) > 0 {
		var block *pem.Block
		block, pemCerts = pem.Decode(pemCerts)
		if block == nil {
			break
		}
		if len(block.Headers) != 0 {
			continue
		}
		switch block.Type {
		// defined in OpenSSL pem.h
		case "RSA PRIVATE KEY", "PRIVATE KEY", "ANY PRIVATE KEY", "DSA PRIVATE KEY", "ENCRYPTED PRIVATE KEY", "EC PRIVATE KEY":
			if err := pem.Encode(buf, block); err != nil {
				return nil, err
			}
		}
	}
	return buf.Bytes(), nil
}
