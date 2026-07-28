package templaterouter

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"sync"
	"sync/atomic"
	"testing"

	routev1 "github.com/openshift/api/route/v1"
)

func TestCertManager(t *testing.T) {
	cfg := newFakeCertificateManagerConfig()
	fakeCertWriter := &fakeCertWriter{}
	certManager, _ := newSimpleCertificateManager(cfg, fakeCertWriter)

	testCases := map[string]struct {
		cfg             *ServiceAliasConfig
		expectedAdds    []string
		expectedDeletes []string
	}{
		"add cert nil config": {
			expectedAdds:    []string{},
			expectedDeletes: []string{},
		},
		"add cert edge": {
			//expect that the ca cert will be concatenated to the regular cert so we only come out with a single add/delete
			cfg: &ServiceAliasConfig{
				Host:           "www.example.com",
				TLSTermination: routev1.TLSTerminationEdge,
				Certificates: map[string]Certificate{
					"www.example.com": {
						ID: "testCert",
					},
					"www.example.com" + caCertPostfix: {
						ID: "testCert",
					},
				},
			},
			expectedAdds:    []string{cfg.certDir + "testCert"},
			expectedDeletes: []string{cfg.certDir + "testCert"},
		},
		"add cert passthrough": {
			//passthrough should not define certs (enforced by the api) Certmanager shouldn't attempt to write anything for it
			//even if it has certs since it is unsupported
			cfg: &ServiceAliasConfig{
				Host:           "www.example.com",
				TLSTermination: routev1.TLSTerminationPassthrough,
				Certificates: map[string]Certificate{
					"www.example.com": {
						ID: "testCert",
					},
					"www.example.com" + caCertPostfix: {
						ID: "testCert",
					},
				},
			},
			expectedAdds:    []string{},
			expectedDeletes: []string{},
		},
		"add cert reencrypt": {
			//expect that we have 2 adds/deletes.  1 for the regular cert/ca and 1 for the destination cert
			cfg: &ServiceAliasConfig{
				Host:           "www.example.com",
				TLSTermination: routev1.TLSTerminationReencrypt,
				Certificates: map[string]Certificate{
					"www.example.com": {
						ID: "testCert",
					},
					"www.example.com" + caCertPostfix: {
						ID: "testCert",
					},
					"www.example.com" + destCertPostfix: {
						ID: "testCert",
					},
				},
			},
			expectedAdds:    []string{cfg.certDir + "testCert", cfg.caCertDir + "testCert"},
			expectedDeletes: []string{cfg.certDir + "testCert", cfg.caCertDir + "testCert"},
		},
		"add cert no certs": {
			cfg: &ServiceAliasConfig{
				Host:           "www.example.com",
				TLSTermination: routev1.TLSTerminationEdge,
				Certificates:   map[string]Certificate{},
			},
			expectedAdds:    []string{},
			expectedDeletes: []string{},
		},
		"add cert no tls termination type": {
			cfg: &ServiceAliasConfig{
				Host: "www.example.com",
				Certificates: map[string]Certificate{
					"www.example.com": {
						ID: "testCert",
					},
					"www.example.com" + caCertPostfix: {
						ID: "testCert",
					},
				},
			},
			expectedAdds:    []string{},
			expectedDeletes: []string{},
		},
		"add cert invalid tls termination type": {
			cfg: &ServiceAliasConfig{
				Host:           "www.example.com",
				TLSTermination: "invalid",
				Certificates: map[string]Certificate{
					"www.example.com": {
						ID: "testCert",
					},
					"www.example.com" + caCertPostfix: {
						ID: "testCert",
					},
				},
			},
			expectedAdds:    []string{},
			expectedDeletes: []string{},
		},
	}

	for k, tc := range testCases {
		certManager.Commit()
		fakeCertWriter.clear()
		err := certManager.WriteCertificatesForConfig(tc.cfg)
		if err != nil {
			t.Fatalf("Unexpected error writing certs for service alias config for %s.  Config: %v, err: %v", k, tc.cfg, err)
		}
		err = certManager.DeleteCertificatesForConfig(tc.cfg)
		if err != nil {
			t.Fatalf("Unexpected error deleting certs for service alias config for %s.  Config: %v, err: %v", k, tc.cfg, err)
		}

		if len(tc.expectedAdds) != len(fakeCertWriter.addedCerts) {
			t.Errorf("Unexpected number of adds for %s occurred. Expected: %d Got: %d", k, len(tc.expectedAdds), len(fakeCertWriter.addedCerts))
		}

		if 0 != len(fakeCertWriter.deletedCerts) {
			t.Errorf("Unexpected number of deletes prior to certManager.Commit() for %s occurred. Expected: 0 Got: %d", k, len(fakeCertWriter.deletedCerts))
		}

		err = certManager.Commit()
		if err != nil {
			t.Fatalf("Unexpected error committing certs for service alias config for %s.  Config: %v, err: %v", k, tc.cfg, err)
		}

		if len(tc.expectedAdds) != len(fakeCertWriter.addedCerts) {
			t.Errorf("Unexpected number of adds for %s occurred. Expected: %d Got: %d", k, len(tc.expectedAdds), len(fakeCertWriter.addedCerts))
		}

		if len(tc.expectedDeletes) != len(fakeCertWriter.deletedCerts) {
			t.Errorf("Unexpected number of deletes for %s occurred. Expected: %d Got: %d", k, len(tc.expectedDeletes), len(fakeCertWriter.deletedCerts))
		}

		sort.Strings(tc.expectedAdds)
		sort.Strings(fakeCertWriter.addedCerts)
		if !reflect.DeepEqual(tc.expectedAdds, fakeCertWriter.addedCerts) {
			t.Errorf("Unexpected adds for %s, wanted: %v, got %v", k, tc.expectedAdds, fakeCertWriter.addedCerts)
		}

		sort.Strings(tc.expectedDeletes)
		sort.Strings(fakeCertWriter.deletedCerts)
		if !reflect.DeepEqual(tc.expectedDeletes, fakeCertWriter.deletedCerts) {
			t.Errorf("Unexpected deletes for %s, wanted: %v, got %v", k, tc.expectedDeletes, fakeCertWriter.deletedCerts)
		}
	}
}

