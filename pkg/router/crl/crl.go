package crl

import (
	"bytes"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	logf "github.com/openshift/router/log"
	"github.com/openshift/router/pkg/util"
	kerrors "k8s.io/apimachinery/pkg/util/errors"
)

var log = logf.Logger.WithName("crl")

const (
	// crlFilePermissions is the permission bits used for the CRL file.
	crlFilePermissions = 0644
	// stagingDirPermissions is the permission bits used for staging directories
	stagingDirPermissions = 0755
	// crlFallbackTime is how long to wait before retrying if nextUpdate can't be determined.
	crlFallbackTime = 5 * time.Minute
	// errorBackoffTime is how long to wait before retrying if a generic error happens during CRL refresh.
	errorBackoffTime = 5 * time.Minute
	// mtlsBaseDirectory is the directory where all crl temp directories and symlinks will live.
	mtlsBaseDirectory = "/var/lib/haproxy/mtls"
	// crlBasename is the name of the crl file
	crlBasename = "crls.pem"
	// caBundleBasename is the name of the CA bundle file copied from the CA bundle configmap.
	caBundleBasename = "ca-bundle.pem"
	// dummyCRL is a placeholder CRL so that HAProxy can start serving non-mTLS traffic while CRLs are downloaded. This
	// CRL is for a CA cert/key that are intentionally not included, and was generated with an expiration (nextUpdate)
	// at 1:00AM GMT on Jan 1, 2000 so that in the extremely unlikely case that someone is able to generate a matching
	// cert and key, HAProxy will still reject the connection due to the expired CRL.
	dummyCRL = `-----BEGIN X509 CRL-----
MIIBzzCBuAIBATANBgkqhkiG9w0BAQsFADBhMQswCQYDVQQGEwJVUzELMAkGA1UE
CAwCTkMxEDAOBgNVBAcMB1JhbGVpZ2gxDDAKBgNVBAoMA09TNDEMMAoGA1UECwwD
RW5nMRcwFQYDVQQDDA5QbGFjZWhvbGRlciBDQRcNMDAwMTAxMDAwMDAwWhcNMDAw
MTAxMDEwMDAwWqAjMCEwHwYDVR0jBBgwFoAUbaqyU8VAswFgefsu6pOdvqK1nfgw
DQYJKoZIhvcNAQELBQADggEBAD8W+OmWHp9Pg7914rA5QOk+pUCZ4F7++fbmGPpc
9gdNOxkeCrZ3sdBeEs0P3+tSf8dLcpI5PKEbL+bC3wrIM3yzsD+mIZkvV/FGhgE1
s7b6IA/8FYsmNWIjgAWBAp13zh0AH3qhpI01tm+cQETz6r249TWQ+p04pEA89+XT
7CE99nHd8yNDOESs1xZreSFkIF/Hmm8y4I0o/+8wpjA9e3PJ7O25ZB2OGX4FufMf
tVa0xfWd9czWFqM1DjU3ME0mVi6lr38AhUDoG6sFbHk+TfzTp4ykVUpXIHu4bJTG
DPfV3SE277EvsrsGFYIsxWgXskITjzb9no9fnodd/jG46tw=
-----END X509 CRL-----
`
)

var (
	// mtlsLatestSymlink is the fully qualified path to the symlink used in the template.
	mtlsLatestSymlink = filepath.Join(mtlsBaseDirectory, "latest")
	// mtlsNextSymlink is the fully qualified path to the staging symlink, used to atomically replace the existing
	// mtlsLatestSymlink.
	mtlsNextSymlink = filepath.Join(mtlsBaseDirectory, "next")
	// CRLFilename is the fully qualified path to the currently in use crl file.
	CRLFilename = filepath.Join(mtlsLatestSymlink, crlBasename)
	// CABundleFilename is the fully qualified path to the currently in use CA bundle.
	CABundleFilename = filepath.Join(mtlsLatestSymlink, caBundleBasename)
	// crlsUpdated is true when all CRLs have been successfully updated, and false when there are missing CRLs.
	crlsUpdated = false
	crlsMutex   = sync.Mutex{}
)

