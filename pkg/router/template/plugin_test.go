package templaterouter

import (
	"fmt"
	"reflect"
	"testing"
	"time"

	kapi "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/watch"

	routev1 "github.com/openshift/api/route/v1"
	"github.com/openshift/router/pkg/router/controller"
)

const (
	// openssl req -newkey rsa:1024 -nodes -keyout testExpiredCert.key -out testExpiredCert.csr -subj '/CN=www.example.com/ST=SC/C=US/emailAddress=example@example.com/O=Example/OU=Example'
	// faketime 'last year 5pm' /bin/bash -c 'openssl x509 -req -days 1 -sha256 -in testExpiredCert.csr -CA testExpiredCertCA.crt -CAcreateserial -CAkey testExpiredCertCA.key -extensions ext -extfile <(echo $"[ext]\nbasicConstraints = CA:FALSE") -out testExpiredCert.crt'
	//
	// Key = testExpiredCertKey
	// CA = testExpiredCertCA
	testExpiredCert = `-----BEGIN CERTIFICATE-----
MIICoDCCAgkCFAaeel1AtQzHHpRUjVZSaSEbuzcvMA0GCSqGSIb3DQEBCwUAMIGh
MQswCQYDVQQGEwJVUzELMAkGA1UECAwCU0MxFTATBgNVBAcMDERlZmF1bHQgQ2l0
eTEcMBoGA1UECgwTRGVmYXVsdCBDb21wYW55IEx0ZDEQMA4GA1UECwwHVGVzdCBD
QTEaMBgGA1UEAwwRd3d3LmV4YW1wbGVjYS5jb20xIjAgBgkqhkiG9w0BCQEWE2V4
YW1wbGVAZXhhbXBsZS5jb20wHhcNMjMwMTI2MjIwMDAwWhcNMjMwMTI3MjIwMDAw
WjB8MRgwFgYDVQQDDA93d3cuZXhhbXBsZS5jb20xCzAJBgNVBAgMAlNDMQswCQYD
VQQGEwJVUzEiMCAGCSqGSIb3DQEJARYTZXhhbXBsZUBleGFtcGxlLmNvbTEQMA4G
A1UECgwHRXhhbXBsZTEQMA4GA1UECwwHRXhhbXBsZTCBnzANBgkqhkiG9w0BAQEF
AAOBjQAwgYkCgYEAv2GAaJvd0aWxiw7jBTM+VDATQ2vTPbrRc5r8+2yNTwP1Dhr1
VlLmX5o3yv/LeHhK6g8xw1xHcDdvIAdW+J6Uu99BwDK9R5dxwVoEeTYr82vOUoFQ
1sdXqm4226bdAJXf3kdFo+zBr2aefpygpxvMGghz6oW7Nxz78SD8yHNaAzUCAwEA
ATANBgkqhkiG9w0BAQsFAAOBgQABr3mFRjl60RAGL8s1pB6lqBRqy5pG2WoujQlT
N65bDpFJP7vDiFe35qYVVGLAutPxTAaAtGkNhuiyoOqfP6BIlwkM68gD5plmdRBR
hQi35A/6YMFAsBDOnhJccOCO2i19yhyAg2R0tMSder8b51IpmLABLt3UkujwVRJ+
a3zkug==
-----END CERTIFICATE-----`

	testExpiredCertKey = `-----BEGIN PRIVATE KEY-----
MIICeAIBADANBgkqhkiG9w0BAQEFAASCAmIwggJeAgEAAoGBAL9hgGib3dGlsYsO
4wUzPlQwE0Nr0z260XOa/PtsjU8D9Q4a9VZS5l+aN8r/y3h4SuoPMcNcR3A3byAH
VvielLvfQcAyvUeXccFaBHk2K/NrzlKBUNbHV6puNtum3QCV395HRaPswa9mnn6c
oKcbzBoIc+qFuzcc+/Eg/MhzWgM1AgMBAAECgYEAqC4Ood8XNzzcoM8cQV2e0GzP
ANiocf7SQT1aQ7hJFb7sgtC9+HYxbKIhlYrkS6Gqc7WWjY9yV/Le/M52Z1U0bb5r
/guvTx686AYoTZzCjptwpAuVE1sS9r+WCa/VgflPZYtaql960TtO72ntjcWHoIl/
2AsE0/1wLHLquYHaDaECQQDxdDL9ecvEWD6yTom9GlfQq/6CwzA6Nhs1oncgDT/4
1OXGD/GgXUvlJZszdkTrYh4az8YeobDsJsfMjIkFfQpDAkEAyukOORSRIk9/KBUm
Ufyu7JtFibtr8mozkf43Dd+2BYyO6dPqS/E9m0nUZoMSoGfS0LqS8pT1hUU0RMun
dXURJwJBALBC5V5I5UmmKc68qqxTaLu6cwc+OhykluRmf5P0WDjsIfiedwNcWCUl
eNDui41RiSyFdNmzq5YZEU3vYa+SAkUCQQCx99EyvVhCWKl1dX9jv5WJDvLhx9H5
D67lqKuO7p0OpuaeLfE85H0dW5cAxouqxwU/b7T9MSta1YTvphPdUG1XAkBwH06h
STCeJCluaFZp/Aaok8fTAbodEu5gdfo/W8dspflTFM6RcR9+WJb91elObh3QiJET
T1ZXsR6eRuHdx4oh
-----END PRIVATE KEY-----`

	// Key = testPrivateKey
	// CA = testCACertificate
	testCertificate = `-----BEGIN CERTIFICATE-----
MIICwjCCAiugAwIBAgIBATANBgkqhkiG9w0BAQsFADBjMQswCQYDVQQGEwJVUzEL
MAkGA1UECAwCQ0ExETAPBgNVBAoMCFNlY3VyaXR5MRswGQYDVQQLDBJPcGVuU2hp
ZnQzIHRlc3QgQ0ExFzAVBgNVBAMMDmhlYWRlci50ZXN0IENBMB4XDTE2MDMxMjA0
MjEwM1oXDTM2MDMxMjA0MjEwM1owWDEUMBIGA1UEAwwLaGVhZGVyLnRlc3QxCzAJ
BgNVBAgMAkNBMQswCQYDVQQGEwJVUzERMA8GA1UECgwIU2VjdXJpdHkxEzARBgNV
BAsMCk9wZW5TaGlmdDMwggEiMA0GCSqGSIb3DQEBAQUAA4IBDwAwggEKAoIBAQD0
XEAzUMflZy8zluwzqMKnu8jYK3yUoEGLN0Bw0A/7ydno1g0E92ee8M9p59TCCWA6
nKnt1DEK5285xAKs9AveutSYiDkpf2px59GvCVx2ecfFBTECWHMAJ/6Y7pqlWOt2
hvPx5rP+jVeNLAfK9d+f57FGvWXrQAcBnFTegS6J910kbvDgNP4Nerj6RPAx2UOq
6URqA4j7qZs63nReeu/1t//BQHNokKddfxw2ZXcL/5itgpPug16thp+ugGVdjcFs
aasLJOjErUS0D+7bot98FL0TSpxWqwtCF117bSLY7UczZFNAZAOnZBFmSZBxcJJa
TZzkda0Oiqo0J3GPcZ+rAgMBAAGjDTALMAkGA1UdEwQCMAAwDQYJKoZIhvcNAQEL
BQADgYEACkdKRUm9ERjgbe6w0fw4VY1s5XC9qR1m5AwLMVVwKxHJVG2zMzeDTHyg
3cjxmfZdFU9yxmNUCh3mRsi2+qjEoFfGRyMwMMx7cduYhsFY3KA+Fl4vBRXAuPLR
eCI4ErCPi+Y08vOto9VVXg2f4YFQYLq1X6TiXD5RpQAN0t8AYk4=
-----END CERTIFICATE-----`

	testPrivateKey = `-----BEGIN RSA PRIVATE KEY-----
MIIEpAIBAAKCAQEA9FxAM1DH5WcvM5bsM6jCp7vI2Ct8lKBBizdAcNAP+8nZ6NYN
BPdnnvDPaefUwglgOpyp7dQxCudvOcQCrPQL3rrUmIg5KX9qcefRrwlcdnnHxQUx
AlhzACf+mO6apVjrdobz8eaz/o1XjSwHyvXfn+exRr1l60AHAZxU3oEuifddJG7w
4DT+DXq4+kTwMdlDqulEagOI+6mbOt50Xnrv9bf/wUBzaJCnXX8cNmV3C/+YrYKT
7oNerYafroBlXY3BbGmrCyToxK1EtA/u26LffBS9E0qcVqsLQhdde20i2O1HM2RT
QGQDp2QRZkmQcXCSWk2c5HWtDoqqNCdxj3GfqwIDAQABAoIBAEfl+NHge+CIur+w
MXGFvziBLThFm1NTz9U5fZFz9q/8FUzH5m7GqMuASVb86oHpJlI4lFsw6vktXXGe
tbbT28Y+LJ1wv3jxT42SSwT4eSc278uNmnz5L2UlX2j6E7CA+E8YqCBN5DoKtm8I
PIbAT3sKPgP1aE6OuUEFEYeidOIMvjco2aQH0338sl6cObkQFEgnWf2ncun3KGnb
s+dMO5EdYLo0rOdDXY88sElfqiNYYl/FRu9O3OfqHvScA5uo9FlIhukcrRkbjFcq
j/7k4tt0iLs9B2j+4ihBWYo5eRFIde4Izj6a6ArEk0ShEUvwlZBuGMM/vs+jvbDK
l3+0NpECgYEA/+qxwvOGjmlYNKFK/rzxd51EnfCISnV+tb17pNyRmlGToi1/LmmV
+jcJfcwlf2o8mTFn3xAdD3fSaHF7t8Li7xDwH2S+sSuFE/8bhgHUvw1S7oILMYyO
hO6sWG+JocMhr8IejaAnQxav9VvP01YDfw/XBB0O1EIuzzr2KHq+AGMCgYEA9HCY
JGTcv7lfs3kcCAkDtjl8NbjNRMxRErG0dfYS+6OSaXOOMg1TsaSNEgjOGyUX+yQ4
4vtKcLwHk7+qz3ZPbhS6m7theZG9jUwMrQRGyCE7z3JUy8vmV/N+HP0V+boT+4KM
Tai3+I3hf9+QMHYx/Z/VA0K6f27LwP+kEL9C8hkCgYEAoiHeXNRL+w1ihHVrPdgW
YuGQBz/MGOA3VoylON1Eoa/tCGIqoQzjp5IWwUwEtaRon+VdGUTsJFCVTPYYm2Ms
wqjIeBsrdLNNrE2C8nNWhXO7hr98t/eEk1NifOStHX6yaNdi4/cC6M4GzDtOf2WO
8YDniAOg0Xjcjw2bxil9FmECgYBuUeq4cjUW6okArSYzki30rhka/d7WsAffEgjK
PFbw7zADG74PZOhjAksQ2px6r9EU7ZInDxbXrmUVD6n9m/3ZRs25v2YMwfP0s1/9
LjLr2+PsikMu/0VkaGaAmtCyNoMSPicoXX86VH5zgejHlnCVcO9oW1NkdBLNdhML
4+ZI8QKBgQDb+SH7i50Yu3adwvPkDSp3ACCzPoHXno79a7Y5S2JzpFtNq+cNLWEb
HP8gHJSZnaGrLKmjwNeQNsARYajKmDKO5HJ9g5H5Hae8enOb2yie541dneDT8rID
4054dMQJnijd8620yf8wiNy05ZPOQQ0JvA/rW3WWZc5PGm8c2PsVjg==
-----END RSA PRIVATE KEY-----`

	// Key = N/A
	// CA = self-signed
	testCACertificate = `-----BEGIN CERTIFICATE-----
MIIClDCCAf2gAwIBAgIJAPU57OGhuqJtMA0GCSqGSIb3DQEBCwUAMGMxCzAJBgNV
BAYTAlVTMQswCQYDVQQIDAJDQTERMA8GA1UECgwIU2VjdXJpdHkxGzAZBgNVBAsM
Ek9wZW5TaGlmdDMgdGVzdCBDQTEXMBUGA1UEAwwOaGVhZGVyLnRlc3QgQ0EwHhcN
MTYwMzEyMDQyMTAzWhcNMzYwMzEyMDQyMTAzWjBjMQswCQYDVQQGEwJVUzELMAkG
A1UECAwCQ0ExETAPBgNVBAoMCFNlY3VyaXR5MRswGQYDVQQLDBJPcGVuU2hpZnQz
IHRlc3QgQ0ExFzAVBgNVBAMMDmhlYWRlci50ZXN0IENBMIGfMA0GCSqGSIb3DQEB
AQUAA4GNADCBiQKBgQCsdVIJ6GSrkFdE9LzsMItYGE4q3qqSqIbs/uwMoVsMT+33
pLeyzeecPuoQsdO6SEuqhUM1ivUN4GyXIR1+aW2baMwMXpjX9VIJu5d4FqtGi6SD
RfV+tbERWwifPJlN+ryuvqbbDxrjQeXhemeo7yrJdgJ1oyDmoM5pTiSUUmltvQID
AQABo1AwTjAdBgNVHQ4EFgQUOVuieqGfp2wnKo7lX2fQt+Yk1C4wHwYDVR0jBBgw
FoAUOVuieqGfp2wnKo7lX2fQt+Yk1C4wDAYDVR0TBAUwAwEB/zANBgkqhkiG9w0B
AQsFAAOBgQA8VhmNeicRnKgXInVyYZDjL0P4WRbKJY7DkJxRMRWxikbEVHdySki6
jegpqgJqYbzU6EiuTS2sl2bAjIK9nGUtTDt1PJIC1Evn5Q6v5ylNflpv6GxtUbCt
bGvtpjWA4r9WASIDPFsxk/cDEEEO6iPxgMOf5MdpQC2y2MU0rzF/Gg==
-----END CERTIFICATE-----`

	// openssl req -x509 -sha1 -newkey rsa:1024 -days 3650 -keyout testCertificateRsaSha1SelfSignedRootCA.key -out testCertificateRsaSha1SelfSignedRootCA.crt -addext "keyUsage=cRLSign, digitalSignature, keyCertSign" -addext "extendedKeyUsage=serverAuth, clientAuth" -nodes -subj '/C=US/ST=SC/L=Default City/O=Default Company Ltd/OU=Test CA/CN=www.exampleca.com/emailAddress=example@example.com'
	//
	// Key = testCertificateRsaSha1SelfSignedRootCAKey
	// CA = self-signed
	testCertificateRsaSha1SelfSignedRootCA = `-----BEGIN CERTIFICATE-----
MIIDTDCCArWgAwIBAgIUESnhsJLBoYVoOfqUpoJcxIjpr9IwDQYJKoZIhvcNAQEF
BQAwgaExCzAJBgNVBAYTAlVTMQswCQYDVQQIDAJTQzEVMBMGA1UEBwwMRGVmYXVs
dCBDaXR5MRwwGgYDVQQKDBNEZWZhdWx0IENvbXBhbnkgTHRkMRAwDgYDVQQLDAdU
ZXN0IENBMRowGAYDVQQDDBF3d3cuZXhhbXBsZWNhLmNvbTEiMCAGCSqGSIb3DQEJ
ARYTZXhhbXBsZUBleGFtcGxlLmNvbTAeFw0yNDEyMTIwMTA3MTVaFw0zNDEyMTAw
MTA3MTVaMIGhMQswCQYDVQQGEwJVUzELMAkGA1UECAwCU0MxFTATBgNVBAcMDERl
ZmF1bHQgQ2l0eTEcMBoGA1UECgwTRGVmYXVsdCBDb21wYW55IEx0ZDEQMA4GA1UE
CwwHVGVzdCBDQTEaMBgGA1UEAwwRd3d3LmV4YW1wbGVjYS5jb20xIjAgBgkqhkiG
9w0BCQEWE2V4YW1wbGVAZXhhbXBsZS5jb20wgZ8wDQYJKoZIhvcNAQEBBQADgY0A
MIGJAoGBANiNMnmpMORg+X9TujvAXx1ysM9SzuYLX5SKhxq9SiSqKE+YZjxpkf2E
vBKraxgKIBEHrGpn5CX2YKycT0Tio6G98/8O/xyDAqdHIE5PCD9srz5INtw5Vx9u
LbtSOPwzLoN6qQIH31rdXShdkKVKDegsKgPaRPBlY1O43sXgkCahAgMBAAGjfzB9
MB0GA1UdDgQWBBRcxFzhkQELDqWRGp2Hjnb+PHSDYzAfBgNVHSMEGDAWgBRcxFzh
kQELDqWRGp2Hjnb+PHSDYzAPBgNVHRMBAf8EBTADAQH/MAsGA1UdDwQEAwIBhjAd
BgNVHSUEFjAUBggrBgEFBQcDAQYIKwYBBQUHAwIwDQYJKoZIhvcNAQEFBQADgYEA
u1CFt+f41DhmsOWkaT7SLBR8ODmdq91ta8vYP+L3Ws2fZ2tUNH/DX/lofR90GXA3
L5W8aWhQYdk+S7zuCFmt18QFjRXX0szbLawGRA+t4zQy/AeOIVnmrlKSs6rQ4I+e
yuoyUfLE8+ULl92NZbj3pHKnWLddD7uVK2GYHr/P8kQ=
-----END CERTIFICATE-----
`
	// Key is not used, but keep here for reference for signing new certs if needed.
	testCertificateRsaSha1SelfSignedRootCAKey = `-----BEGIN PRIVATE KEY-----
MIICdwIBADANBgkqhkiG9w0BAQEFAASCAmEwggJdAgEAAoGBANiNMnmpMORg+X9T
ujvAXx1ysM9SzuYLX5SKhxq9SiSqKE+YZjxpkf2EvBKraxgKIBEHrGpn5CX2YKyc
T0Tio6G98/8O/xyDAqdHIE5PCD9srz5INtw5Vx9uLbtSOPwzLoN6qQIH31rdXShd
kKVKDegsKgPaRPBlY1O43sXgkCahAgMBAAECgYAxSVGnpv5dvESM2j2Uw9/iD+x2
A17btNL4N98wEs0BM0khdIowTcbQcJltll41hnht59UyEps2mLDAGINiJkMfbWyD
uKuy1Lmo/+QE4hTZ7VSIoznmpQr4XjytHmSVP5JYBSQIG/uCSg2OoMwjwnXLO6rO
NbYX2392upZZm135UQJBAPzubZkElK19qU0hbMwWfgJE2OwYuo6lS/3x/l40sgHq
NbkurL8W/NIF5v+/X9DOCYbUqp8E0DtLZPmXebACDoMCQQDbLccxv9erBBq2+YNJ
P2ZYzHbwSrj98NLMwdMkutbbHd2521DSXbaT0mdb2QT3MpdK0PT98JnJccI53vHQ
ua0LAkEAoLFGVjIv121/s24p9hvQINbmzlEDrX7dIdCuH+HwugC38xfxTlJne3Oe
iBto33sXWF8iq3beaN2EoIIZILadywJAD+K7g0GSUhTUEtr2xwJPWrRHEpd33P/t
Z2XM9eaM2AjMH0JkEzszlnczgpayI3CJQqTufNFJdC5Ik4UzJZuvjQJBAM8cYMDt
tO6ylsZ2JWKlnsFVW0Nsx696Y3dLygymVLlU607/a7QP9Lakf9XwI8dSmZDIuW9l
w0VeEQOmXrayLUM=
-----END PRIVATE KEY-----
`

	testDestinationCACertificate = testCACertificate

	// openssl req -newkey rsa:1024 -nodes -keyout testCertificateRsaSha1.key -out testCertificateRsaSha1.csr -subj '/CN=www.example.com/ST=SC/C=US/emailAddress=example@example.com/O=Example/OU=Example'
	// openssl x509 -req -days 3650 -sha1 -in testCertificateRsaSha1.csr -CA testCertificateRsaSha1SelfSignedRootCA.crt -CAcreateserial -CAkey testCertificateRsaSha1SelfSignedRootCA.key -extensions ext -extfile <(echo $'[ext]\nbasicConstraints = CA:FALSE') -out testCertificateRsaSha1.crt
	//
	// Key = testCertificateRsaSha1Key
	// CA = testCertificateRsaSha1SelfSignedRootCA
	testCertificateRsaSha1 = `-----BEGIN CERTIFICATE-----
MIIC9DCCAl2gAwIBAgIUaTcUc8Cz/ZVnUotUfvexgWLIiUAwDQYJKoZIhvcNAQEF
BQAwgaExCzAJBgNVBAYTAlVTMQswCQYDVQQIDAJTQzEVMBMGA1UEBwwMRGVmYXVs
dCBDaXR5MRwwGgYDVQQKDBNEZWZhdWx0IENvbXBhbnkgTHRkMRAwDgYDVQQLDAdU
ZXN0IENBMRowGAYDVQQDDBF3d3cuZXhhbXBsZWNhLmNvbTEiMCAGCSqGSIb3DQEJ
ARYTZXhhbXBsZUBleGFtcGxlLmNvbTAeFw0yNDEyMTIwMTE5MzFaFw0zNDEyMTAw
MTE5MzFaMHwxGDAWBgNVBAMMD3d3dy5leGFtcGxlLmNvbTELMAkGA1UECAwCU0Mx
CzAJBgNVBAYTAlVTMSIwIAYJKoZIhvcNAQkBFhNleGFtcGxlQGV4YW1wbGUuY29t
MRAwDgYDVQQKDAdFeGFtcGxlMRAwDgYDVQQLDAdFeGFtcGxlMIGfMA0GCSqGSIb3
DQEBAQUAA4GNADCBiQKBgQDOg5xJj2j1L/bMeCEzq4L+lQNX3A/xpGq2cVL1FfoM
9+ZhUhREIN0PhBnnt1+xPGc9IqoBN8NzmyoGUfrnQGuAlXLHc8RV4Cve+ms6YXYZ
j2YBI1fmgkie7BbnaVzZZYmD9YPSicUpu67x9kpp4O6CTpkdLSgWf1EmrGz/2ynS
HQIDAQABo00wSzAJBgNVHRMEAjAAMB0GA1UdDgQWBBQpXIYiyu06TdXTMxWEL6/C
+E2YiTAfBgNVHSMEGDAWgBRcxFzhkQELDqWRGp2Hjnb+PHSDYzANBgkqhkiG9w0B
AQUFAAOBgQAQ7dEL3vRXWn41lDAjnhHi72DEHfpUazpW9zAJz63IDTWJNKP2h0Ab
xyHCryReB4oxwiFgFzLHAaknudoK8d3ceBL/ZLDlGy0KskxwW0Re3zNixYEFoWgx
Yzh+Fin/QXlJs3xtJqlHfaeo2AX9X5C1MDhAg22Ybt4w91OA88U5jg==
-----END CERTIFICATE-----
`
	testCertificateRsaSha1Key = `-----BEGIN PRIVATE KEY-----
MIICdwIBADANBgkqhkiG9w0BAQEFAASCAmEwggJdAgEAAoGBAM6DnEmPaPUv9sx4
ITOrgv6VA1fcD/GkarZxUvUV+gz35mFSFEQg3Q+EGee3X7E8Zz0iqgE3w3ObKgZR
+udAa4CVcsdzxFXgK976azphdhmPZgEjV+aCSJ7sFudpXNlliYP1g9KJxSm7rvH2
Smng7oJOmR0tKBZ/USasbP/bKdIdAgMBAAECgYEAh3dR1/cY1G1oKWxL60cAoNtC
3Clg1BQUZCUmU9rcshETsJdU7/PWzszK6XMidHK5DiNk/XOE5JrOEGNKgNODMCIC
06vUJ8joXSMbRMvpPmRuRwf2P1OeUv7nXig6iKPFI0u6zCsfYaXBIf4C3lcssXc+
Ra6hH9CPU59DNhQUQ0kCQQDt/Z27n1gSpa612FZPDEtoMy/mZHjG9sc1WIO07Kqd
kElQ2k6S/VYxoSfVmG8Oj6yK13hs/Z3Kr5MJvjl+jTEvAkEA3iQ56aR2t1Xe1Qle
oMtI5ZuH8a+C97RF5T/FSKahAXaVsCSCDxRCLI7GDDqqdFxUhT5khBmqCaLZlv6J
0RxmcwJAYpmAkAskYhVinNRUbcuaMkGCxuE5aLU1M1TIvFyRE1aECYtool1zKHys
FEJjQJUl1yAONJmelirHsHGvQE8e4QJBAKldt2XixbysVNPaa/Jua2rcNT7Y0SLo
qG3MPC9TE+iYsDH2885pZLayOF90jydeifZ5BowNQS5NolZURWFQpO8CQAxHqB79
1Z8mnatEkb0pJaw/CXngfbCGKCn7h7X5+Pup4nOpnnGmwmQtG6A5YRPsvBaY22/3
uQgaIwjsQArmlhA=
-----END PRIVATE KEY-----`

	// openssl req -newkey rsa:1024 -nodes -keyout testCertificateRsaSha1SameSubjIssuer.key -out testCertificateRsaSha1SameSubjIssuer.csr -subj '/C=US/ST=SC/L=Default City/O=Default Company Ltd/OU=Test CA/CN=www.exampleca.com/emailAddress=example@example.com'
	// openssl x509 -req -days 3650 -sha1 -in testCertificateRsaSha1SameSubjIssuer.csr -CA testCertificateRsaSha1SelfSignedRootCA.crt -CAcreateserial -CAkey testCertificateRsaSha1SelfSignedRootCA.key -extensions ext -extfile <(echo $'[ext]\nbasicConstraints = CA:FALSE') -out testCertificateRsaSha1SameSubjIssuer.crt
	//
	// Key = testCertificateRsaSha1Key
	// CA = testCertificateRsaSha1SelfSignedRootCA
	//
	// This key intentionally has the same subject as testCertificateRsaSha1SelfSignedRootCA.
	testCertificateRsaSha1SameSubjIssuer = `-----BEGIN CERTIFICATE-----
MIIDGjCCAoOgAwIBAgIUaTcUc8Cz/ZVnUotUfvexgWLIiT0wDQYJKoZIhvcNAQEF
BQAwgaExCzAJBgNVBAYTAlVTMQswCQYDVQQIDAJTQzEVMBMGA1UEBwwMRGVmYXVs
dCBDaXR5MRwwGgYDVQQKDBNEZWZhdWx0IENvbXBhbnkgTHRkMRAwDgYDVQQLDAdU
ZXN0IENBMRowGAYDVQQDDBF3d3cuZXhhbXBsZWNhLmNvbTEiMCAGCSqGSIb3DQEJ
ARYTZXhhbXBsZUBleGFtcGxlLmNvbTAeFw0yNDEyMTIwMTEyNTNaFw0zNDEyMTAw
MTEyNTNaMIGhMQswCQYDVQQGEwJVUzELMAkGA1UECAwCU0MxFTATBgNVBAcMDERl
ZmF1bHQgQ2l0eTEcMBoGA1UECgwTRGVmYXVsdCBDb21wYW55IEx0ZDEQMA4GA1UE
CwwHVGVzdCBDQTEaMBgGA1UEAwwRd3d3LmV4YW1wbGVjYS5jb20xIjAgBgkqhkiG
9w0BCQEWE2V4YW1wbGVAZXhhbXBsZS5jb20wgZ8wDQYJKoZIhvcNAQEBBQADgY0A
MIGJAoGBALyHvdCqBhQZHVipNfRPbvYVHlfPG1q32fkYVcEUyKjQXrd9R31UogjH
LxhxEb2cxBi63yxIkJD1fnQXMLtubgvF++AcyYGK5/rSpcgHpcYF0x6vsXCJMvO8
QN5rPJZX/gk75Ci26uq15K25LmOYeEJs+IIsUiRNUAEOtTufeuOPAgMBAAGjTTBL
MAkGA1UdEwQCMAAwHQYDVR0OBBYEFLE4C5n+g9n6hd/HbMctz0/9Da3wMB8GA1Ud
IwQYMBaAFFzEXOGRAQsOpZEanYeOdv48dINjMA0GCSqGSIb3DQEBBQUAA4GBAFrY
Ovoj9VOUFcGdrZ45fqCYxemjMWbgkjqE0HwvxwkdKrVEjOkemlcpKD6wzUx7EYs1
fWHd+vvn9mQLvUENhpCTr+9yS+z9m6m+q6xUYGp9G9rMIpwz9M/zTYrJp1pKqcU7
GeVqRN168ouWj/FfR+ubOcq3tji1qQA3mzRog5nz
-----END CERTIFICATE-----
`
	testCertificateRsaSha1SameSubjIssuerKey = `
-----BEGIN PRIVATE KEY-----
MIICdgIBADANBgkqhkiG9w0BAQEFAASCAmAwggJcAgEAAoGBALyHvdCqBhQZHVip
NfRPbvYVHlfPG1q32fkYVcEUyKjQXrd9R31UogjHLxhxEb2cxBi63yxIkJD1fnQX
MLtubgvF++AcyYGK5/rSpcgHpcYF0x6vsXCJMvO8QN5rPJZX/gk75Ci26uq15K25
LmOYeEJs+IIsUiRNUAEOtTufeuOPAgMBAAECgYB4B8A84pL+JsM9WHYGdrBBsk5g
P3a9+kGnyuuGA3KBsDAtiHCEheanyhDc8dgGrZFX4VoHOqf38qSwyrb3Dia29nIl
mzrWFa+xRy8r5FQw+BUw0SnOhC59RyCU7zLvUzVvqcv+/DRv7vRT96Fbe654ql3W
4sk0A6znwpmIGFcUSQJBAN5VLVrxq1qF058yZfiJ1PJDtMjBaMNrkBiNMT01Uvby
iDemGPXgX2eAtSB80sNEkHdSwjHCNNfuOLUD9BtPrdUCQQDZFDJVqSzJgAVBJ4ry
zlVEFx+ZoKu5G/T3E6ojUqh1hsJO5ODosqjh86BFdvvxz8ciRGdpB568VRRjuNvE
WanTAkAYnoP0MxiPYIxLb5A9Ej4jSX4GUOxh31JIdbIDHhl+wOJ2jwzqhRrrYiQs
YcYQ21HH9MEOM3wYgQeEe9iXAZ61AkAft/vC2H1a1AHwiz6aS9vZnydW40s0OQmK
MK1ji+hhg9dQf9D9L13N5jM88y3NH3cRYr1Zc2uWSTg5egFip1dRAkEAui5QR6OH
+H6PjQWeW5sq2XWJ3rMepfHfkEWOOOyjSpUN832LyWfJJYQVBwySVXCkbPqbRscG
h8zPSgGSPt9UIg==
-----END PRIVATE KEY-----
`

	// openssl req -x509 -newkey rsa:2048 -days 3650 -sha1 -keyout testCertificateRsaSha1SelfSigned.key -nodes -subj '/CN=www.example.com/ST=SC/C=US/emailAddress=example@example.com/O=Example/OU=Example' -addext "basicConstraints=CA:FALSE" -out testCertificateRsaSha1SelfSigned.crt
	//
	// Key = testCertificateRsaSha1SelfSignedKey
	// CA = self-signed
	testCertificateRsaSha1SelfSigned = `-----BEGIN CERTIFICATE-----
MIID0zCCArugAwIBAgIUYnuOhBfzAKuCC2fUAmVMR7+C1jEwDQYJKoZIhvcNAQEF
BQAwfDEYMBYGA1UEAwwPd3d3LmV4YW1wbGUuY29tMQswCQYDVQQIDAJTQzELMAkG
A1UEBhMCVVMxIjAgBgkqhkiG9w0BCQEWE2V4YW1wbGVAZXhhbXBsZS5jb20xEDAO
BgNVBAoMB0V4YW1wbGUxEDAOBgNVBAsMB0V4YW1wbGUwHhcNMjQxMjA1MTc1MjM0
WhcNMzQxMjAzMTc1MjM0WjB8MRgwFgYDVQQDDA93d3cuZXhhbXBsZS5jb20xCzAJ
BgNVBAgMAlNDMQswCQYDVQQGEwJVUzEiMCAGCSqGSIb3DQEJARYTZXhhbXBsZUBl
eGFtcGxlLmNvbTEQMA4GA1UECgwHRXhhbXBsZTEQMA4GA1UECwwHRXhhbXBsZTCC
ASIwDQYJKoZIhvcNAQEBBQADggEPADCCAQoCggEBANyrQEMLrd9QY0ZH8GbDENHh
qDrEuaib1Xy+M8qdRQbWZBRYDLQQrveDOfp7oPT5DylYg5oH0P1K01bfqRp6+PhG
LEr2GDu41smvUQiCCsIJTxwGKFYygFuKM4OfB6ieydTQJnZNc+1QNSDnIhizZ98O
j9H8bnfUeHSbVjL9oONFOIUbLzqF/FzdL7yvlifFDdI998uBc2iYprh3m1NOAxQu
6TXhxK2j34qPaBhGdtPaOXsKW0qkA0XySROSh9EWnkoQx4bdc71dmbCJflxeWkOV
RVCHwEU1oRK3FA73LzMP9C/rSp8TiTYc39rNSq4Tnbm5EDcHEI298egp3xnsxekC
AwEAAaNNMEswHQYDVR0OBBYEFN+n2yc9ULcaMkqTfXRGQ9AuU/H7MB8GA1UdIwQY
MBaAFN+n2yc9ULcaMkqTfXRGQ9AuU/H7MAkGA1UdEwQCMAAwDQYJKoZIhvcNAQEF
BQADggEBAJim5Ep7rD6wfbg2aWdltsrHeSbX/1iva/yPkFyMvDMpTpeGKqRWQlRL
e39PyqF6QyZGsfUJsib/UzsUQD0xuabwpS2aOIy3Ie+x+xmNga1FdYvN9NbnPUyi
7VoQ5lZSe+ZQHa5iYWuDJtrAcFUib3YrTOKtgDiHroMICWCQEnK4vwMHk0G9yvHJ
RJVqubu+JSEwivgtQRdcUHBSz9GHgCm58YyV9we6UAVFSudpFfTRbr5gKIiP858q
atCQ7S3S25DHcr8Hj1RmaiLmhe1o5LtG282y5zGte+8TlMnimwCoeldRVngH9Nhs
bnqtc2ouTrKiR0Ec+QsV1a1hfhRuj2M=
-----END CERTIFICATE-----
`
	testCertificateRsaSha1SelfSignedKey = `-----BEGIN PRIVATE KEY-----
MIIEvQIBADANBgkqhkiG9w0BAQEFAASCBKcwggSjAgEAAoIBAQDcq0BDC63fUGNG
R/BmwxDR4ag6xLmom9V8vjPKnUUG1mQUWAy0EK73gzn6e6D0+Q8pWIOaB9D9StNW
36kaevj4RixK9hg7uNbJr1EIggrCCU8cBihWMoBbijODnweonsnU0CZ2TXPtUDUg
5yIYs2ffDo/R/G531Hh0m1Yy/aDjRTiFGy86hfxc3S+8r5YnxQ3SPffLgXNomKa4
d5tTTgMULuk14cSto9+Kj2gYRnbT2jl7CltKpANF8kkTkofRFp5KEMeG3XO9XZmw
iX5cXlpDlUVQh8BFNaEStxQO9y8zD/Qv60qfE4k2HN/azUquE525uRA3BxCNvfHo
Kd8Z7MXpAgMBAAECggEAY78lNSk6Vw9HUKWEDW9vUu/l02rJYWXPgquXTab5ZLXU
Vz3VwC8qZ8dxlb/8ab+LEu1nz2BpH5WLImHHVqjvkYpmyxuiqJxMuq38uxPNORhs
IgbGhPAfBUHbN0vTcm0UXpYYTLGGDWeMHGteBjxSX4l9iTXJ2XC5Yjw1Iqdy6kew
wEACuHgROJKYFEBeufhuSOSpplrepaqpBV4g5l75BVCBYQ/nQLsKcLQgaQ42kx+x
7YNvSlGeieEcj/Eft5zB6HxADfjyMlNwDJ2bi37oq9s9q8PKVBVFYyCOAz06ZGuo
pwY8z2Qpi3j1D0nnPWMXjEP5NmDotORy4EFJtfSC4QKBgQD5G28GHxtp1197hMhB
SZ8bzFQ6kBFxVHjrgjxYb8kS5j2ANm49/oW+PnnNwFbO84fgC97oQDE0K8cPBL3A
tcsQvbvz29M2VcPu9zus6YxRcsGTyCLRg0aT4NuXtRccYg681jH1FTFZCiNpZGnx
Z6C1+zW9CcB1aBbzjiRlbPx6+wKBgQDixl+awgDIt19HnsUVup7+zSEXxT/8ixc9
QENdZaEC8lZJY/WzehKgZpMjmN0zTmWGU2anq6i5tbivyFXaLlZTFdpjK1eq4h/n
JU9oJjMhZzoRA6Vhlrqiy6CTECa/fyr/d7zB9bkLveSUds/U0n4P6oU2msOtAJ8d
SFtApbHtawKBgQDAfbRzFIKIbQa5Wcesu4kZX/EON9liq5Ws1rxu0iKcWhHYCzdw
7EbI1Vol5aSu0nyCYmnjKgdbeyCcuFswmMnLq/Ga5Jj3eZqoA5+3Y9kr7vMqkRJm
t3xINQ860ZKEOjmNLi74ZWH2neDzRcaf5iXHudCyvOBdWQuzNHlnbqpDFQKBgCrV
o5tcx78h++pQUBPRo1SntHeD95khQKt+JvtORgKDec71BaT4CuqnVWWk6ytUxJKB
0GMdZopli9QQOD80/3NELnMK7c1GVxZXEs+uX3wQvoQWNzfeu7QiWFtO8rK7N4j3
ufy9CE3yeWmdo5YkiFFDUBRHWWylMGjckPf+FESvAoGAdZ63rjBO9XT2I/zu+Yvj
fTror7gkwHlb5H1O/ynA/R6TdMjlCZHl1Sv6ThdS77nzrEML1U3DfmEm+D3NgtVd
zEfT6Sd9HQFjt1qjydVxicSNPUc4Uv30WZ6+HsIqp7ER9XzYEPPsUkfQxZEghddb
X7ziGItWQDkoCNS0SzR0rqw=
-----END PRIVATE KEY-----
`

	// openssl req -newkey rsa:1024 -nodes -keyout testCertificateRsaSha256Key.key -out testCertificateRsaSha256.csr -subj '/CN=www.example.com/ST=SC/C=US/emailAddress=example@example.com/O=Example/OU=Example'
	// openssl x509 -req -days 3650 -sha256 -in testCertificateRsaSha256.csr -CA testCertificateRsaSha1SelfSignedRootCA.crt -CAcreateserial -CAkey testCertificateRsaSha1SelfSignedRootCA.key -extensions ext -extfile <(echo $'[ext]\nbasicConstraints = CA:FALSE') -out testCertificateRsaSha256.crt
	//
	// Key = testCertificateRsaSha256Key
	// CA = testCertificateRsaSha1SelfSignedRootCA
	testCertificateRsaSha256 = `-----BEGIN CERTIFICATE-----
MIIDTDCCArWgAwIBAgIUESnhsJLBoYVoOfqUpoJcxIjpr9IwDQYJKoZIhvcNAQEF
BQAwgaExCzAJBgNVBAYTAlVTMQswCQYDVQQIDAJTQzEVMBMGA1UEBwwMRGVmYXVs
dCBDaXR5MRwwGgYDVQQKDBNEZWZhdWx0IENvbXBhbnkgTHRkMRAwDgYDVQQLDAdU
ZXN0IENBMRowGAYDVQQDDBF3d3cuZXhhbXBsZWNhLmNvbTEiMCAGCSqGSIb3DQEJ
ARYTZXhhbXBsZUBleGFtcGxlLmNvbTAeFw0yNDEyMTIwMTA3MTVaFw0zNDEyMTAw
MTA3MTVaMIGhMQswCQYDVQQGEwJVUzELMAkGA1UECAwCU0MxFTATBgNVBAcMDERl
ZmF1bHQgQ2l0eTEcMBoGA1UECgwTRGVmYXVsdCBDb21wYW55IEx0ZDEQMA4GA1UE
CwwHVGVzdCBDQTEaMBgGA1UEAwwRd3d3LmV4YW1wbGVjYS5jb20xIjAgBgkqhkiG
9w0BCQEWE2V4YW1wbGVAZXhhbXBsZS5jb20wgZ8wDQYJKoZIhvcNAQEBBQADgY0A
MIGJAoGBANiNMnmpMORg+X9TujvAXx1ysM9SzuYLX5SKhxq9SiSqKE+YZjxpkf2E
vBKraxgKIBEHrGpn5CX2YKycT0Tio6G98/8O/xyDAqdHIE5PCD9srz5INtw5Vx9u
LbtSOPwzLoN6qQIH31rdXShdkKVKDegsKgPaRPBlY1O43sXgkCahAgMBAAGjfzB9
MB0GA1UdDgQWBBRcxFzhkQELDqWRGp2Hjnb+PHSDYzAfBgNVHSMEGDAWgBRcxFzh
kQELDqWRGp2Hjnb+PHSDYzAPBgNVHRMBAf8EBTADAQH/MAsGA1UdDwQEAwIBhjAd
BgNVHSUEFjAUBggrBgEFBQcDAQYIKwYBBQUHAwIwDQYJKoZIhvcNAQEFBQADgYEA
u1CFt+f41DhmsOWkaT7SLBR8ODmdq91ta8vYP+L3Ws2fZ2tUNH/DX/lofR90GXA3
L5W8aWhQYdk+S7zuCFmt18QFjRXX0szbLawGRA+t4zQy/AeOIVnmrlKSs6rQ4I+e
yuoyUfLE8+ULl92NZbj3pHKnWLddD7uVK2GYHr/P8kQ=
-----END CERTIFICATE-----
`
	testCertificateRsaSha256Key = `-----BEGIN PRIVATE KEY-----
MIICdwIBADANBgkqhkiG9w0BAQEFAASCAmEwggJdAgEAAoGBANiNMnmpMORg+X9T
ujvAXx1ysM9SzuYLX5SKhxq9SiSqKE+YZjxpkf2EvBKraxgKIBEHrGpn5CX2YKyc
T0Tio6G98/8O/xyDAqdHIE5PCD9srz5INtw5Vx9uLbtSOPwzLoN6qQIH31rdXShd
kKVKDegsKgPaRPBlY1O43sXgkCahAgMBAAECgYAxSVGnpv5dvESM2j2Uw9/iD+x2
A17btNL4N98wEs0BM0khdIowTcbQcJltll41hnht59UyEps2mLDAGINiJkMfbWyD
uKuy1Lmo/+QE4hTZ7VSIoznmpQr4XjytHmSVP5JYBSQIG/uCSg2OoMwjwnXLO6rO
NbYX2392upZZm135UQJBAPzubZkElK19qU0hbMwWfgJE2OwYuo6lS/3x/l40sgHq
NbkurL8W/NIF5v+/X9DOCYbUqp8E0DtLZPmXebACDoMCQQDbLccxv9erBBq2+YNJ
P2ZYzHbwSrj98NLMwdMkutbbHd2521DSXbaT0mdb2QT3MpdK0PT98JnJccI53vHQ
ua0LAkEAoLFGVjIv121/s24p9hvQINbmzlEDrX7dIdCuH+HwugC38xfxTlJne3Oe
iBto33sXWF8iq3beaN2EoIIZILadywJAD+K7g0GSUhTUEtr2xwJPWrRHEpd33P/t
Z2XM9eaM2AjMH0JkEzszlnczgpayI3CJQqTufNFJdC5Ik4UzJZuvjQJBAM8cYMDt
tO6ylsZ2JWKlnsFVW0Nsx696Y3dLygymVLlU607/a7QP9Lakf9XwI8dSmZDIuW9l
w0VeEQOmXrayLUM=
-----END PRIVATE KEY-----
`

	// openssl req -newkey rsa:1024 -nodes -keyout testCertificateRsaSha1IntCA.key -out testCertificateRsaSha1IntCA.csr -subj '/CN=www.example-int.com/ST=SC/C=US/emailAddress=example@example.com/O=Example/OU=Example'
	// openssl req -x509 -days 3650 -sha1 -in testCertificateRsaSha1IntCA.csr -CA testCertificateRsaSha1SelfSignedRootCA.crt -CAkey testCertificateRsaSha1SelfSignedRootCA.key -addext "keyUsage=cRLSign,  digitalSignature, keyCertSign" -addext "extendedKeyUsage=serverAuth, clientAuth" -nodes -out testCertificateRsaSha1IntCA.crt
	//
	// Key = testCertificateRsaSha1IntCAKey
	// CA = testCertificateRsaSha1SelfSignedRootCA
	testCertificateRsaSha1IntCA = `-----BEGIN CERTIFICATE-----
MIIDKzCCApSgAwIBAgIUHQKtMkN+OTAgVcWesPBcLGdKKyYwDQYJKoZIhvcNAQEF
BQAwgaExCzAJBgNVBAYTAlVTMQswCQYDVQQIDAJTQzEVMBMGA1UEBwwMRGVmYXVs
dCBDaXR5MRwwGgYDVQQKDBNEZWZhdWx0IENvbXBhbnkgTHRkMRAwDgYDVQQLDAdU
ZXN0IENBMRowGAYDVQQDDBF3d3cuZXhhbXBsZWNhLmNvbTEiMCAGCSqGSIb3DQEJ
ARYTZXhhbXBsZUBleGFtcGxlLmNvbTAeFw0yNDEyMTIwMTE1MTZaFw0zNDEyMTAw
MTE1MTZaMIGAMRwwGgYDVQQDDBN3d3cuZXhhbXBsZS1pbnQuY29tMQswCQYDVQQI
DAJTQzELMAkGA1UEBhMCVVMxIjAgBgkqhkiG9w0BCQEWE2V4YW1wbGVAZXhhbXBs
ZS5jb20xEDAOBgNVBAoMB0V4YW1wbGUxEDAOBgNVBAsMB0V4YW1wbGUwgZ8wDQYJ
KoZIhvcNAQEBBQADgY0AMIGJAoGBAMhQWsJpwgn9o/3tMXh9UVwuU8f+FzYWI3ff
F0XrI9M3Th1DvxwCldx818S1QrnJoSVH8BMFacdXFFMo48xcRSO9muS05KY5xVmU
3J96Ylca/oNsk5w7vKzuGFX4LcnfD3iRC7ZyfTi8ZgkrFIk0TmB6WHQUACh5Atnq
uFQPzOeBAgMBAAGjfzB9MB0GA1UdDgQWBBQ+Q7pxwLdGiV1AX0JBsh0mu6+UnzAf
BgNVHSMEGDAWgBRcxFzhkQELDqWRGp2Hjnb+PHSDYzAPBgNVHRMBAf8EBTADAQH/
MAsGA1UdDwQEAwIBhjAdBgNVHSUEFjAUBggrBgEFBQcDAQYIKwYBBQUHAwIwDQYJ
KoZIhvcNAQEFBQADgYEAYVUcnEMt4ybpAsje3torX95N/7gsdmSsZCLWHPy6mkLB
YpKUy/tiwcKkStau30HU3glfwD/ys+8SrXERRodU+ja5FOr3Usj74GGzwYI10PE/
0FjI+IHPPK1F1djbJmjeEG2nT7qa51ugN3pmf6ci2SfamuLDjEI7EUTUwqCbw2E=
-----END CERTIFICATE-----
`
	// Key is not used, but keep here for reference for signing new certs if needed.
	testCertificateRsaSha1IntCAKey = `-----BEGIN PRIVATE KEY-----
MIICdwIBADANBgkqhkiG9w0BAQEFAASCAmEwggJdAgEAAoGBAMhQWsJpwgn9o/3t
MXh9UVwuU8f+FzYWI3ffF0XrI9M3Th1DvxwCldx818S1QrnJoSVH8BMFacdXFFMo
48xcRSO9muS05KY5xVmU3J96Ylca/oNsk5w7vKzuGFX4LcnfD3iRC7ZyfTi8Zgkr
FIk0TmB6WHQUACh5AtnquFQPzOeBAgMBAAECgYA+rf4oVWV1MNvOyhivxi7eNFTd
AKIMt5KzoKgspa5ZGjYkLB2xyxFPo/T0RW+yqOf2vXLe0NPPn2zptKLLQJgVUDer
zNkVH+aw2WTgC7QXzCz1CuJALoIh3R/uz3Ksdqc3QgjQkPsECKwJDdqKQmlhGvCR
XWIfT96CHF1Xv/2QXQJBAON/E6QcR3BJsa/nkN/5bzYNtM+UKnZmZueCGdEyOuJE
AGUdbGdV9Tg6WWUZn2j5VGF41s/l8KL4umzm7rL/0pMCQQDhaWbS+meWkKGdNI4q
TkwSRX4iRLotq6gEsSAVoxBWfjjtRmcuxi3WFjflMa1ZfKBJCgEvVhFpb1ow0FVa
vMYbAkEA049PoqQxwzilJ2J/leoPBAOHDCtLucPNGqogfCzsGZMHkwDj2M1VOC77
B0vmtOZ5FBQeIERDnisUo0W24XuKRQJBAJJHtmS//61kGp1MV934hcFtu5c9hpzQ
wu6Yi7u+4IFg1EyW3asrDN/b91YTUO27xMDhbzdq4U3M53i6GkoSK3UCQFN5heH6
qOKGY3fpKIlWEk5W9wLZZlguOcCmOR1f3n/PtRC+71lFFpjDzeCyONoWEtLY4TgR
XseuoC1yof+Q05c=
-----END PRIVATE KEY-----
`
)

