#!/bin/bash
# Generate a placeholder CRL, like the one included in pkg/router/crl/crl.go as dummyCRL. In order to create an already
# expired CRL, this script requires the utility faketime, which is provided by libfaketime on fedora.
set -e
cwd=$(dirname $0)
tmpdir=$(mktemp -d)
sed -e "s@%%tmpdir%%@${tmpdir}@" ${cwd}/placeholder-ca.cnf.template > ${tmpdir}/placeholder-ca.cnf
openssl genrsa -out ${tmpdir}/placeholder-ca.key 2048
openssl req -new -key ${tmpdir}/placeholder-ca.key -out ${tmpdir}/placeholder-ca.csr -subj "/C=US/ST=NC/L=Raleigh/O=OS4/OU=Eng/CN=Placeholder CA"
openssl x509 -req -in ${tmpdir}/placeholder-ca.csr -out ${tmpdir}/placeholder-ca.crt -days 3650 -signkey ${tmpdir}/placeholder-ca.key -extfile ${tmpdir}/placeholder-ca.cnf
cat /dev/null > ${tmpdir}/placeholder-crl-index.txt
faketime 'Jan 1, 2000 12:00AM GMT' openssl ca -gencrl -crlhours 1 -out ${tmpdir}/placeholder-ca.crl -config ${tmpdir}/placeholder-ca.cnf

echo "new placeholder crl at ${tmpdir}/placeholder-ca.crl" >&2
cat ${tmpdir}/placeholder-ca.crl