func TestCertManagerSkipsWrittenConfigs(t *testing.T) {
	fakeCertWriter := &fakeCertWriter{}
	certManager, _ := newSimpleCertificateManager(newFakeCertificateManagerConfig(), fakeCertWriter)
	cfg := &ServiceAliasConfig{
		Host:           "www.example.com",
		TLSTermination: routev1.TLSTerminationEdge,
		Certificates: map[string]Certificate{
			"www.example.com": {
				ID: "testCert",
			},
			"www.example.com" + caCertPostfix: {
				ID: "testCert",
			},
		},
	}
	certManager.WriteCertificatesForConfig(cfg)
	if len(fakeCertWriter.addedCerts) != 1 {
		t.Errorf("expected 1 add for initial certificate write but got %d", len(fakeCertWriter.addedCerts))
	}
	cfg.Status = ServiceAliasConfigStatusSaved
	certManager.WriteCertificatesForConfig(cfg)
	if len(fakeCertWriter.addedCerts) != 1 {
		t.Errorf("expected 1 add for initial certificate write but got %d", len(fakeCertWriter.addedCerts))
	}
	// clear status and ensure it is written
	cfg.Status = ""
	certManager.WriteCertificatesForConfig(cfg)
	if len(fakeCertWriter.addedCerts) != 2 {
		t.Errorf("expected 2 adds for initial certificate write but got %d", len(fakeCertWriter.addedCerts))
	}
}

