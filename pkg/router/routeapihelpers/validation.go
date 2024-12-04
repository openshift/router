package routeapihelpers

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"

	"k8s.io/apiserver/pkg/authentication/user"
	"k8s.io/client-go/util/cert"

	routev1 "github.com/openshift/api/route/v1"
	"github.com/openshift/library-go/pkg/authorization/authorizationutil"

	authorizationv1 "k8s.io/api/authorization/v1"
	kapi "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/validation/field"
	authorizationclient "k8s.io/client-go/kubernetes/typed/authorization/v1"
	corev1client "k8s.io/client-go/kubernetes/typed/core/v1"
)

const (
	// routerServiceAccount is used to validate RBAC permissions for externalCertificate
	// TODO: avoid hard coding the serviceaccount name, and instead use environment variable, may be through cluster-ingress-operator.
	routerServiceAccount = "system:serviceaccount:openshift-ingress:router"
)

type blockVerifierFunc func(block *pem.Block) (*pem.Block, error)

func publicKeyBlockVerifier(block *pem.Block) (*pem.Block, error) {
	key, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	block = &pem.Block{
		Type: "PUBLIC KEY",
	}
	if block.Bytes, err = x509.MarshalPKIXPublicKey(key); err != nil {
		return nil, err
	}
	return block, nil
}

func certificateBlockVerifier(block *pem.Block) (*pem.Block, error) {
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, err
	}
	block = &pem.Block{
		Type:  "CERTIFICATE",
		Bytes: cert.Raw,
	}
	return block, nil
}

func privateKeyBlockVerifier(block *pem.Block) (*pem.Block, error) {
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		key, err = x509.ParsePKCS1PrivateKey(block.Bytes)
		if err != nil {
			key, err = x509.ParseECPrivateKey(block.Bytes)
			if err != nil {
				return nil, fmt.Errorf("block %s is not valid", block.Type)
			}
		}
	}
	switch t := key.(type) {
	case *rsa.PrivateKey:
		block = &pem.Block{
			Type:  "RSA PRIVATE KEY",
			Bytes: x509.MarshalPKCS1PrivateKey(t),
		}
	case *ecdsa.PrivateKey:
		block = &pem.Block{
			Type: "EC PRIVATE KEY",
		}
		if block.Bytes, err = x509.MarshalECPrivateKey(t); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("block private key %T is not valid", key)
	}
	return block, nil
}

func ignoreBlockVerifier(block *pem.Block) (*pem.Block, error) {
	return nil, nil
}

var knownBlockDecoders = map[string]blockVerifierFunc{
	"RSA PRIVATE KEY":   privateKeyBlockVerifier,
	"ECDSA PRIVATE KEY": privateKeyBlockVerifier,
	"EC PRIVATE KEY":    privateKeyBlockVerifier,
	"PRIVATE KEY":       privateKeyBlockVerifier,
	"PUBLIC KEY":        publicKeyBlockVerifier,
	// Potential "in the wild" PEM encoded blocks that can be normalized
	"RSA PUBLIC KEY":   publicKeyBlockVerifier,
	"DSA PUBLIC KEY":   publicKeyBlockVerifier,
	"ECDSA PUBLIC KEY": publicKeyBlockVerifier,
	"CERTIFICATE":      certificateBlockVerifier,
	// Blocks that should be dropped
	"EC PARAMETERS": ignoreBlockVerifier,
}

// sanitizePEM takes a block of data that should be encoded in PEM and returns only
// the parts of it that parse and serialize as valid recognized certs in valid PEM blocks.
// We perform this transformation to eliminate potentially incorrect / invalid PEM contents
// to prevent OpenSSL or other non Golang tools from receiving unsanitized input.
func sanitizePEM(data []byte) ([]byte, error) {
	var block *pem.Block
	buf := &bytes.Buffer{}
	for len(data) > 0 {
		block, data = pem.Decode(data)
		if block == nil {
			return buf.Bytes(), nil
		}
		fn, ok := knownBlockDecoders[block.Type]
		if !ok {
			return nil, fmt.Errorf("unrecognized PEM block %s", block.Type)
		}
		newBlock, err := fn(block)
		if err != nil {
			return nil, err
		}
		if newBlock == nil {
			continue
		}
		if err := pem.Encode(buf, newBlock); err != nil {
			return nil, err
		}
	}
	return buf.Bytes(), nil
}