// authorityKeyIdentifier is a certificate's authority key identifier.
type authorityKeyIdentifier struct {
	KeyIdentifier []byte `asn1:"optional,tag:0"`
}

// authorityKeyIdentifierOID is the ASN.1 object identifier for the authority key identifier extension.
var authorityKeyIdentifierOID = asn1.ObjectIdentifier{2, 5, 29, 35}

// InitMTLSDirectory creates an initial directory for HAProxy to use to complete startup and serve non-mTLS traffic
// while CRLs are being downloaded in the background. Returns an error if any of the filesystem operations fail.
func InitMTLSDirectory(caBundleFilename string) error {
	stagingDirectory, err := makeStagingDirectory()
	if err != nil {
		return err
	}
	// Copy the CA bundle from the configmap mount location to the staging directory
	if err := util.CopyFile(caBundleFilename, filepath.Join(stagingDirectory, caBundleBasename)); err != nil {
		return err
	}
	// Write out the dummyCRL as a placeholder. With this, HAProxy will reject connections that require CRLs until after
	// all CRL downloads are complete, but other traffic should be handled correctly.
	if err := os.WriteFile(filepath.Join(stagingDirectory, crlBasename), []byte(dummyCRL), crlFilePermissions); err != nil {
		return err
	}
	// At any time other than startup, we need to be careful overwrite the previous mtlsLatestSymlink in an atomic way,
	// but at initialization, mtlsLatestSymlink shouldn't exist yet, so it should be safe to just directly create the
	// symlink.
	if err := os.Symlink(stagingDirectory, mtlsLatestSymlink); err != nil {
		return err
	}
	return nil
}

// CABundleHasCRLs returns true if any of the certificates in caBundleFilename specify a CRL distribution point.
// Returns an error if the CA Bundle could not be parsed.
func CABundleHasCRLs(caBundleFilename string) (bool, error) {
	clientCAData, err := os.ReadFile(caBundleFilename)
	if err != nil {
		return false, err
	}
	for len(clientCAData) > 0 {
		block, data := pem.Decode(clientCAData)
		if block == nil {
			break
		}
		clientCAData = data
		if block.Type != "CERTIFICATE" {
			log.Info("found non-certificate data in client CA bundle. skipping.", "type", block.Type)
			continue
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return false, fmt.Errorf("client CA bundle has an invalid certificate: %w", err)
		}
		if len(cert.CRLDistributionPoints) != 0 {
			return true, nil
		}
	}
	return false, nil
}

// ManageCRLs spins off a goroutine that ensures that any CRLs specified in caBundleFilename are downloaded and kept
// up-to-date. It will automatically refresh expired CRLs and download missing CRLs when it receives a message on
// caUpdateChannel (indicating the CA bundle has been updated), or when any existing CRL expires. Whenever either the CA
// bundle or the CRL file has changed, updateCallback is called, with a boolean indicating whether crl-file needs to be
// specified in the HAProxy config.
func ManageCRLs(caBundleFilename string, caUpdateChannel <-chan struct{}, updateCallback func(bool)) {
	go func() {
		caUpdated := false
		nextUpdate := time.Now()
		shouldHaveCRLs, err := CABundleHasCRLs(caBundleFilename)
		if err != nil {
			log.Error(err, "failed to parse CA bundle", "CA bundle filename", caBundleFilename)
			nextUpdate = time.Now().Add(errorBackoffTime)
		}
		if !shouldHaveCRLs {
			SetCRLsUpdated(true)
		}
		for {
			updated := false
			if nextUpdate.IsZero() {
				log.V(4).Info("no nextUpdate. only watching for CA updates")
				select {
				case <-caUpdateChannel:
					SetCRLsUpdated(false)
					caUpdated = true
				}
			} else {
				log.V(4).Info("nextUpdate is at " + nextUpdate.Format(time.RFC3339))
				select {
				case <-time.After(time.Until(nextUpdate)):
				case <-caUpdateChannel:
					SetCRLsUpdated(false)
					caUpdated = true
				}
			}

			if caUpdated {
				shouldHaveCRLs, err = CABundleHasCRLs(caBundleFilename)
				if err != nil {
					log.Error(err, "failed to parse CA bundle", "CA bundle filename", caBundleFilename)
					nextUpdate = time.Now().Add(errorBackoffTime)
					continue
				}
			}

			nextUpdate, updated, err = updateCRLFile(caBundleFilename, caUpdated)
			if err != nil {
				log.Error(err, "failed to update CRLs")
				nextUpdate = time.Now().Add(errorBackoffTime)
				continue
			}
			// After successfully updating the CRL file, reset caUpdated and mark CRLs as updated
			caUpdated = false
			SetCRLsUpdated(true)
			if updated {
				updateCallback(shouldHaveCRLs)
			}
		}
	}()
}