// TestRouter provides an implementation of the plugin's router interface suitable for unit testing.
type TestRouter struct {
	State        map[ServiceAliasConfigKey]ServiceAliasConfig
	ServiceUnits map[ServiceUnitKey]ServiceUnit
}

// NewTestRouter creates a new TestRouter and registers the initial state.
func newTestRouter(state map[ServiceAliasConfigKey]ServiceAliasConfig) *TestRouter {
	return &TestRouter{
		State:        state,
		ServiceUnits: make(map[ServiceUnitKey]ServiceUnit),
	}
}

// CreateServiceUnit creates an empty service unit identified by id
func (r *TestRouter) CreateServiceUnit(id ServiceUnitKey) {
	su := ServiceUnit{
		Name:          string(id),
		EndpointTable: []Endpoint{},
	}

	r.ServiceUnits[id] = su
}

// FindServiceUnit finds the service unit in the state
func (r *TestRouter) FindServiceUnit(id ServiceUnitKey) (v ServiceUnit, ok bool) {
	v, ok = r.ServiceUnits[id]
	return
}

// AddEndpoints adds the endpoints to the service unit identified by id
func (r *TestRouter) AddEndpoints(id ServiceUnitKey, endpoints []Endpoint) {
	su, _ := r.FindServiceUnit(id)

	// simulate the logic that compares endpoints
	if reflect.DeepEqual(su.EndpointTable, endpoints) {
		return
	}
	su.EndpointTable = endpoints
	r.ServiceUnits[id] = su
}