// splitCertKey takes a slice of bytes containing sanitized PEM data and returns
// two slices of bytes containing PEM data: one slice with the public
// certificate block or blocks from the input PEM data and one slice with any
// private key blocks.
func splitCertKey(data []byte) ([]byte, []byte, error) {
	var block *pem.Block
	publicBuf := &bytes.Buffer{}
	privateBuf := &bytes.Buffer{}
	for len(data) > 0 {
		block, data = pem.Decode(data)
		if block == nil {
			break
		}
		// Because data is sanitized PEM data, the following switch only
		// needs cases for the block types that sanitizePEM produces.
		switch block.Type {
		case "PUBLIC KEY", "CERTIFICATE":
			if err := pem.Encode(publicBuf, block); err != nil {
				return nil, nil, err
			}
		case "EC PRIVATE KEY", "RSA PRIVATE KEY":
			if err := pem.Encode(privateBuf, block); err != nil {
				return nil, nil, err
			}
		}
	}
	return publicBuf.Bytes(), privateBuf.Bytes(), nil
}

// ExtendedValidateRoute performs an extended validation on the route
// including checking that the TLS config is valid. It also sanitizes
// the contents of valid certificates by removing any data that
// is not recognizable PEM blocks on the incoming route.
func ExtendedValidateRoute(route *routev1.Route) field.ErrorList {
	tlsConfig := route.Spec.TLS
	result := field.ErrorList{}

	if tlsConfig == nil {
		return result
	}

	tlsFieldPath := field.NewPath("spec").Child("tls")
	if errs := validateTLS(route, tlsFieldPath); len(errs) != 0 {
		result = append(result, errs...)
	}

	// TODO: Check if we can be stricter with validating the certificate
	//       is for the route hostname. Don't want existing routes to
	//       break, so disable the hostname validation for now.
	// hostname := route.Spec.Host
	hostname := ""
	var verifyOptions *x509.VerifyOptions

	if len(tlsConfig.CACertificate) > 0 {
		certPool := x509.NewCertPool()
		if certs, err := cert.ParseCertsPEM([]byte(tlsConfig.CACertificate)); err != nil {
			errmsg := fmt.Sprintf("failed to parse CA certificate: %v", err)
			result = append(result, field.Invalid(tlsFieldPath.Child("caCertificate"), "redacted ca certificate data", errmsg))
		} else {
			for _, cert := range certs {
				certPool.AddCert(cert)
			}
			if data, err := sanitizePEM([]byte(tlsConfig.CACertificate)); err != nil {
				result = append(result, field.Invalid(tlsFieldPath.Child("caCertificate"), "redacted ca certificate data", err.Error()))
			} else {
				tlsConfig.CACertificate = string(data)
			}
			// HAProxy will fail to start if intermediate CA certs use unsupported signature algorithms.
			// However, root CAs can still use unsupported algorithms since they are self-signed.
			if err := validateCertSignatureAlgorithms(certs); err != nil {
				result = append(result, field.Invalid(tlsFieldPath.Child("caCertificate"), "redacted ca certificate data", err.Error()))
			}
		}

		verifyOptions = &x509.VerifyOptions{
			DNSName:       hostname,
			Intermediates: certPool,
			Roots:         certPool,
		}
	}

	if len(tlsConfig.Certificate) > 0 {
		// validateCertificatePEM calls validateCertSignatureAlgorithms for check for unsupported signature algorithms.
		if _, err := validateCertificatePEM(tlsConfig.Certificate, verifyOptions); err != nil {
			result = append(result, field.Invalid(tlsFieldPath.Child("certificate"), "redacted certificate data", err.Error()))
		} else {
			if data, err := sanitizePEM([]byte(tlsConfig.Certificate)); err != nil {
				result = append(result, field.Invalid(tlsFieldPath.Child("certificate"), "redacted certificate data", err.Error()))
			} else {
				tlsConfig.Certificate = string(data)
			}
		}
	}

	if len(tlsConfig.Key) > 0 {
		if data, err := sanitizePEM([]byte(tlsConfig.Key)); err != nil {
			result = append(result, field.Invalid(tlsFieldPath.Child("key"), "redacted key data", err.Error()))
		} else {
			tlsConfig.Key = string(data)
		}
	}

	if len(tlsConfig.Certificate) > 0 {
		if certBytes, keyBytes, err := splitCertKey([]byte(tlsConfig.Certificate)); err != nil {
			result = append(result, field.Invalid(tlsFieldPath.Child("certificate"), "redacted key data", err.Error()))
		} else {
			// Use any private key that was found in either
			// tlsConfig.Certificate or tlsConfig.Key.
			keyBytes = append(keyBytes, []byte(tlsConfig.Key)...)
			if len(keyBytes) == 0 {
				result = append(result, field.Invalid(tlsFieldPath.Child("key"), "", "no key specified"))
			} else {
				if _, err := tls.X509KeyPair(certBytes, keyBytes); err != nil {
					result = append(result, field.Invalid(tlsFieldPath.Child("key"), "redacted key data", err.Error()))
				} else {
					tlsConfig.Certificate, tlsConfig.Key = string(certBytes), string(keyBytes)
				}
			}
		}
	}

	if len(tlsConfig.DestinationCACertificate) > 0 {
		if certs, err := cert.ParseCertsPEM([]byte(tlsConfig.DestinationCACertificate)); err != nil {
			errmsg := fmt.Sprintf("failed to parse destination CA certificate: %v", err)
			result = append(result, field.Invalid(tlsFieldPath.Child("destinationCACertificate"), "redacted destination ca certificate data", errmsg))
		} else {
			if data, err := sanitizePEM([]byte(tlsConfig.DestinationCACertificate)); err != nil {
				result = append(result, field.Invalid(tlsFieldPath.Child("destinationCACertificate"), "redacted destination ca certificate data", err.Error()))
			} else {
				tlsConfig.DestinationCACertificate = string(data)
			}
			// Unsupported destinationCACertificates algorithms won't prevent HAProxy from starting.
			// However, HAProxy will quietly refuse to use them at runtime. Rejecting here improves UX.
			if err := validateCertSignatureAlgorithms(certs); err != nil {
				result = append(result, field.Invalid(tlsFieldPath.Child("destinationCACertificate"), "redacted destination ca certificate data", err.Error()))
			}
		}
	}

	return result
}