// updateCRLFile creates a new staging directory, updates CRLs, and updates mtlsLatestSymlink to point to the new
// staging directory. Returns the next update time and a boolean for if anything changed. Returns an error if there was
// an issue during the update.
func updateCRLFile(caBundleFilename string, caUpdated bool) (time.Time, bool, error) {
	stagingDirectory, err := makeStagingDirectory()
	if err != nil {
		log.Error(err, "failed to create staging directory")
		return time.Time{}, false, err
	}

	defer reapStaleDirectories()

	stagingCRLFilename := filepath.Join(stagingDirectory, crlBasename)

	nextUpdate, crlsUpdated, err := writeCRLFile(caBundleFilename, CRLFilename, stagingCRLFilename)
	if err != nil {
		log.Error(err, "failed to update CRLs")
		return time.Time{}, false, err
	}

	if caUpdated || crlsUpdated {
		if err := commitCACRLUpdate(stagingDirectory, caBundleFilename, crlsUpdated); err != nil {
			log.Error(err, "failed to commit CRL update")
			return time.Time{}, false, err
		}
		return nextUpdate, true, nil
	}
	return nextUpdate, false, nil
}

// commitCACRLUpdate makes sure stagingDirectory contains up-to-date versions of both the CA bundle and the CRL file,
// then updates the symlink so that HAProxy will reference the new versions on reload. Returns an error if any of the
// file operations fail.
func commitCACRLUpdate(stagingDirectory, caBundleFilename string, stagingCRLUpdated bool) error {
	// Copy CA bundle to the new directory.
	if err := util.CopyFile(caBundleFilename, filepath.Join(stagingDirectory, caBundleBasename)); err != nil {
		return err
	}
	// If stagingCRLUpdated is true, then the CRL file has already been written to the new directory, so nothing needs
	// to be done for it. However, if stagingCRLUpdated is false, then we need to copy the existing crl file to the new
	// directory.
	if !stagingCRLUpdated {
		if err := util.CopyFile(CRLFilename, filepath.Join(stagingDirectory, crlBasename)); err != nil {
			if os.IsNotExist(err) {
				// Even if CRLFilename doesn't currently exist, a crl file may need to be supplied to HAProxy when this
				// update is committed. Using the dummyCRL allows http traffic to be handled as normal, but essentially
				// guarantees that any mTLS connections will fail until new CRLs are downloaded.
				if err := os.WriteFile(filepath.Join(stagingDirectory, crlBasename), []byte(dummyCRL), crlFilePermissions); err != nil {
					return err
				}
			} else {
				return err
			}
		}
	}

	// os.Symlink() will return an error if a file exists with the same name, so to avoid that, create a symlink with a
	// temporary name, then use os.Rename() to replace any existing file with the name we actually want.

	// Remove the staging symlink if it exists. It should not exist at all if the last commitCACRLUpdate call was
	// successful, and should never be referred to outside of this function, so it should be safe to remove.
	if err := os.Remove(mtlsNextSymlink); err != nil && !os.IsNotExist(err) {
		// Successfully removing mtlsNextSymlink and receiving ErrNotExist are both acceptible; we just need
		// mtlsNextSymlink to not be there. However, any other error should be returned.
		return err
	}
	if err := os.Symlink(stagingDirectory, mtlsNextSymlink); err != nil {
		return err
	}
	if err := os.Rename(mtlsNextSymlink, mtlsLatestSymlink); err != nil {
		return err
	}
	return nil
}