// DeleteEndpoints removes all endpoints from the service unit
func (r *TestRouter) DeleteEndpoints(id ServiceUnitKey) {
	if su, ok := r.FindServiceUnit(id); !ok {
		return
	} else {
		su.EndpointTable = []Endpoint{}
		r.ServiceUnits[id] = su
	}
}

func (r *TestRouter) calculateServiceWeights(serviceUnits map[ServiceUnitKey]int32) map[ServiceUnitKey]int32 {
	var serviceWeights = make(map[ServiceUnitKey]int32)
	for key := range serviceUnits {
		serviceWeights[key] = serviceUnits[key]
	}
	return serviceWeights
}

// AddRoute adds a ServiceAliasConfig and associated ServiceUnits for the route
func (r *TestRouter) AddRoute(route *routev1.Route) {
	routeKey := getKey(route)

	config := ServiceAliasConfig{
		Host:         route.Spec.Host,
		Path:         route.Spec.Path,
		ServiceUnits: getServiceUnits(route),
	}
	config.ServiceUnitNames = r.calculateServiceWeights(config.ServiceUnits)

	for key := range config.ServiceUnits {
		r.CreateServiceUnit(key)
	}

	r.State[routeKey] = config
}

// RemoveRoute removes the service alias config for Route
func (r *TestRouter) RemoveRoute(route *routev1.Route) {
	routeKey := getKey(route)
	_, ok := r.State[routeKey]
	if !ok {
		return
	} else {
		delete(r.State, routeKey)
	}
}