func TestCertManagerConfig(t *testing.T) {
	validCfg := newFakeCertificateManagerConfig()

	missingCertKeyCfg := newFakeCertificateManagerConfig()
	missingCertKeyCfg.certKeyFunc = nil

	missingCACertKeyCfg := newFakeCertificateManagerConfig()
	missingCACertKeyCfg.caCertKeyFunc = nil

	missingDestCertKeyCfg := newFakeCertificateManagerConfig()
	missingDestCertKeyCfg.destCertKeyFunc = nil

	missingCertDirCfg := newFakeCertificateManagerConfig()
	missingCertDirCfg.certDir = ""

	missingCACertDirCfg := newFakeCertificateManagerConfig()
	missingCACertDirCfg.caCertDir = ""

	matchingCertDirCfg := newFakeCertificateManagerConfig()
	matchingCertDirCfg.caCertDir = matchingCertDirCfg.certDir

	testCases := map[string]struct {
		config     *certificateManagerConfig
		shouldPass bool
	}{
		"valid":                                    {shouldPass: true, config: validCfg},
		"missing certificateKeyFunc":               {shouldPass: false, config: missingCertKeyCfg},
		"missing caCertificateKeyFunc":             {shouldPass: false, config: missingCACertKeyCfg},
		"missing destCertificateKeyFunc":           {shouldPass: false, config: missingDestCertKeyCfg},
		"missing 	certificateDir":                  {shouldPass: false, config: missingCertDirCfg},
		"missing caCertificateDir":                 {shouldPass: false, config: missingCACertDirCfg},
		"matching certificateDir/caCertificateDir": {shouldPass: false, config: matchingCertDirCfg},
	}

	fakeCertWriter := &fakeCertWriter{}
	for k, tc := range testCases {
		_, err := newSimpleCertificateManager(tc.config, fakeCertWriter)
		if tc.shouldPass && err != nil {
			t.Errorf("%s expected config to pass validation but failed with err: %v", k, err)
		}
		if !tc.shouldPass && err == nil {
			t.Errorf("%s expected config to fail validation but passed", k)
		}
	}
}

// TestWriteCertificateAtomicity verifies that WriteCertificate's temp+rename
// approach prevents concurrent readers from observing truncated or empty PEM
// files. A writer goroutine repeatedly overwrites the cert while a reader
// goroutine checks that the file is never empty or truncated.
func TestWriteCertificateAtomicity(t *testing.T) {
	dir := t.TempDir()
	writer := &simpleCertificateWriter{}

	certA := []byte("-----BEGIN RSA PRIVATE KEY-----\nAAAAAAAAAAAAAAAA\n-----END RSA PRIVATE KEY-----\n-----BEGIN CERTIFICATE-----\nBBBBBBBBBBBBBBBB\n-----END CERTIFICATE-----\n")
	certB := []byte("-----BEGIN RSA PRIVATE KEY-----\nCCCCCCCCCCCCCCCC\n-----END RSA PRIVATE KEY-----\n-----BEGIN CERTIFICATE-----\nDDDDDDDDDDDDDDDD\n-----END CERTIFICATE-----\n")

	// Seed the file so there is always something to read.
	if err := writer.WriteCertificate(dir, "test", certA); err != nil {
		t.Fatalf("initial write failed: %v", err)
	}

	const iterations = 2000
	var truncatedReads atomic.Int64
	var emptyReads atomic.Int64
	var totalReads atomic.Int64

	var wg sync.WaitGroup

	// Writer goroutine: alternates between certA and certB.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			cert := certA
			if i%2 == 1 {
				cert = certB
			}
			if err := writer.WriteCertificate(dir, "test", cert); err != nil {
				t.Errorf("WriteCertificate iteration %d: %v", i, err)
				return
			}
		}
	}()

	// Reader goroutine: reads the PEM file and checks for truncation.
	wg.Add(1)
	go func() {
		defer wg.Done()
		pemPath := filepath.Join(dir, "test.pem")
		for i := 0; i < iterations*2; i++ {
			data, err := os.ReadFile(pemPath)
			if err != nil {
				continue
			}
			totalReads.Add(1)
			if len(data) == 0 {
				emptyReads.Add(1)
			} else if len(data) < len(certA) && len(data) < len(certB) {
				truncatedReads.Add(1)
			}
		}
	}()

	wg.Wait()

	t.Logf("total reads: %d, empty: %d, truncated: %d",
		totalReads.Load(), emptyReads.Load(), truncatedReads.Load())

	if emptyReads.Load() > 0 || truncatedReads.Load() > 0 {
		t.Errorf("observed %d empty and %d truncated reads out of %d total — "+
			"WriteCertificate must use atomic temp+rename to prevent HAProxy from reading partial PEM files during reload",
			emptyReads.Load(), truncatedReads.Load(), totalReads.Load())
	}
}