var existingCRLs map[string]*pkix.CertificateList

// writeCRLFile reads the CA bundle at caBundleFilename, and makes sure all CRLs specified in the CA bundle are written
// into the crl file at newCRLFilename. If any of the specified CRLs are in existingCRLFilename and have not expired,
// writeCRLFile will prefer to use those over downloading them again from their distribution points.
//
// Returns the time of the next CRL expiration (zero if no CRLs are in use), and whether or not the CRL file was
// updated. Returns an error if parsing data, encoding data, or a file operation fails.
func writeCRLFile(caBundleFilename, existingCRLFilename, newCRLFilename string) (time.Time, bool, error) {
	clientCAData, err := os.ReadFile(caBundleFilename)
	if err != nil {
		return time.Time{}, false, err
	}

	crls, nextCRLUpdate, updated, err := downloadMissingCRLs(existingCRLs, clientCAData)
	if err != nil {
		return time.Time{}, false, err
	}

	existingCRLs = crls

	if len(crls) == 0 {
		// If there are no CRLs, still write out dummyCRL as a placeholder.
		return time.Time{}, updated, os.WriteFile(newCRLFilename, []byte(dummyCRL), crlFilePermissions)
	}

	// If any CRLs changed, encode the CRLs and write to newCRLFilename.
	if !updated {
		return nextCRLUpdate, updated, nil
	}

	buf := &bytes.Buffer{}
	for subjectKeyId, crl := range crls {
		asn1Data, err := asn1.Marshal(*crl)
		if err != nil {
			return time.Time{}, false, fmt.Errorf("failed to encode ASN.1 for CRL for certificate key %s: %w", subjectKeyId, err)
		}
		block := &pem.Block{
			Type:  "X509 CRL",
			Bytes: asn1Data,
		}
		if err := pem.Encode(buf, block); err != nil {
			return time.Time{}, false, fmt.Errorf("failed to encode PEM for CRL for certificate key %s: %w", subjectKeyId, err)
		}
	}

	if err := os.WriteFile(newCRLFilename, buf.Bytes(), crlFilePermissions); err != nil {
		return time.Time{}, false, err
	}

	return nextCRLUpdate, updated, nil
}