func (r *TestRouter) HasRoute(route *routev1.Route) bool {
	// Not used
	return false
}

func (r *TestRouter) SyncedAtLeastOnce() bool {
	// Not used
	return false
}

func (r *TestRouter) FilterNamespaces(namespaces sets.String) {
	if len(namespaces) == 0 {
		r.State = make(map[ServiceAliasConfigKey]ServiceAliasConfig)
		r.ServiceUnits = make(map[ServiceUnitKey]ServiceUnit)
	}
	for k := range r.ServiceUnits {
		// TODO: the id of a service unit should be defined inside this class, not passed in from the outside
		//   remove the leak of the abstraction when we refactor this code
		ns, _ := getPartsFromEndpointsKey(k)
		if namespaces.Has(ns) {
			continue
		}
		delete(r.ServiceUnits, k)
	}

	for k := range r.State {
		ns, _ := getPartsFromRouteKey(k)
		if namespaces.Has(ns) {
			continue
		}
		delete(r.State, k)
	}
}

// getKey create an identifier for the route consisting of host-path
func getKey(route *routev1.Route) ServiceAliasConfigKey {
	return routeKeyFromParts(route.Spec.Host, route.Spec.Path)
}

func (r *TestRouter) Commit() {
	// No op
}

// TestHandleEndpoints test endpoint watch events
func TestHandleEndpoints(t *testing.T) {
	testCases := []struct {
		name                string          //human readable name for test case
		eventType           watch.EventType //type to be passed to the HandleEndpoints method
		endpoints           *kapi.Endpoints //endpoints to be passed to the HandleEndpoints method
		expectedServiceUnit *ServiceUnit    //service unit that will be compared against.
		excludeUDP          bool
	}{
		{
			name:      "Endpoint add",
			eventType: watch.Added,
			endpoints: &kapi.Endpoints{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "foo",
					Name:      "test", //kapi.endpoints inherits the name of the service
				},
				Subsets: []kapi.EndpointSubset{{
					Addresses: []kapi.EndpointAddress{{IP: "1.1.1.1"}},
					Ports:     []kapi.EndpointPort{{Port: 345, Name: "port"}},
				}}, //not specifying a port to force the port 80 assumption
			},
			expectedServiceUnit: &ServiceUnit{
				Name: "foo/test", //service name from kapi.endpoints object
				EndpointTable: []Endpoint{
					{
						ID:   "ept:test:port:1.1.1.1:345",
						IP:   "1.1.1.1",
						Port: "345",
					},
				},
			},
		},
		{
			name:      "Endpoint mod",
			eventType: watch.Modified,
			endpoints: &kapi.Endpoints{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "foo",
					Name:      "test",
				},
				Subsets: []kapi.EndpointSubset{{
					Addresses: []kapi.EndpointAddress{{IP: "2.2.2.2", TargetRef: &kapi.ObjectReference{Kind: "Pod", Name: "pod-1"}}},
					Ports:     []kapi.EndpointPort{{Port: 8080, Name: "port"}},
				}},
			},
			expectedServiceUnit: &ServiceUnit{
				Name: "foo/test",
				EndpointTable: []Endpoint{
					{
						ID:   "pod:pod-1:test:port:2.2.2.2:8080",
						IP:   "2.2.2.2",
						Port: "8080",
					},
				},
			},
		},
		{
			name:      "Endpoint delete",
			eventType: watch.Deleted,
			endpoints: &kapi.Endpoints{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "foo",
					Name:      "test",
				},
				Subsets: []kapi.EndpointSubset{{
					Addresses: []kapi.EndpointAddress{{IP: "3.3.3.3"}},
					Ports:     []kapi.EndpointPort{{Port: 0}},
				}},
			},
			expectedServiceUnit: &ServiceUnit{
				Name:          "foo/test",
				EndpointTable: []Endpoint{},
			},
		},
	}

	router := newTestRouter(make(map[ServiceAliasConfigKey]ServiceAliasConfig))
	templatePlugin := newDefaultTemplatePlugin(router, true, nil)
	// TODO: move tests that rely on unique hosts to pkg/router/controller and remove them from
	// here
	plugin := controller.NewUniqueHost(templatePlugin, false, controller.LogRejections)

	for _, tc := range testCases {
		plugin.HandleEndpoints(tc.eventType, tc.endpoints)

		su, ok := router.FindServiceUnit(ServiceUnitKey(tc.expectedServiceUnit.Name))

		if !ok {
			t.Errorf("TestHandleEndpoints test case %s failed.  Couldn't find expected service unit with name %s", tc.name, tc.expectedServiceUnit.Name)
		} else {
			if len(su.EndpointTable) != len(tc.expectedServiceUnit.EndpointTable) {
				t.Errorf("TestHandleEndpoints test case %s failed. endpoints: %d expected %d", tc.name, len(su.EndpointTable), len(tc.expectedServiceUnit.EndpointTable))
			}
			for expectedKey, expectedEp := range tc.expectedServiceUnit.EndpointTable {
				actualEp := su.EndpointTable[expectedKey]

				if expectedEp.ID != actualEp.ID || expectedEp.IP != actualEp.IP || expectedEp.Port != actualEp.Port {
					t.Errorf("TestHandleEndpoints test case %s failed.  Expected endpoint didn't match actual endpoint %v : %v", tc.name, expectedEp, actualEp)
				}
			}
		}
	}
}