// validateTLS tests fields for different types of TLS combinations are set.  Called
// by ValidateRoute.
func validateTLS(route *routev1.Route, fldPath *field.Path) field.ErrorList {
	result := field.ErrorList{}
	tls := route.Spec.TLS

	// no tls config present, no need for validation
	if tls == nil {
		return nil
	}

	switch tls.Termination {
	// reencrypt may specify destination ca cert
	// cert, key, cacert may not be specified because the route may be a wildcard
	case routev1.TLSTerminationReencrypt:
		//passthrough term should not specify any cert
	case routev1.TLSTerminationPassthrough:
		if len(tls.Certificate) > 0 {
			result = append(result, field.Invalid(fldPath.Child("certificate"), "redacted certificate data", "passthrough termination does not support certificates"))
		}

		if len(tls.Key) > 0 {
			result = append(result, field.Invalid(fldPath.Child("key"), "redacted key data", "passthrough termination does not support certificates"))
		}

		if len(tls.CACertificate) > 0 {
			result = append(result, field.Invalid(fldPath.Child("caCertificate"), "redacted ca certificate data", "passthrough termination does not support certificates"))
		}

		if len(tls.DestinationCACertificate) > 0 {
			result = append(result, field.Invalid(fldPath.Child("destinationCACertificate"), "redacted destination ca certificate data", "passthrough termination does not support certificates"))
		}
		// edge cert should only specify cert, key, and cacert but those certs
		// may not be specified if the route is a wildcard route
	case routev1.TLSTerminationEdge:
		if len(tls.DestinationCACertificate) > 0 {
			result = append(result, field.Invalid(fldPath.Child("destinationCACertificate"), "redacted destination ca certificate data", "edge termination does not support destination certificates"))
		}
	default:
		validValues := []string{string(routev1.TLSTerminationEdge), string(routev1.TLSTerminationPassthrough), string(routev1.TLSTerminationReencrypt)}
		result = append(result, field.NotSupported(fldPath.Child("termination"), tls.Termination, validValues))
	}

	if err := validateInsecureEdgeTerminationPolicy(tls, fldPath.Child("insecureEdgeTerminationPolicy")); err != nil {
		result = append(result, err)
	}

	return result
}