// downloadMissingCRLs parses the certificates in the CA bundle, clientCAData, and returns a map of all CRLs that were
// specified. downloadMissingCRLs will prefer to use CRLs from existingCRLs if they are still valid, but otherwise, CRLs
// are downloaded from the distribution points from the CA bundle.
//
// Returns:
//   - a map of all CRLs keyed by their subject key ID
//   - the time at which the next CRL will expire, or a fallback time if any CRLs have already expired. If clientCAData
//     specifies no CRL distribution points, this time will be zero.
//   - whether the crl map has been updated, either because new CRLs were downloaded, or because some CRLs in
//     existingCRLs are no longer required
//
// Returns an error if CRL downloading or parsing fails.
func downloadMissingCRLs(existingCRLs map[string]*pkix.CertificateList, clientCAData []byte) (map[string]*pkix.CertificateList, time.Time, bool, error) {
	var nextCRLUpdate time.Time
	crls := make(map[string]*pkix.CertificateList)
	updated := false
	now := time.Now()
	for len(clientCAData) > 0 {
		block, data := pem.Decode(clientCAData)
		if block == nil {
			break
		}
		clientCAData = data
		if block.Type != "CERTIFICATE" {
			log.Info("found non-certificate data in client CA bundle. skipping.", "type", block.Type)
			continue
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return nil, time.Time{}, false, fmt.Errorf("client CA bundle has an invalid certificate: %w", err)
		}
		subjectKeyId := hex.EncodeToString(cert.SubjectKeyId)
		if len(cert.CRLDistributionPoints) == 0 {
			continue
		}
		if crl, ok := existingCRLs[subjectKeyId]; ok {
			if crl.TBSCertList.NextUpdate.Before(now) {
				log.Info("certificate revocation list has expired", "subject key identifier", subjectKeyId, "next update", crl.TBSCertList.NextUpdate.Format(time.RFC3339))
			} else {
				crls[subjectKeyId] = existingCRLs[subjectKeyId]
				if nextCRLUpdate.IsZero() || crl.TBSCertList.NextUpdate.Before(nextCRLUpdate) {
					nextCRLUpdate = crl.TBSCertList.NextUpdate
				}
				continue
			}
		}
		log.Info("retrieving certificate revocation list", "subject key identifier", subjectKeyId)
		if crl, err := getCRL(cert.CRLDistributionPoints, now); err != nil {
			// Creating or updating the crl file with incomplete data would compromise security by potentially
			// permitting revoked certificates.
			return nil, time.Time{}, false, fmt.Errorf("failed to get certificate revocation list for certificate key %s: %w", subjectKeyId, err)
		} else {
			crls[subjectKeyId] = crl
			log.Info("new certificate revocation list", "subject key identifier", subjectKeyId, "next update", crl.TBSCertList.NextUpdate.Format(time.RFC3339))
			if nextCRLUpdate.IsZero() || crl.TBSCertList.NextUpdate.Before(nextCRLUpdate) {
				nextCRLUpdate = crl.TBSCertList.NextUpdate
			}
			updated = true
		}
	}
	// If updated is still false, no new CRLs have been downloaded, but it's possible that some existing CRLs are no
	// longer necessary. If that's the case, then existingCRLs will contain more items than crls, so we can compare
	// their lengths to determine if an update is necessary.
	updated = updated || (len(existingCRLs) != len(crls))
	// If nextCRLUpdate is non-zero but is still in the past, that means at least one CRL has already expired, but a
	// non-expired version wasn't able to be downloaded. If that's the case, use the fallback time for the nextCRLUpdate
	// time instead.
	if !nextCRLUpdate.IsZero() && nextCRLUpdate.Before(now) {
		nextCRLUpdate = now.Add(crlFallbackTime)
	}
	return crls, nextCRLUpdate, updated, nil
}

// getCRL gets a certificate revocation list using the provided distribution points and returns the certificate list.
// Returns an error if the CRL could not be downloaded.
func getCRL(distributionPoints []string, now time.Time) (*pkix.CertificateList, error) {
	var errs []error
	for _, distributionPoint := range distributionPoints {
		// The distribution point is typically a URL with the "http" scheme.  "https" is generally not used because the
		// certificate list is signed, and because using TLS to get the certificate list could introduce a circular
		// dependency (cannot use TLS without the revocation list, and cannot get the revocation list without using
		// TLS).
		//
		// TODO Support ldap.
		switch {
		case strings.HasPrefix(distributionPoint, "http:"):
			log.Info("retrieving CRL distribution point", "distribution point", distributionPoint)
			crl, err := getHTTPCRL(distributionPoint)
			if err != nil {
				errs = append(errs, fmt.Errorf("error getting %q: %w", distributionPoint, err))
				continue
			}
			if crl.TBSCertList.NextUpdate.Before(now) {
				log.Info("CRL expired. trying next distribution point", "nextUpdate", crl.TBSCertList.NextUpdate.Format(time.RFC3339))
				errs = append(errs, fmt.Errorf("retrieved expired CRL from %s", distributionPoint))
				continue
			}
			return crl, nil
		default:
			errs = append(errs, fmt.Errorf("unsupported distribution point type: %s", distributionPoint))
		}
	}
	log.Info("failed to get valid CRL after trying all distribution points")
	return nil, kerrors.NewAggregate(errs)
}