// TestHandleTCPEndpoints test endpoint watch events with UDP excluded
func TestHandleTCPEndpoints(t *testing.T) {
	testCases := []struct {
		name                string          //human readable name for test case
		eventType           watch.EventType //type to be passed to the HandleEndpoints method
		endpoints           *kapi.Endpoints //endpoints to be passed to the HandleEndpoints method
		expectedServiceUnit *ServiceUnit    //service unit that will be compared against.
	}{
		{
			name:      "Endpoint add",
			eventType: watch.Added,
			endpoints: &kapi.Endpoints{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "foo",
					Name:      "test", //kapi.endpoints inherits the name of the service
				},
				Subsets: []kapi.EndpointSubset{{
					Addresses: []kapi.EndpointAddress{{IP: "1.1.1.1", TargetRef: &kapi.ObjectReference{Kind: "Pod", Name: "pod-1"}}},
					Ports: []kapi.EndpointPort{
						{Port: 345, Name: "tcp"},
						{Port: 346, Protocol: kapi.ProtocolUDP, Name: "udp"},
					},
				}}, //not specifying a port to force the port 80 assumption
			},
			expectedServiceUnit: &ServiceUnit{
				Name: "foo/test", //service name from kapi.endpoints object
				EndpointTable: []Endpoint{
					{
						ID:   "pod:pod-1:test:tcp:1.1.1.1:345",
						IP:   "1.1.1.1",
						Port: "345",
					},
				},
			},
		},
		{
			name:      "Endpoint mod",
			eventType: watch.Modified,
			endpoints: &kapi.Endpoints{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "foo",
					Name:      "test",
				},
				Subsets: []kapi.EndpointSubset{{
					Addresses: []kapi.EndpointAddress{{IP: "2.2.2.2"}},
					Ports: []kapi.EndpointPort{
						{Port: 8080, Name: "tcp"},
						{Port: 8081, Protocol: kapi.ProtocolUDP, Name: "udp"},
					},
				}},
			},
			expectedServiceUnit: &ServiceUnit{
				Name: "foo/test",
				EndpointTable: []Endpoint{
					{
						ID:   "ept:test:tcp:2.2.2.2:8080",
						IP:   "2.2.2.2",
						Port: "8080",
					},
				},
			},
		},
		{
			name:      "Endpoint delete",
			eventType: watch.Deleted,
			endpoints: &kapi.Endpoints{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "foo",
					Name:      "test",
				},
				Subsets: []kapi.EndpointSubset{{
					Addresses: []kapi.EndpointAddress{{IP: "3.3.3.3"}},
					Ports:     []kapi.EndpointPort{{Port: 0}},
				}},
			},
			expectedServiceUnit: &ServiceUnit{
				Name:          "foo/test",
				EndpointTable: []Endpoint{},
			},
		},
	}

	router := newTestRouter(make(map[ServiceAliasConfigKey]ServiceAliasConfig))
	templatePlugin := newDefaultTemplatePlugin(router, false, nil)
	// TODO: move tests that rely on unique hosts to pkg/router/controller and remove them from
	// here
	plugin := controller.NewUniqueHost(templatePlugin, false, controller.LogRejections)

	for _, tc := range testCases {
		plugin.HandleEndpoints(tc.eventType, tc.endpoints)

		su, ok := router.FindServiceUnit(ServiceUnitKey(tc.expectedServiceUnit.Name))

		if !ok {
			t.Errorf("TestHandleEndpoints test case %s failed.  Couldn't find expected service unit with name %s", tc.name, tc.expectedServiceUnit.Name)
		} else {
			for expectedKey, expectedEp := range tc.expectedServiceUnit.EndpointTable {
				actualEp := su.EndpointTable[expectedKey]

				if expectedEp.ID != actualEp.ID || expectedEp.IP != actualEp.IP || expectedEp.Port != actualEp.Port {
					t.Errorf("TestHandleEndpoints test case %s failed.  Expected endpoint didn't match actual endpoint %v : %v", tc.name, expectedEp, actualEp)
				}
			}
		}
	}
}