// validateInsecureEdgeTerminationPolicy tests fields for different types of
// insecure options. Called by validateTLS.
func validateInsecureEdgeTerminationPolicy(tls *routev1.TLSConfig, fldPath *field.Path) *field.Error {
	// Check insecure option value if specified (empty is ok).
	if len(tls.InsecureEdgeTerminationPolicy) == 0 {
		return nil
	}

	// It is an edge-terminated or reencrypt route, check insecure option value is
	// one of None(for disable), Allow or Redirect.
	allowedValues := map[routev1.InsecureEdgeTerminationPolicyType]struct{}{
		routev1.InsecureEdgeTerminationPolicyNone:     {},
		routev1.InsecureEdgeTerminationPolicyAllow:    {},
		routev1.InsecureEdgeTerminationPolicyRedirect: {},
	}

	switch tls.Termination {
	case routev1.TLSTerminationReencrypt:
		fallthrough
	case routev1.TLSTerminationEdge:
		if _, ok := allowedValues[tls.InsecureEdgeTerminationPolicy]; !ok {
			msg := fmt.Sprintf("invalid value for InsecureEdgeTerminationPolicy option, acceptable values are %s, %s, %s, or empty", routev1.InsecureEdgeTerminationPolicyNone, routev1.InsecureEdgeTerminationPolicyAllow, routev1.InsecureEdgeTerminationPolicyRedirect)
			return field.Invalid(fldPath, tls.InsecureEdgeTerminationPolicy, msg)
		}
	case routev1.TLSTerminationPassthrough:
		if routev1.InsecureEdgeTerminationPolicyNone != tls.InsecureEdgeTerminationPolicy && routev1.InsecureEdgeTerminationPolicyRedirect != tls.InsecureEdgeTerminationPolicy {
			msg := fmt.Sprintf("invalid value for InsecureEdgeTerminationPolicy option, acceptable values are %s, %s, or empty", routev1.InsecureEdgeTerminationPolicyNone, routev1.InsecureEdgeTerminationPolicyRedirect)
			return field.Invalid(fldPath, tls.InsecureEdgeTerminationPolicy, msg)
		}
	}

	return nil
}

// isSelfSignedCert determines if a certificate is self-signed by verifying that the issuer matches the subject,
// the authority key identifier matches the subject key identifier, and the public key algorithm matches the
// signature algorithm. This logic mirrors the approach that OpenSSL uses to set the EXFLAG_SS flag, which
// indicates a certificate is self-signed.
// Ref: https://github.com/openssl/openssl/blob/b85e6f534906f0bf9114386d227e481d2336a0ff/crypto/x509/v3_purp.c#L557
func isSelfSignedCert(cert *x509.Certificate) bool {
	issuerIsEqualToSubject := bytes.Equal(cert.RawIssuer, cert.RawSubject)
	authorityKeyIsEqualToSubjectKey := bytes.Equal(cert.AuthorityKeyId, cert.SubjectKeyId)
	algorithmIsConsistent := signatureAlgorithmToPublicKeyAlgorithm(cert.SignatureAlgorithm) == cert.PublicKeyAlgorithm
	return issuerIsEqualToSubject &&
		(cert.AuthorityKeyId == nil || authorityKeyIsEqualToSubjectKey) &&
		algorithmIsConsistent
}

// signatureAlgorithmToPublicKeyAlgorithm maps a SignatureAlgorithm to its corresponding PublicKeyAlgorithm.
// Unfortunately, the x509 library does not expose a public mapping function for this.
// Returns UnknownPublicKeyAlgorithm if the mapping is not recognized.
func signatureAlgorithmToPublicKeyAlgorithm(sigAlgo x509.SignatureAlgorithm) x509.PublicKeyAlgorithm {
	switch sigAlgo {
	case x509.MD2WithRSA,
		x509.MD5WithRSA,
		x509.SHA1WithRSA,
		x509.SHA256WithRSA,
		x509.SHA384WithRSA,
		x509.SHA512WithRSA,
		x509.SHA256WithRSAPSS,
		x509.SHA384WithRSAPSS,
		x509.SHA512WithRSAPSS:
		return x509.RSA
	case x509.DSAWithSHA1,
		x509.DSAWithSHA256:
		return x509.DSA
	case x509.ECDSAWithSHA1,
		x509.ECDSAWithSHA256,
		x509.ECDSAWithSHA384,
		x509.ECDSAWithSHA512:
		return x509.ECDSA
	case x509.PureEd25519:
		return x509.Ed25519
	default:
		return x509.UnknownPublicKeyAlgorithm
	}
}

// validateCertSignatureAlgorithms checks if the certificate list has any certs that use a
// signature algorithm that the router no longer supports. If an unsupported cert is present, HAProxy
// may refuse to start (server & CA certs) or may start but reject connections (destination CA certs).
func validateCertSignatureAlgorithms(certs []*x509.Certificate) error {
	var errs []error
	for _, cert := range certs {
		// Verify the signature algorithms only for certs signed by a CA.
		// Since OpenSSL doesn't validate self-signed certificates, the signature algorithm check can be skipped.
		// It's important that we do NOT reject self-signed certificates, as many root CAs still utilize SHA1.
		if !isSelfSignedCert(cert) {
			switch cert.SignatureAlgorithm {
			case x509.SHA1WithRSA, x509.ECDSAWithSHA1, x509.DSAWithSHA1:
				errs = append(errs, fmt.Errorf("router does not support CA-signed certs using SHA1"))
			case x509.MD2WithRSA:
				errs = append(errs, fmt.Errorf("router does not support CA-signed certs using MD2"))
			case x509.MD5WithRSA:
				errs = append(errs, fmt.Errorf("router does not support CA-signed certs using MD5"))
			}
		}
	}
	return kerrors.NewAggregate(errs)
}

