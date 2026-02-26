package templaterouter

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"os"
	"time"
)

func generateSelfSignCert(outputFile string) error {
	san := "localhost"
	if domain := os.Getenv("ROUTER_DOMAIN"); domain != "" {
		san = "*." + domain
	}
	crt, key, err := createX509Certificate(san, san)
	if err != nil {
		return fmt.Errorf("error creating self-signed certificate: %w", err)
	}
	return os.WriteFile(outputFile, append(crt, key...), 0644)
}

func createX509Certificate(cn string, san ...string) (crtpem, keypem []byte, err error) {
	serial, err := rand.Int(rand.Reader, big.NewInt(2^63))
	if err != nil {
		return nil, nil, err
	}
	notBefore := time.Now().Add(-time.Hour)
	notAfter := notBefore.Add(3652 * 24 * time.Hour) // 10y
	template := x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName: cn,
		},
		NotBefore: notBefore,
		NotAfter:  notAfter,
		DNSNames:  san,
	}
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, nil, err
	}
	crtder, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		return nil, nil, err
	}
	keyder := x509.MarshalPKCS1PrivateKey(priv)
	crtpem = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: crtder})
	keypem = pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: keyder})
	return crtpem, keypem, nil
}