type status struct {
	route   *routev1.Route
	reason  string
	message string
}

type fakeStatusRecorder struct {
	rejections                 []status
	unservableInFutureVersions []status
}

func (r *fakeStatusRecorder) RecordRouteRejection(route *routev1.Route, reason, message string) {
	r.rejections = append(r.rejections, status{route: route, reason: reason, message: message})
}
func (r *fakeStatusRecorder) RecordRouteUnservableInFutureVersions(route *routev1.Route, reason, message string) {
	r.unservableInFutureVersions = append(r.unservableInFutureVersions, status{route: route, reason: reason, message: message})
}
func (r *fakeStatusRecorder) RecordRouteUnservableInFutureVersionsClear(route *routev1.Route) {
	var unservableInFutureVersions []status
	for _, entry := range r.unservableInFutureVersions {
		if entry.route.UID != route.UID {
			unservableInFutureVersions = append(unservableInFutureVersions, entry)
		}
	}
	r.unservableInFutureVersions = unservableInFutureVersions
}

func (r *fakeStatusRecorder) isUnservableInFutureVersions(route *routev1.Route) bool {
	for _, r := range r.unservableInFutureVersions {
		if r.route.UID == route.UID {
			return true
		}
	}
	return false
}

// TestHandleRoute test route watch events
func TestHandleRoute(t *testing.T) {
	rejections := &fakeStatusRecorder{}
	router := newTestRouter(make(map[ServiceAliasConfigKey]ServiceAliasConfig))
	templatePlugin := newDefaultTemplatePlugin(router, true, nil)
	// TODO: move tests that rely on unique hosts to pkg/router/controller and remove them from
	// here
	plugin := controller.NewUniqueHost(templatePlugin, false, rejections)

	uidCount := 0
	nextUID := func() types.UID {
		uidCount++
		return types.UID(fmt.Sprintf("%03d", uidCount))
	}
	original := metav1.Time{Time: time.Now()}

	//add
	fooTest1 := &routev1.Route{
		ObjectMeta: metav1.ObjectMeta{
			CreationTimestamp: original,
			Namespace:         "foo",
			Name:              "test",
			UID:               nextUID(),
		},
		Spec: routev1.RouteSpec{
			Host: "www.example.com",
			To: routev1.RouteTargetReference{
				Name:   "TestService",
				Weight: new(int32),
			},
		},
	}
	serviceUnitKey := endpointsKeyFromParts(fooTest1.Namespace, fooTest1.Spec.To.Name)

	plugin.HandleRoute(watch.Added, fooTest1)

	_, ok := router.FindServiceUnit(serviceUnitKey)

	if !ok {
		t.Errorf("TestHandleRoute was unable to find the service unit %s after HandleRoute was called", fooTest1.Spec.To.Name)
	} else {
		serviceAliasCfg, ok := router.State[getKey(fooTest1)]

		if !ok {
			t.Errorf("TestHandleRoute expected route key %s", getKey(fooTest1))
		} else {
			if serviceAliasCfg.Host != fooTest1.Spec.Host || serviceAliasCfg.Path != fooTest1.Spec.Path {
				t.Errorf("Expected route did not match service alias config %v : %v", fooTest1, serviceAliasCfg)
			}
		}
	}

	if len(rejections.rejections) > 0 {
		t.Fatalf("did not expect a recorded status: %#v", rejections)
	}

	// attempt to add a second route with a newer time, verify it is ignored
	fooDupe2 := &routev1.Route{
		ObjectMeta: metav1.ObjectMeta{
			CreationTimestamp: metav1.Time{Time: original.Add(time.Hour)},
			Namespace:         "foo",
			Name:              "dupe",
			UID:               nextUID(),
		},
		Spec: routev1.RouteSpec{
			Host: "www.example.com",
			To: routev1.RouteTargetReference{
				Name:   "TestService2",
				Weight: new(int32),
			},
		},
	}
	if err := plugin.HandleRoute(watch.Added, fooDupe2); err != nil {
		t.Fatal("unexpected error")
	}

	if _, ok := router.FindServiceUnit(endpointsKeyFromParts("foo", "TestService2")); ok {
		t.Fatalf("unexpected second unit: %#v", router)
	}
	if r, ok := plugin.RoutesForHost("www.example.com"); !ok || r[0].Name != "test" {
		t.Fatalf("unexpected claimed routes: %#v", r)
	}
	if len(rejections.rejections) != 1 ||
		rejections.rejections[0].route.Name != "dupe" ||
		rejections.rejections[0].reason != "HostAlreadyClaimed" ||
		rejections.rejections[0].message != "route test already exposes www.example.com and is older" {
		t.Fatalf("did not record status: %#v", rejections)
	}
	rejections.rejections = nil

	// attempt to remove the second route that is not being used, verify it is ignored
	if err := plugin.HandleRoute(watch.Deleted, fooDupe2); err != nil {
		t.Fatal("unexpected error")
	}

	if _, ok := router.FindServiceUnit(endpointsKeyFromParts("foo", "TestService2")); ok {
		t.Fatalf("unexpected second unit: %#v", router)
	}
	if _, ok := router.FindServiceUnit(endpointsKeyFromParts("foo", "TestService")); !ok {
		t.Fatalf("unexpected first unit: %#v", router)
	}
	if r, ok := plugin.RoutesForHost("www.example.com"); !ok || r[0].Name != "test" {
		t.Fatalf("unexpected claimed routes: %#v", r)
	}
	if len(rejections.rejections) != 0 {
		t.Fatalf("did not record status: %#v", rejections)
	}
	rejections.rejections = nil

	// add a third route with an older time, verify it takes effect
	copied := *fooDupe2
	fooDupe3 := &copied
	fooDupe3.UID = nextUID()
	fooDupe3.CreationTimestamp = metav1.Time{Time: original.Add(-time.Hour)}
	if err := plugin.HandleRoute(watch.Added, fooDupe3); err != nil {
		t.Fatal("unexpected error")
	}
	_, ok = router.FindServiceUnit(endpointsKeyFromParts("foo", "TestService2"))
	if !ok {
		t.Fatalf("missing second unit: %#v", router)
	}
	if len(rejections.rejections) != 1 ||
		rejections.rejections[0].route.Name != "test" ||
		rejections.rejections[0].reason != "HostAlreadyClaimed" ||
		rejections.rejections[0].message != "replaced by older route dupe" {
		t.Fatalf("did not record status: %#v", rejections)
	}
	rejections.rejections = nil

	//mod
	copied2 := *fooTest1
	fooTest1NewHost := &copied2
	fooTest1NewHost.Spec.Host = "www.example2.com"
	if err := plugin.HandleRoute(watch.Modified, fooTest1NewHost); err != nil {
		t.Fatal("unexpected error")
	}

	key := getKey(fooTest1NewHost)
	_, ok = router.FindServiceUnit(serviceUnitKey)
	if !ok {
		t.Errorf("TestHandleRoute was unable to find the service unit %s after HandleRoute was called", fooTest1NewHost.Spec.To.Name)
	} else {
		serviceAliasCfg, ok := router.State[key]
		if !ok {
			t.Errorf("TestHandleRoute expected route key %s", key)
		} else {
			if serviceAliasCfg.Host != fooTest1NewHost.Spec.Host || serviceAliasCfg.Path != fooTest1NewHost.Spec.Path {
				t.Errorf("Expected route did not match service alias config %v : %v", fooTest1NewHost, serviceAliasCfg)
			}
		}
	}
	if plugin.HostLen() != 2 {
		t.Fatalf("did not clear claimed route: %#v", plugin)
	}
	if len(rejections.rejections) != 0 {
		t.Fatalf("unexpected status: %#v", rejections)
	}

	plugin.HandleRoute(watch.Deleted, fooDupe3)

	if plugin.HostLen() != 1 {
		t.Fatalf("did not clear claimed route: %#v", plugin)
	}
	if len(rejections.rejections) != 0 {
		t.Fatalf("unexpected status: %#v", rejections)
	}

	//delete
	if err := plugin.HandleRoute(watch.Deleted, fooTest1NewHost); err != nil {
		t.Fatal("unexpected error")
	}
	_, ok = router.FindServiceUnit(serviceUnitKey)
	if !ok {
		t.Errorf("TestHandleRoute was unable to find the service unit %s after HandleRoute was called", fooTest1NewHost.Spec.To.Name)
	} else {

		_, ok := router.State[key]

		if ok {
			t.Errorf("TestHandleRoute did not expect route key %s", key)
		}
	}
	if plugin.HostLen() != 0 {
		t.Errorf("did not clear claimed route: %#v", plugin)
	}
	if len(rejections.rejections) != 0 {
		t.Fatalf("unexpected status: %#v", rejections)
	}
}