// getHTTPCRL gets a certificate revocation list using the provided HTTP URL. Returns an error if the CRL could not be
// downloaded, or if parsing the CRL fails.
func getHTTPCRL(url string) (*pkix.CertificateList, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("http.Get failed: %w", err)
	}
	defer resp.Body.Close()
	// If http.Get returned anything other than 200 OK, we can't rely on the response body to actually be a CRL. Return
	// an error with the status code rather than failing at the parsing stage.
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("got unexpected status %s", resp.Status)
	}
	crlBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("error reading response: %w", err)
	}
	// Try to decode the CRL from PEM to DER. If pemBlock comes back nil, assume the CRL was already in DER format, and
	// try to parse anyway.
	pemBlock, _ := pem.Decode(crlBytes)
	if pemBlock != nil {
		if pemBlock.Type == "X509 CRL" {
			crlBytes = pemBlock.Bytes
		} else {
			return nil, fmt.Errorf("error parsing response: file is not CRL type")
		}
	}
	crl, err := x509.ParseCRL(crlBytes)
	if err != nil {
		return nil, fmt.Errorf("error parsing response: %w", err)
	}
	return crl, nil
}

// reapStaleDirectories deletes any subdirectories of mtlsBaseDirectory that are no longer necessary
func reapStaleDirectories() {
	log.V(4).Info("cleaning up stale mtls files...")
	// list all entries in the base directory.
	dirEntries, err := os.ReadDir(mtlsBaseDirectory)
	if err != nil {
		log.Error(err, "Failed to read directory", "directory", mtlsBaseDirectory)
		return
	}
	// The base directory should only contain 2 entries: the latest mTLS files, and the "latest" symlink. If it only
	// contains 2 items, assume it's those two, so nothing needs to be done.
	if len(dirEntries) <= 2 {
		log.V(2).Info("no stale directories")
		return
	}
	// Get the name of the directory that mtlsLatestSymlink points to so we don't delete it. If os.Readlink() returns an
	// error, it'll be because mtlsLatestSymlink doesn't exist. That shouldn't happen, but if it does, we can't
	// determine what we need to keep, so just exit and hope it's fixed in a future run.
	inUseDirName, err := os.Readlink(mtlsLatestSymlink)
	if err != nil {
		log.V(1).Error(err, "failed to read symlink to determine which files to clean", "name", mtlsLatestSymlink)
		return
	}
	// Walk through all entries in mtlsBaseDirectory. Only 2 things need to stay: mtlsLatestSymlink, and the directory
	// that mtlsLatestSymlink points to.  Everything else can be deleted.
	for _, dirEntry := range dirEntries {
		entryName := dirEntry.Name()
		// entryName is the bare file/directory name, but mtlsLatestSymlink and inUseDirName are fully-qualified paths.
		// Since we know that all the directory entries are the files/directories in mtlsBaseDirectory, we can prepend
		// that to entryName to get the full name to compare against.
		fullPath := filepath.Join(mtlsBaseDirectory, entryName)
		log.V(4).Info("checking for staleness", "filename", fullPath)
		// Leave mtlsLatestSymlink and the current directory pointed to by mtlsLatestSymlink alone.
		if fullPath == mtlsLatestSymlink || fullPath == inUseDirName {
			continue
		}
		log.V(4).Info("removing stale file", "path", fullPath)
		if err := os.RemoveAll(fullPath); err != nil {
			log.Error(err, "failed to remove stale file or directory", "path", fullPath)
		}
	}
	log.V(4).Info("cleanup done")
}

// makeStagingDirectory creates a new staging directory, with the name format
// mtls-[year][month][day]-[hour][minute][second], so that directories are unique but still sortable for debugging
// purposes. The exact directory name is returned. Returns an error if the directory could not be created.
func makeStagingDirectory() (string, error) {
	stagingDirName := filepath.Join(mtlsBaseDirectory, fmt.Sprintf("mtls-%s", time.Now().Format("20060102-150405")))
	if err := os.MkdirAll(stagingDirName, stagingDirPermissions); err != nil {
		return "", err
	}
	return stagingDirName, nil
}

func GetCRLsUpdated() bool {
	crlsMutex.Lock()
	defer crlsMutex.Unlock()
	return crlsUpdated
}

func SetCRLsUpdated(value bool) {
	crlsMutex.Lock()
	defer crlsMutex.Unlock()
	crlsUpdated = value
}