// validateCertificatePEM checks if a certificate PEM is valid and
// optionally verifies the certificate using the options.
func validateCertificatePEM(certPEM string, options *x509.VerifyOptions) ([]*x509.Certificate, error) {
	certs, err := cert.ParseCertsPEM([]byte(certPEM))
	if err != nil {
		return nil, err
	}

	if len(certs) < 1 {
		return nil, fmt.Errorf("invalid/empty certificate data")
	}

	// Reject any unsupported cert algorithms as HaProxy will refuse to start with them.
	if err := validateCertSignatureAlgorithms(certs); err != nil {
		return certs, err
	}

	if options != nil {
		// Ensure we don't report errors for expired certs or if
		// the validity is in the future.
		// Not that this can be for the actual certificate or any
		// intermediates in the CA chain. This allows the router to
		// still serve an expired/valid-in-the-future certificate
		// and lets the client to control if it can tolerate that
		// (just like for self-signed certs).
		_, err = certs[0].Verify(*options)
		if err != nil {
			if invalidErr, ok := err.(x509.CertificateInvalidError); !ok || invalidErr.Reason != x509.Expired {
				return certs, fmt.Errorf("error verifying certificate: %s", err.Error())
			}
		}
	}

	return certs, nil
}

// UpgradeRouteValidation performs an upgrade validation for
// a route. This checks for issues that will cause failures in the next
// OpenShift version.
func UpgradeRouteValidation(route *routev1.Route) field.ErrorList {
	// This function returns nil because this release has no current route upgradeable issues.
	// Previously in 4.15, we introduced the Upgrade Validation plugin to add the
	// UnservableInFutureVersions condition to indicate that 4.16 does not support SHA1 certs.
	// It's important to keep the Upgrade Validation plugin active in 4.16 as always
	// returning nil clears stale UnservableInFutureVersions conditions (SHA1 Routes
	// are getting rejected now).
	return nil
}

// ValidateTLSExternalCertificate tests different pre-conditions required for
// using externalCertificate.
func ValidateTLSExternalCertificate(route *routev1.Route, fldPath *field.Path, sarc authorizationclient.SubjectAccessReviewInterface, secretsGetter corev1client.SecretsGetter) field.ErrorList {
	tls := route.Spec.TLS

	errs := field.ErrorList{}
	// The router serviceaccount must have permission to get/list/watch the referenced secret.
	// The role and rolebinding to provide this access must be provided by the user.
	if err := authorizationutil.Authorize(sarc, &user.DefaultInfo{Name: routerServiceAccount},
		&authorizationv1.ResourceAttributes{
			Namespace: route.Namespace,
			Verb:      "get",
			Resource:  "secrets",
			Name:      tls.ExternalCertificate.Name,
		}); err != nil {
		errs = append(errs, field.Forbidden(fldPath, "router serviceaccount does not have permission to get this secret"))
	}

	if err := authorizationutil.Authorize(sarc, &user.DefaultInfo{Name: routerServiceAccount},
		&authorizationv1.ResourceAttributes{
			Namespace: route.Namespace,
			Verb:      "watch",
			Resource:  "secrets",
			Name:      tls.ExternalCertificate.Name,
		}); err != nil {
		errs = append(errs, field.Forbidden(fldPath, "router serviceaccount does not have permission to watch this secret"))
	}

	if err := authorizationutil.Authorize(sarc, &user.DefaultInfo{Name: routerServiceAccount},
		&authorizationv1.ResourceAttributes{
			Namespace: route.Namespace,
			Verb:      "list",
			Resource:  "secrets",
			Name:      tls.ExternalCertificate.Name,
		}); err != nil {
		errs = append(errs, field.Forbidden(fldPath, "router serviceaccount does not have permission to list this secret"))
	}

	// The secret should be in the same namespace as that of the route.
	secret, err := secretsGetter.Secrets(route.Namespace).Get(context.TODO(), tls.ExternalCertificate.Name, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return append(errs, field.NotFound(fldPath, err.Error()))
		}
		return append(errs, field.InternalError(fldPath, err))
	}

	// The secret should be of type kubernetes.io/tls
	if secret.Type != kapi.SecretTypeTLS {
		errs = append(errs, field.Invalid(fldPath, tls.ExternalCertificate.Name, fmt.Sprintf("secret of type %q required", kapi.SecretTypeTLS)))
	}

	return errs
}