type fakePlugin struct {
	Route *routev1.Route
	Err   error
}

func (p *fakePlugin) HandleRoute(event watch.EventType, route *routev1.Route) error {
	p.Route = route
	return p.Err
}

func (p *fakePlugin) HandleEndpoints(watch.EventType, *kapi.Endpoints) error {
	return p.Err
}

func (p *fakePlugin) HandleNamespaces(namespaces sets.String) error {
	return p.Err
}

func (p *fakePlugin) HandleNode(watch.EventType, *kapi.Node) error {
	return p.Err
}

func (p *fakePlugin) Commit() error {
	return p.Err
}

// TestHandleRouteExtendedValidation test route watch events with extended route configuration validation.
func TestHandleRouteExtendedValidation(t *testing.T) {
	rejections := &fakeStatusRecorder{}
	fake := &fakePlugin{}
	plugin := controller.NewExtendedValidator(fake, rejections)

	original := metav1.Time{Time: time.Now()}

	//add
	route := &routev1.Route{
		ObjectMeta: metav1.ObjectMeta{
			CreationTimestamp: original,
			Namespace:         "foo",
			Name:              "test",
		},
		Spec: routev1.RouteSpec{
			Host: "www.example.com",
			To: routev1.RouteTargetReference{
				Name:   "TestService",
				Weight: new(int32),
			},
		},
	}

	plugin.HandleRoute(watch.Added, route)
	if fake.Route != route {
		t.Fatalf("unexpected route: %#v", fake.Route)
	}

	if len(rejections.rejections) > 0 {
		t.Fatalf("did not expect a recorded status: %#v", rejections)
	}

	tests := []struct {
		name          string
		route         *routev1.Route
		errorExpected bool
	}{
		{
			name: "No TLS Termination",
			route: &routev1.Route{
				Spec: routev1.RouteSpec{
					Host: "www.no.tls.test",
					TLS: &routev1.TLSConfig{
						Termination: "",
					},
				},
			},
			errorExpected: true,
		},
		{
			name: "Passthrough termination OK",
			route: &routev1.Route{
				Spec: routev1.RouteSpec{
					Host: "www.passthrough.test",
					TLS: &routev1.TLSConfig{
						Termination: routev1.TLSTerminationPassthrough,
					},
				},
			},
			errorExpected: false,
		},
		{
			name: "Reencrypt termination OK with certs",
			route: &routev1.Route{
				Spec: routev1.RouteSpec{
					Host: "www.example.com",

					TLS: &routev1.TLSConfig{
						Termination:              routev1.TLSTerminationReencrypt,
						Certificate:              testCertificate,
						Key:                      testPrivateKey,
						CACertificate:            testCACertificate,
						DestinationCACertificate: testDestinationCACertificate,
					},
				},
			},
			errorExpected: false,
		},
		{
			name: "Reencrypt termination OK with bad config",
			route: &routev1.Route{
				Spec: routev1.RouteSpec{
					Host: "www.reencrypt.badconfig.test",
					TLS: &routev1.TLSConfig{
						Termination:              routev1.TLSTerminationReencrypt,
						Certificate:              "def",
						Key:                      "ghi",
						CACertificate:            "jkl",
						DestinationCACertificate: "abc",
					},
				},
			},
			errorExpected: true,
		},
		{
			name: "Reencrypt termination OK without certs",
			route: &routev1.Route{
				Spec: routev1.RouteSpec{
					Host: "www.reencrypt.nocerts.test",
					TLS: &routev1.TLSConfig{
						Termination:              routev1.TLSTerminationReencrypt,
						DestinationCACertificate: testDestinationCACertificate,
					},
				},
			},
			errorExpected: false,
		},
		{
			name: "Reencrypt termination bad config without certs",
			route: &routev1.Route{
				Spec: routev1.RouteSpec{
					Host: "www.reencrypt.badconfignocerts.test",
					TLS: &routev1.TLSConfig{
						Termination:              routev1.TLSTerminationReencrypt,
						DestinationCACertificate: "abc",
					},
				},
			},
			errorExpected: true,
		},
		{
			name: "Reencrypt termination no dest cert",
			route: &routev1.Route{
				Spec: routev1.RouteSpec{
					Host: "www.reencrypt.nodestcert.test",
					TLS: &routev1.TLSConfig{
						Termination:   routev1.TLSTerminationReencrypt,
						Certificate:   testCertificate,
						Key:           testPrivateKey,
						CACertificate: testCACertificate,
					},
				},
			},
			errorExpected: false,
		},
		{
			name: "Edge termination OK with certs without host",
			route: &routev1.Route{
				Spec: routev1.RouteSpec{
					TLS: &routev1.TLSConfig{
						Termination:   routev1.TLSTerminationEdge,
						Certificate:   testCertificate,
						Key:           testPrivateKey,
						CACertificate: testCACertificate,
					},
				},
			},
			errorExpected: false,
		},
		{
			name: "Edge termination OK with certs",
			route: &routev1.Route{
				Spec: routev1.RouteSpec{
					Host: "www.example.com",
					TLS: &routev1.TLSConfig{
						Termination:   routev1.TLSTerminationEdge,
						Certificate:   testCertificate,
						Key:           testPrivateKey,
						CACertificate: testCACertificate,
					},
				},
			},
			errorExpected: false,
		},
		{
			name: "Edge termination bad config with certs",
			route: &routev1.Route{
				Spec: routev1.RouteSpec{
					Host: "www.edge.badconfig.test",
					TLS: &routev1.TLSConfig{
						Termination:   routev1.TLSTerminationEdge,
						Certificate:   "abc",
						Key:           "abc",
						CACertificate: "abc",
					},
				},
			},
			errorExpected: true,
		},
		{
			name: "Edge termination mismatched key and cert",
			route: &routev1.Route{
				Spec: routev1.RouteSpec{
					Host: "www.edge.mismatchdkeyandcert.test",
					TLS: &routev1.TLSConfig{
						Termination:   routev1.TLSTerminationEdge,
						Certificate:   testCertificate,
						Key:           testExpiredCertKey,
						CACertificate: testCACertificate,
					},
				},
			},
			errorExpected: true,
		},
		{
			name: "Edge termination expired cert",
			route: &routev1.Route{
				Spec: routev1.RouteSpec{
					Host: "www.edge.expiredcert.test",
					TLS: &routev1.TLSConfig{
						Termination:   routev1.TLSTerminationEdge,
						Certificate:   testExpiredCert,
						Key:           testExpiredCertKey,
						CACertificate: testCACertificate,
					},
				},
			},
		},
		{
			name: "Edge termination expired cert key mismatch",
			route: &routev1.Route{
				Spec: routev1.RouteSpec{
					Host: "www.edge.expiredcertkeymismatch.test",
					TLS: &routev1.TLSConfig{
						Termination:   routev1.TLSTerminationEdge,
						Certificate:   testExpiredCert,
						Key:           testPrivateKey,
						CACertificate: testCACertificate,
					},
				},
			},
			errorExpected: true,
		},
		{
			name: "Edge termination OK without certs",
			route: &routev1.Route{
				Spec: routev1.RouteSpec{
					Host: "www.edge.nocerts.test",
					TLS: &routev1.TLSConfig{
						Termination: routev1.TLSTerminationEdge,
					},
				},
			},
			errorExpected: false,
		},
		{
			name: "Edge termination, bad dest cert",
			route: &routev1.Route{
				Spec: routev1.RouteSpec{
					Host: "www.edge.baddestcert.test",
					TLS: &routev1.TLSConfig{
						Termination:              routev1.TLSTerminationEdge,
						DestinationCACertificate: "abc",
					},
				},
			},
			errorExpected: true,
		},
		{
			name: "Passthrough termination, bad cert",
			route: &routev1.Route{
				Spec: routev1.RouteSpec{
					Host: "www.passthrough.badcert.test",
					TLS:  &routev1.TLSConfig{Termination: routev1.TLSTerminationPassthrough, Certificate: "test"},
				},
			},
			errorExpected: true,
		},
		{
			name: "Passthrough termination, bad key",
			route: &routev1.Route{
				Spec: routev1.RouteSpec{
					Host: "www.passthrough.badkey.test",
					TLS:  &routev1.TLSConfig{Termination: routev1.TLSTerminationPassthrough, Key: "test"},
				},
			},
			errorExpected: true,
		},
		{
			name: "Passthrough termination, bad ca cert",
			route: &routev1.Route{
				Spec: routev1.RouteSpec{
					Host: "www.passthrough.badcacert.test",
					TLS:  &routev1.TLSConfig{Termination: routev1.TLSTerminationPassthrough, CACertificate: "test"},
				},
			},
			errorExpected: true,
		},
		{
			name: "Passthrough termination, bad dest ca cert",
			route: &routev1.Route{
				Spec: routev1.RouteSpec{
					Host: "www.passthrough.baddestcacert.test",
					TLS:  &routev1.TLSConfig{Termination: routev1.TLSTerminationPassthrough, DestinationCACertificate: "test"},
				},
			},
			errorExpected: true,
		},
		{
			name: "Invalid termination type",
			route: &routev1.Route{
				Spec: routev1.RouteSpec{
					TLS: &routev1.TLSConfig{
						Termination: "invalid",
					},
				},
			},
			errorExpected: true,
		},
		{
			name: "Double escaped newlines",
			route: &routev1.Route{
				Spec: routev1.RouteSpec{
					Host: "www.reencrypt.doubleescapednewlines.test",
					TLS: &routev1.TLSConfig{
						Termination:              routev1.TLSTerminationReencrypt,
						Certificate:              "d\\nef",
						Key:                      "g\\nhi",
						CACertificate:            "j\\nkl",
						DestinationCACertificate: "j\\nkl",
					},
				},
			},
			errorExpected: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := plugin.HandleRoute(watch.Added, tc.route)
			if tc.errorExpected && err == nil {
				t.Fatal("expected an error, got none")
			} else if !tc.errorExpected && err != nil {
				t.Fatalf("expected no errors, got %v", err)
			}
		})
	}
}

// TestHandleRouteUpgradeValidation tests the upgrade route validation plugin.
func TestHandleRouteUpgradeValidation(t *testing.T) {
	rejections := &fakeStatusRecorder{}
	fake := &fakePlugin{}
	plugin := controller.NewUpgradeValidation(fake, rejections, false, false)

	tests := []struct {
		name                               string
		route                              *routev1.Route
		unservableInFutureVersionsExpected bool
	}{
		{
			name: "route with no cert should not be unservable in future versions",
			route: &routev1.Route{
				Spec: routev1.RouteSpec{
					Host: "www.normal.test",
				},
			},
			unservableInFutureVersionsExpected: false,
		},
		{
			name: "route with SHA256 cert should not be unservable in future versions",
			route: &routev1.Route{
				Spec: routev1.RouteSpec{
					Host: "www.example.com",
					TLS: &routev1.TLSConfig{
						Termination:              routev1.TLSTerminationReencrypt,
						Certificate:              testCertificate,
						Key:                      testPrivateKey,
						CACertificate:            testCACertificate,
						DestinationCACertificate: testDestinationCACertificate,
					},
				},
			},
			unservableInFutureVersionsExpected: false,
		},
		{
			name: "route with invalid certs should not be unservable in future versions",
			route: &routev1.Route{
				Spec: routev1.RouteSpec{
					Host: "www.reencrypt.badconfig.test",
					TLS: &routev1.TLSConfig{
						Termination:              routev1.TLSTerminationReencrypt,
						Certificate:              "def",
						Key:                      "ghi",
						CACertificate:            "jkl",
						DestinationCACertificate: "abc",
					},
				},
			},
			unservableInFutureVersionsExpected: false,
		},
		{
			name: "route with expired cert should not be unservable in future versions",
			route: &routev1.Route{
				Spec: routev1.RouteSpec{
					Host: "www.edge.expiredcert.test",
					TLS: &routev1.TLSConfig{
						Termination:   routev1.TLSTerminationEdge,
						Certificate:   testExpiredCert,
						Key:           testExpiredCertKey,
						CACertificate: testCACertificate,
					},
				},
			},
			unservableInFutureVersionsExpected: false,
		},
		{
			name: "SHA1 certificate should be unservable in future versions",
			route: &routev1.Route{
				Spec: routev1.RouteSpec{
					Host: "www.reencrypt.sha1.test",
					TLS: &routev1.TLSConfig{
						Termination: routev1.TLSTerminationReencrypt,
						Certificate: testCertificateRsaSha1,
						Key:         testCertificateRsaSha1Key,
					},
				},
			},
			unservableInFutureVersionsExpected: true,
		},
		{
			name: "Edge termination with self-signed cert using SHA1 RSA is not unservable in future versions",
			route: &routev1.Route{
				Spec: routev1.RouteSpec{
					TLS: &routev1.TLSConfig{
						Termination: routev1.TLSTerminationEdge,
						Certificate: testCertificateRsaSha1SelfSigned,
						Key:         testCertificateRsaSha1SelfSignedKey,
					},
				},
			},
			unservableInFutureVersionsExpected: false,
		},
		{
			name: "Reencrypt termination with destination CA root and intermediate cert using SHA1 RSA is unservable in future versions",
			route: &routev1.Route{
				Spec: routev1.RouteSpec{
					TLS: &routev1.TLSConfig{
						Termination:              routev1.TLSTerminationReencrypt,
						DestinationCACertificate: testCertificateRsaSha1SelfSignedRootCA + testCertificateRsaSha1IntCA,
					},
				},
			},
			unservableInFutureVersionsExpected: true,
		},
		{
			// Root CAs are self-signed; therefore not subject to signature algorithm restrictions.
			name: "Reencrypt termination with self-signed destination CA root cert using SHA1 with RSA key is not unservable in future versions",
			route: &routev1.Route{
				Spec: routev1.RouteSpec{
					TLS: &routev1.TLSConfig{
						Termination:              routev1.TLSTerminationReencrypt,
						DestinationCACertificate: testCertificateRsaSha1SelfSignedRootCA,
					},
				},
			},
			unservableInFutureVersionsExpected: false,
		},
		{
			// Root CAs are self-signed; therefore not subject to signature algorithm restrictions.
			name: "Edge termination with self-signed root CA cert using SHA1 and server cert using SHA256 is not unservable in future versions",
			route: &routev1.Route{
				Spec: routev1.RouteSpec{
					TLS: &routev1.TLSConfig{
						Termination:   routev1.TLSTerminationEdge,
						CACertificate: testCertificateRsaSha1SelfSignedRootCA,
						Certificate:   testCertificateRsaSha256,
						Key:           testCertificateRsaSha256Key,
					},
				},
			},
			unservableInFutureVersionsExpected: false,
		},
		{
			// Intermediate CAs are NOT self-signed; therefore are subject to signature algorithm restrictions.
			name: "Edge termination with root CA cert using SHA1, intermediate cert using SHA1, and server cert using SHA256 is unservable in future versions",
			route: &routev1.Route{
				Spec: routev1.RouteSpec{
					TLS: &routev1.TLSConfig{
						Termination:   routev1.TLSTerminationEdge,
						CACertificate: testCertificateRsaSha1SelfSignedRootCA + testCertificateRsaSha1IntCA,
						Certificate:   testCertificateRsaSha256,
						Key:           testCertificateRsaSha256Key,
					},
				},
			},
			unservableInFutureVersionsExpected: true,
		},
		{
			// Make sure our isSelfSignedCert function doesn't assume that when subject is
			// equal to the issuer that it's a self-signed cert.
			name: "Edge termination with CA-signed cert using SHA1, but cert subject is equal to issuer is still unservable in future versions",
			route: &routev1.Route{
				Spec: routev1.RouteSpec{
					TLS: &routev1.TLSConfig{
						Termination: routev1.TLSTerminationEdge,
						Certificate: testCertificateRsaSha1SameSubjIssuer,
						Key:         testCertificateRsaSha1SameSubjIssuerKey,
					},
				},
			},
			unservableInFutureVersionsExpected: true,
		},
	}

	uidCount := 0
	nextUID := func() types.UID {
		uidCount++
		return types.UID(fmt.Sprintf("%03d", uidCount))
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tc.route.UID = nextUID()
			plugin.HandleRoute(watch.Added, tc.route)
			unservableInFutureVersions := rejections.isUnservableInFutureVersions(tc.route)
			if tc.unservableInFutureVersionsExpected != unservableInFutureVersions {
				t.Fatalf("expected to be unservableInFutureVersions=%t, got unservableInFutureVersions=%t", tc.unservableInFutureVersionsExpected, unservableInFutureVersions)
			}
		})
	}
}

func TestNamespaceScopingFromEmpty(t *testing.T) {
	router := newTestRouter(make(map[ServiceAliasConfigKey]ServiceAliasConfig))
	templatePlugin := newDefaultTemplatePlugin(router, true, nil)
	// TODO: move tests that rely on unique hosts to pkg/router/controller and remove them from
	// here
	plugin := controller.NewUniqueHost(templatePlugin, false, controller.LogRejections)

	// no namespaces allowed
	plugin.HandleNamespaces(sets.String{})

	//add
	route := &routev1.Route{
		ObjectMeta: metav1.ObjectMeta{Namespace: "foo", Name: "test"},
		Spec: routev1.RouteSpec{
			Host: "www.example.com",
			To: routev1.RouteTargetReference{
				Name:   "TestService",
				Weight: new(int32),
			},
		},
	}

	// ignores all events for namespace that doesn't match
	for _, s := range []watch.EventType{watch.Added, watch.Modified, watch.Deleted} {
		plugin.HandleRoute(s, route)
		if _, ok := router.FindServiceUnit(endpointsKeyFromParts("foo", "TestService")); ok || plugin.HostLen() != 0 {
			t.Errorf("unexpected router state %#v", router)
		}
	}

	// allow non matching
	plugin.HandleNamespaces(sets.NewString("bar"))
	for _, s := range []watch.EventType{watch.Added, watch.Modified, watch.Deleted} {
		plugin.HandleRoute(s, route)
		if _, ok := router.FindServiceUnit(endpointsKeyFromParts("foo", "TestService")); ok || plugin.HostLen() != 0 {
			t.Errorf("unexpected router state %#v", router)
		}
	}

	// allow foo
	plugin.HandleNamespaces(sets.NewString("foo", "bar"))
	plugin.HandleRoute(watch.Added, route)
	if _, ok := router.FindServiceUnit(endpointsKeyFromParts("foo", "TestService")); !ok || plugin.HostLen() != 1 {
		t.Errorf("unexpected router state %#v", router)
	}

	// forbid foo, and make sure it's cleared
	plugin.HandleNamespaces(sets.NewString("bar"))
	if _, ok := router.FindServiceUnit(endpointsKeyFromParts("foo", "TestService")); ok || plugin.HostLen() != 0 {
		t.Errorf("unexpected router state %#v", router)
	}
	plugin.HandleRoute(watch.Modified, route)
	if _, ok := router.FindServiceUnit(endpointsKeyFromParts("foo", "TestService")); ok || plugin.HostLen() != 0 {
		t.Errorf("unexpected router state %#v", router)
	}
	plugin.HandleRoute(watch.Added, route)
	if _, ok := router.FindServiceUnit(endpointsKeyFromParts("foo", "TestService")); ok || plugin.HostLen() != 0 {
		t.Errorf("unexpected router state %#v", router)
	}
}
