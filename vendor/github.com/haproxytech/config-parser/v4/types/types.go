/*
Copyright 2019 HAProxy Technologies

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

//nolint:godot,gocritic
package types

import "github.com/haproxytech/config-parser/v4/params"

//sections:frontend,backend
//name:acl
//is-multiple:true
//test:ok:acl url_stats path_beg /stats
//test:ok:acl url_static path_beg -i /static /images /javascript /stylesheets
//test:ok:acl url_static path_end -i .jpg .gif .png .css .js
//test:ok:acl be_app_ok nbsrv(be_app) gt 0
//test:ok:acl be_static_ok nbsrv(be_static) gt 0
//test:ok:acl key req.hdr(X-Add-ACL-Key) -m found
//test:ok:acl add path /addacl
//test:ok:acl del path /delacl
//test:ok:acl myhost hdr(Host) -f myhost.lst
//test:ok:acl clear dst_port 80
//test:ok:acl secure dst_port 8080
//test:ok:acl login_page url_beg /login
//test:ok:acl logout url_beg /logout
//test:ok:acl uid_given url_reg /login?userid=[^&]+
//test:ok:acl cookie_set hdr_sub(cookie) SEEN=1
//test:fail:acl cookie
//test:fail:acl
type ACL struct {
	Name      string
	Criterion string
	Value     string
	Comment   string
}

//sections:frontend
//name:bind
//is-multiple:true
//test:fail:bind
//test:ok:bind :80,:443
//test:ok:bind 10.0.0.1:10080,10.0.0.1:10443
//test:ok:bind /var/run/ssl-frontend.sock user root mode 600 accept-proxy
//test:ok:bind :80
//test:ok:bind :443 ssl crt /etc/haproxy/site.pem
//test:ok:bind :443 ssl crt /etc/haproxy/site.pem alpn h2,http/1.1
//test:ok:bind :::443 v4v6 ssl crt /etc/haproxy/site.pem alpn h2,http/1.1
//test:ok:bind ipv6@:80
//test:ok:bind ipv4@public_ssl:443 ssl crt /etc/haproxy/site.pem
//test:ok:bind unix@ssl-frontend.sock user root mode 600 accept-proxy
//test:ok:bind :443 accept-netscaler-cip 1234
//test:ok:bind :443 accept-proxy
//test:ok:bind :443 allow-0rtt
//test:ok:bind :443 alpn h2
//test:ok:bind :443 alpn http/1.1
//test:ok:bind :443 alpn h2,http/1.1
//test:ok:bind :443 backlog test
//test:ok:bind :443 curves ECDH_ECDSA,ECDHE_ECDSA,ECDH_RSA,ECDHE_RSA,ECDH_anon
//test:ok:bind :443 ecdhe ECDH_ECDSA,ECDHE_ECDSA,ECDH_RSA,ECDHE_RSA,ECDH_anon
//test:ok:bind :443 ca-file file.pem
//test:ok:bind :443 ca-ignore-err all
//test:ok:bind :443 ca-ignore-err 1234
//test:ok:bind :443 ca-sign-file file.test
//test:ok:bind :443 ca-sign-pass passphrase
//test:ok:bind :443 ca-verify-file file.test
//test:ok:bind :443 ciphers ECDHE+aRSA+AES256+GCM+SHA384:ECDHE+aRSA+AES128+GCM+SHA256:ECDHE+aRSA+AES256+SHA384:ECDHE+aRSA+AES128+SHA256:ECDHE+aRSA+RC4+SHA:ECDHE+aRSA+AES256+SHA:ECDHE+aRSA+AES128+SHA:AES256+GCM+SHA384:AES128+GCM+SHA256:AES128+SHA256:AES256+SHA256:DHE+aRSA+AES128+SHA:RC4+SHA:HIGH:!aNULL:!eNULL:!LOW:!3DES:!MD5:!EXP:!PSK:!SRP:!DSS
//test:ok:bind :443 ciphersuites TODO
//test:ok:bind :443 crl-file file.test
//test:ok:bind :443 crt example.pem
//test:ok:bind :443 crt-ignore-err all
//test:ok:bind :443 crt-ignore-err 404,410
//test:ok:bind :443 crt-list cert1.pem
//test:ok:bind :443 defer-accept
//test:ok:bind :443 expose-fd listeners
//test:ok:bind :443 force-sslv3
//test:ok:bind :443 force-tlsv10
//test:ok:bind :443 force-tlsv11
//test:ok:bind :443 force-tlsv12
//test:ok:bind :443 force-tlsv13
//test:ok:bind :443 generate-certificates
//test:ok:bind :443 gid users
//test:ok:bind :443 group group
//test:ok:bind :443 id 1
//test:ok:bind :443 interface eth0
//test:ok:bind :443 interface eth1
//test:ok:bind :443 interface pppoe-wan
//test:ok:bind :443 level user
//test:ok:bind :443 level opeerator
//test:ok:bind :443 level admin
//test:ok:bind :443 severity-output none
//test:ok:bind :443 severity-output number
//test:ok:bind :443 severity-output string
//test:ok:bind :443 maxconn 1024
//test:ok:bind :443 mode TODO
//test:ok:bind :443 mss 1460
//test:ok:bind :443 mss -1460
//test:ok:bind :443 name sockets
//test:ok:bind :443 namespace example
//test:ok:bind :443 nice 0
//test:ok:bind :443 nice 1024
//test:ok:bind :443 nice -1024
//test:ok:bind :443 no-ca-names
//test:ok:bind :443 no-sslv3
//test:ok:bind :443 no-tlsv10
//test:ok:bind :443 no-tlsv11
//test:ok:bind :443 no-tlsv12
//test:ok:bind :443 no-tlsv13
//test:ok:bind :443 npn http/1.0
//test:ok:bind :443 npn http/1.1
//test:ok:bind :443 npn http/1.0,http/1.1
//test:ok:bind :443 prefer-client-ciphers
//test:ok:bind :443 process all
//test:ok:bind :443 process odd
//test:ok:bind :443 process even
//test:ok:bind :443 process 1-4
//test:ok:bind :443 proto h2
//test:ok:bind :443 ssl
//test:ok:bind :443 ssl-max-ver SSLv3
//test:ok:bind :443 ssl-max-ver TLSv1.0
//test:ok:bind :443 ssl-max-ver TLSv1.1
//test:ok:bind :443 ssl-max-ver TLSv1.2
//test:ok:bind :443 ssl-max-ver TLSv1.3
//test:ok:bind :443 ssl-min-ver SSLv3
//test:ok:bind :443 ssl-min-ver TLSv1.0
//test:ok:bind :443 ssl-min-ver TLSv1.1
//test:ok:bind :443 ssl-min-ver TLSv1.2
//test:ok:bind :443 ssl-min-ver TLSv1.3
//test:ok:bind :443 strict-sni
//test:ok:bind :443 tcp-ut 30s
//test:ok:bind :443 tfo
//test:ok:bind :443 tls-ticket-keys /tmp/tls_ticket_keys
//test:ok:bind :443 transparent
//test:ok:bind :443 v4v6
//test:ok:bind :443 v6only
//test:ok:bind :443 uid 65534
//test:ok:bind :443 user web1
//test:ok:bind :443 verify none
//test:ok:bind :443 verify optional
//test:ok:bind :443 verify required
type Bind struct {
	Path    string // can be address:port or socket path
	Params  []params.BindOption
	Comment string
}

//sections:frontend
//name:bind-process
//is-multiple:false
//test:ok:bind-process all
//test:ok:bind-process odd
//test:ok:bind-process even
//test:ok:bind-process 1 2 3 4
//test:ok:bind-process 1-4
//test:fail:bind-process none
//test:fail:bind-process 1+4
//test:fail:bind-process none-none
//test:fail:bind-process 1-4 1-3
type BindProcess struct {
	Process string
	Comment string
}

//sections:defaults,backend
//name:balance
//is-multiple:false
//test:ok:balance roundrobin
//test:ok:balance uri
//test:ok:balance uri whole
//test:ok:balance uri len 12
//test:ok:balance uri depth 8
//test:ok:balance uri depth 8 whole
//test:ok:balance uri depth 8 len 12 whole
//test:ok:balance url_param
//test:ok:balance url_param session_id
//test:ok:balance url_param check_post 10
//test:ok:balance url_param check_post 10 max_wait 20
//test:ok:balance url_param session_id check_post 10 max_wait 20
//test:ok:balance hdr(hdrName)
//test:ok:balance hdr(hdrName) use_domain_only
//test:ok:balance random
//test:ok:balance random(15)
//test:ok:balance rdp-cookie
//test:ok:balance rdp-cookie(something)
//test:fails:balance something
//test:fail:balance
//test:fail:balance uri len notInteger
//test:fail:balance uri depth notInteger
//test:fail:balance url_param check_post notInteger
type Balance struct {
	Algorithm string
	Params    params.BalanceParams
	Comment   string
}

//sections:defaults,backend
//name:cookie
//is-multiple:false
//test:ok:cookie test
//test:ok:cookie myCookie domain dom1 indirect postonly
//test:ok:cookie myCookie domain dom1 domain dom2 indirect postonly
//test:ok:cookie myCookie indirect maxidle 10 maxlife 5 postonly
//test:ok:cookie myCookie indirect maxidle 10
//test:ok:cookie myCookie indirect maxlife 10
//test:ok:cookie myCookie domain dom1 domain dom2 httponly indirect maxidle 10 maxlife 5 nocache postonly preserve rewrite secure
//test:ok:cookie myCookie attr \"SameSite=Strict\" attr \"mykey=myvalue\" insert
//test:fail:cookie
//test:fail:cookie myCookie maxidle something
//test:fail:cookie myCookie maxlife something
//test:fail:cookie myCookie attr \"SameSite=Lax;\"
type Cookie struct {
	Domain   []string
	Attr     []string
	Name     string
	Type     string
	Comment  string
	Maxidle  int64
	Maxlife  int64
	Dynamic  bool
	Httponly bool
	Indirect bool
	Nocache  bool
	Postonly bool
	Preserve bool
	Secure   bool
}

//sections:global
//name:cpu-map
//is-multiple:true
//test:ok:cpu-map 1-4 0-3
//test:ok:cpu-map 1/all 0-3
//test:ok:cpu-map auto:1-4 0-3
//test:ok:cpu-map auto:1-4 0-1 2-3
//test:fail:cpu-map
type CPUMap struct {
	Process string
	CPUSet  string
	Comment string
}

//sections:defaults,backend
//name:default-server
//is-multiple:true
//test:ok:default-server addr 127.0.0.1
//test:ok:default-server addr ::1
//test:ok:default-server agent-check
//test:ok:default-server agent-send name
//test:ok:default-server agent-inter 1000ms
//test:ok:default-server agent-addr 127.0.0.1
//test:ok:default-server agent-addr site.com
//test:ok:default-server agent-port 1
//test:ok:default-server agent-port 65535
//test:ok:default-server allow-0rtt
//test:ok:default-server alpn h2
//test:ok:default-server alpn http/1.1
//test:ok:default-server alpn h2,http/1.1
//test:ok:default-server backup
//test:ok:default-server ca-file cert.crt
//test:ok:default-server check
//test:ok:default-server check-send-proxy
//test:ok:default-server check-alpn http/1.0
//test:ok:default-server check-alpn http/1.1,http/1.0
//test:ok:default-server check-proto h2
//test:ok:default-server check-ssl
//test:ok:default-server check-via-socks4
//test:ok:default-server ciphers ECDHE-RSA-AES128-GCM-SHA256
//test:ok:default-server ciphers ECDHE-ECDSA-CHACHA20-POLY1305:ECDHE-RSA-CHACHA20-POLY1305:ECDHE-ECDSA-AES128-GCM-SHA256:ECDHE-RSA-AES128-GCM-SHA256:ECDHE-ECDSA-AES256-GCM-SHA384:ECDHE-RSA-AES256-GCM-SHA384:DHE-RSA-AES128-GCM-SHA256:DHE-RSA-AES256-GCM-SHA384:ECDHE-ECDSA-AES128-SHA256:ECDHE-RSA-AES128-SHA256:ECDHE-ECDSA-AES128-SHA:ECDHE-RSA-AES256-SHA384:ECDHE-RSA-AES128-SHA:ECDHE-ECDSA-AES256-SHA384:ECDHE-ECDSA-AES256-SHA:ECDHE-RSA-AES256-SHA:DHE-RSA-AES128-SHA256:DHE-RSA-AES128-SHA:DHE-RSA-AES256-SHA256:DHE-RSA-AES256-SHA:ECDHE-ECDSA-DES-CBC3-SHA:ECDHE-RSA-DES-CBC3-SHA:EDH-RSA-DES-CBC3-SHA:AES128-GCM-SHA256:AES256-GCM-SHA384:AES128-SHA256:AES256-SHA256:AES128-SHA:AES256-SHA:DES-CBC3-SHA:!DSS
//test:ok:default-server ciphersuites ECDHE-ECDSA-CHACHA20-POLY1305:ECDHE-RSA-CHACHA20-POLY1305:ECDHE-ECDSA-AES128-GCM-SHA256:ECDHE-RSA-AES128-GCM-SHA256:ECDHE-ECDSA-AES256-GCM-SHA384:ECDHE-RSA-AES256-GCM-SHA384:DHE-RSA-AES128-GCM-SHA256:DHE-RSA-AES256-GCM-SHA384:ECDHE-ECDSA-AES128-SHA256:ECDHE-RSA-AES128-SHA256:ECDHE-ECDSA-AES128-SHA:ECDHE-RSA-AES256-SHA384:ECDHE-RSA-AES128-SHA:ECDHE-ECDSA-AES256-SHA384:ECDHE-ECDSA-AES256-SHA:ECDHE-RSA-AES256-SHA:DHE-RSA-AES128-SHA256:DHE-RSA-AES128-SHA:DHE-RSA-AES256-SHA256:DHE-RSA-AES256-SHA:ECDHE-ECDSA-DES-CBC3-SHA:ECDHE-RSA-DES-CBC3-SHA:EDH-RSA-DES-CBC3-SHA:AES128-GCM-SHA256:AES256-GCM-SHA384:AES128-SHA256:AES256-SHA256:AES128-SHA:AES256-SHA:DES-CBC3-SHA:!DSS
//test:ok:default-server cookie value
//test:ok:default-server crl-file file.pem
//test:ok:default-server crt cert.pem
//test:ok:default-server disabled
//test:ok:default-server enabled
//test:ok:default-server error-limit 50
//test:ok:default-server fall 30
//test:ok:default-server fall 1 rise 2 inter 3s port 4444
//test:ok:default-server force-sslv3
//test:ok:default-server force-tlsv10
//test:ok:default-server force-tlsv11
//test:ok:default-server force-tlsv12
//test:ok:default-server force-tlsv13
//test:ok:default-server init-addr last,libc,none
//test:ok:default-server init-addr last,libc,none,127.0.0.1
//test:ok:default-server inter 1500ms
//test:ok:default-server inter 1000 weight 13
//test:ok:default-server fastinter 2500ms
//test:ok:default-server fastinter unknown
//test:ok:default-server downinter 3500ms
//test:ok:default-server log-proto legacy
//test:ok:default-server log-proto octet-count
//test:ok:default-server maxconn 1
//test:ok:default-server maxconn 50
//test:ok:default-server maxqueue 0
//test:ok:default-server maxqueue 1000
//test:ok:default-server max-reuse -1
//test:ok:default-server max-reuse 0
//test:ok:default-server max-reuse 1
//test:ok:default-server minconn 1
//test:ok:default-server minconn 50
//test:ok:default-server namespace test
//test:ok:default-server no-agent-check
//test:ok:default-server no-backup
//test:ok:default-server no-check
//test:ok:default-server no-check-ssl
//test:ok:default-server no-send-proxy-v2
//test:ok:default-server no-send-proxy-v2-ssl
//test:ok:default-server no-send-proxy-v2-ssl-cn
//test:ok:default-server no-ssl
//test:ok:default-server no-ssl-reuse
//test:ok:default-server no-sslv3
//test:ok:default-server no-tls-tickets
//test:ok:default-server no-tlsv10
//test:ok:default-server no-tlsv11
//test:ok:default-server no-tlsv12
//test:ok:default-server no-tlsv13
//test:ok:default-server no-verifyhost
//test:ok:default-server no-tfo
//test:ok:default-server non-stick
//test:ok:default-server npn http/1.1,http/1.0
//test:ok:default-server observe layer4
//test:ok:default-server observe layer7
//test:ok:default-server on-error fastinter
//test:ok:default-server on-error fail-check
//test:ok:default-server on-error sudden-death
//test:ok:default-server on-error mark-down
//test:ok:default-server on-marked-down shutdown-sessions
//test:ok:default-server on-marked-up shutdown-backup-session
//test:ok:default-server pool-max-conn -1
//test:ok:default-server pool-max-conn 0
//test:ok:default-server pool-max-conn 100
//test:ok:default-server pool-purge-delay 0
//test:ok:default-server pool-purge-delay 5
//test:ok:default-server pool-purge-delay 500
//test:ok:default-server port 27015
//test:ok:default-server port 27016
//test:ok:default-server proto h2
//test:ok:default-server redir http://image1.mydomain.com
//test:ok:default-server redir https://image1.mydomain.com
//test:ok:default-server rise 2
//test:ok:default-server rise 200
//test:ok:default-server resolve-opts allow-dup-ip
//test:ok:default-server resolve-opts ignore-weight
//test:ok:default-server resolve-opts allow-dup-ip,ignore-weight
//test:ok:default-server resolve-opts prevent-dup-ip,ignore-weight
//test:ok:default-server resolve-prefer ipv4
//test:ok:default-server resolve-prefer ipv6
//test:ok:default-server resolve-net 10.0.0.0/8
//test:ok:default-server resolve-net 10.0.0.0/8,10.0.0.0/16
//test:ok:default-server resolvers mydns
//test:ok:default-server send-proxy
//test:ok:default-server send-proxy-v2
//test:ok:default-server proxy-v2-options ssl
//test:ok:default-server proxy-v2-options ssl,cert-cn
//test:ok:default-server proxy-v2-options ssl,cert-cn,ssl-cipher,cert-sig,cert-key,authority,crc32c,unique-id
//test:ok:default-server send-proxy-v2-ssl
//test:ok:default-server send-proxy-v2-ssl-cn
//test:ok:default-server slowstart 2000ms
//test:ok:default-server sni TODO
//test:ok:default-server source TODO
//test:ok:default-server ssl
//test:ok:default-server ssl-max-ver SSLv3
//test:ok:default-server ssl-max-ver TLSv1.0
//test:ok:default-server ssl-max-ver TLSv1.1
//test:ok:default-server ssl-max-ver TLSv1.2
//test:ok:default-server ssl-max-ver TLSv1.3
//test:ok:default-server ssl-min-ver SSLv3
//test:ok:default-server ssl-min-ver TLSv1.0
//test:ok:default-server ssl-min-ver TLSv1.1
//test:ok:default-server ssl-min-ver TLSv1.2
//test:ok:default-server ssl-min-ver TLSv1.3
//test:ok:default-server ssl-reuse
//test:ok:default-server stick
//test:ok:default-server socks4 127.0.0.1:81
//test:ok:default-server tcp-ut 20ms
//test:ok:default-server tfo
//test:ok:default-server track TODO
//test:ok:default-server tls-tickets
//test:ok:default-server verify none
//test:ok:default-server verify required
//test:ok:default-server verifyhost site.com
//test:ok:default-server weight 1
//test:ok:default-server weight 128
//test:ok:default-server weight 256
//test:fail:default-server
type DefaultServer struct {
	Params  []params.ServerOption
	Comment string
}

//sections:defaults,frontend,backend
//name:errorfile
//no-init:true
//is-multiple:true
//test:ok:errorfile 400 /etc/haproxy/errorfiles/400badreq.http
//test:ok:errorfile 408 /dev/null # work around Chrome pre-connect bug
//test:ok:errorfile 403 /etc/haproxy/errorfiles/403forbid.http
//test:ok:errorfile 503 /etc/haproxy/errorfiles/503sorry.http
//test:fail:errorfile
type ErrorFile struct {
	Code    string
	File    string
	Comment string
}

//sections:userlist
//name:group
//is-multiple:true
//test:ok:group G1 users tiger,scott
//test:ok:group G1
//test:fail:group
type Group struct {
	Name    string
	Users   []string
	Comment string
}

//sections:defaults,backend
//name:hash-type
//test:ok:hash-type map-based
//test:ok:hash-type map-based avalanche
//test:ok:hash-type consistent
//test:ok:hash-type consistent avalanche
//test:ok:hash-type avalanche
//test:ok:hash-type map-based sdbm
//test:ok:hash-type map-based djb2
//test:ok:hash-type map-based wt6
//test:ok:hash-type map-based crc32
//test:ok:hash-type consistent sdbm
//test:ok:hash-type consistent djb2
//test:ok:hash-type consistent wt6
//test:ok:hash-type consistent crc32
//test:ok:hash-type map-based sdbm avalanche
//test:ok:hash-type map-based djb2 avalanche
//test:ok:hash-type map-based wt6 avalanche
//test:ok:hash-type map-based crc32 avalanche
//test:ok:hash-type consistent sdbm avalanche
//test:ok:hash-type consistent djb2 avalanche
//test:ok:hash-type consistent wt6 avalanche
//test:ok:hash-type consistent crc32 avalanche
//test:fail:hash-type
type HashType struct {
	Method   string
	Function string
	Modifier string
	Comment  string
}

//sections:defaults,backend
//name:http-reuse
//is-multiple:false
//test:ok:http-reuse never
//test:ok:http-reuse safe
//test:ok:http-reuse aggressive
//test:ok:http-reuse always
//test:fail:http-reuse sometimes
type HTTPReuse struct {
	ShareType string
	Comment   string
}

//deprecated:true
//sections:defaults,backend
//name:http-check
//is-multiple:true
//test:ok:http-check disable-on-404
//test:ok:http-check send-state
//test:ok:http-check expect status 200
//test:ok:http-check expect ! string SQL\\ Error
//test:ok:http-check expect ! rstatus ^5
//test:ok:http-check expect rstring <!--tag:[0-9a-f]*--></html>
//test:fail:http-check
type HTTPCheckV2 struct {
	Type            string
	ExclamationMark bool
	Match           string
	Pattern         string
	Comment         string
}

//sections:defaults,frontend,backend
//name:log
//is-multiple:true
//no-init:true
//no-parse:true
//test:ok:log global
//test:ok:log stdout format short daemon # send log to systemd
//test:ok:log stdout format raw daemon # send everything to stdout
//test:ok:log stderr format raw daemon notice # send important events to stderr
//test:ok:log 127.0.0.1:514 local0 notice # only send important events
//test:ok:log 127.0.0.1:514 local0 notice notice # same but limit output level
//test:ok:log 127.0.0.1:1515 len 8192 format rfc5424 local2 info
//test:fail:log
type Log struct {
	Global   bool
	NoLog    bool
	Address  string
	Length   int64
	Format   string
	Facility string
	Level    string
	MinLevel string
	Comment  string
}

//sections:mailers
//name:mailer
//is-multiple:true
//test:ok:mailer smtp1 192.168.0.1:587
//test:ok:mailer smtp1 192.168.0.1:587 # just some comment
//test:fail:mailer
type Mailer struct {
	Name    string
	IP      string
	Port    int64
	Comment string
}

//sections:frontend,backend
//name:option forwardfor
//no-parse:true
//test:ok:option forwardfor
//test:ok:option forwardfor except A
//test:ok:option forwardfor except A header B
//test:ok:option forwardfor except A header B if-none
//test:ok:option forwardfor # comment
//test:ok:option forwardfor except A # comment
//test:fail:option forwardfor except
//test:fail:option forwardfor except A header
//test:fail:option forwardfor header
type OptionForwardFor struct {
	Except  string
	Header  string
	IfNone  bool
	Comment string
}

//sections:defaults,backend
//name:option httpchk
//no-parse:true
//test:ok:option httpchk OPTIONS * HTTP/1.1\\r\\nHost:\\ www
//test:ok:option httpchk <uri>
//test:ok:option httpchk <method> <uri>
//test:ok:option httpchk <method> <uri> <version>
type OptionHttpchk struct {
	NoOption bool
	Method   string
	URI      string
	Version  string
	Comment  string
}

//sections:frontend
//name:option httplog
//no-parse:true
//test:ok:option httplog
//test:ok:no option httplog
//test:ok:option httplog clf
//test:ok:option httplog # comment
//test:ok:option httplog clf # comment
type OptionHTTPLog struct {
	NoOption bool
	Clf      bool
	Comment  string
}

//sections:backend
//name:option mysql-check
//no-parse:true
//test:ok:option mysql-check
//test:ok:option mysql-check user john
//test:ok:option mysql-check user john post-41
//test:ok:option mysql-check user john pre-41
//test:ok:option mysql-check # comment
//test:fail:option mysql-check user
//test:fail:option mysql-check user john 41
//test:fail:option mysql-check user # comment
type OptionMysqlCheck struct {
	NoOption      bool
	User          string
	ClientVersion string
	Comment       string
}

//sections:backend
//name:option pgsql-check
//no-parse:true
//test:ok:option pgsql-check user john
//test:ok:option pgsql-check user john # comment
//test:fail:option pgsql-check
//test:fail:option pgsql-check # comment
//test:fail:option pgsql-check user
//test:fail:option pgsql-check user # comment
type OptionPgsqlCheck struct {
	NoOption bool
	User     string
	Comment  string
}

//sections:backend
//name:option redispatch
//no-parse:true
//test:ok:option redispatch
//test:ok:no option redispatch
//test:ok:option redispatch 1
//test:ok:option redispatch # comment
//test:ok:option redispatch -1 # comment
type OptionRedispatch struct {
	NoOption bool
	Interval *int64
	Comment  string
}

//sections:backend
//name:option smtpchk
//no-parse:true
//test:ok:option smtpchk
//test:ok:no option smtpchk
//test:ok:option smtpchk HELO mydomain.org
//test:ok:option smtpchk EHLO mydomain.org
//test:ok:option smtpchk # comment
//test:ok:option smtpchk HELO mydomain.org # comment
type OptionSmtpchk struct {
	NoOption bool
	Hello    string
	Domain   string
	Comment  string
}

//sections:backend
//name:external-check path
//no-parse:true
//test:ok:external-check path /usr/bin:/bin
type ExternalCheckPath struct {
	Path    string
	Comment string
}

//sections:backend
//name:external-check command
//no-parse:true
//test:ok:external-check command /bin/true
type ExternalCheckCommand struct {
	Command string
	Comment string
}

//sections:peers
//name:peer
//is-multiple:true
//test:ok:peer name 127.0.0.1:8080
//test:fail:peer name 127.0.0.1
//test:fail:peer name :8080
//test:fail:peer
type Peer struct {
	Name    string
	IP      string
	Port    int64
	Comment string
}

//sections:backend
//name:server
//is-multiple:true
//test:ok:server name 127.0.0.1:8080
//test:ok:server name 127.0.0.1
//test:ok:server addr 127.0.0.1
//test:ok:server addr ::1
//test:fail:server addr
//test:ok:server name 127.0.0.1 agent-check
//test:ok:server name 127.0.0.1 agent-send name
//test:ok:server name 127.0.0.1 agent-inter 1000ms
//test:ok:server name 127.0.0.1 agent-addr 127.0.0.1
//test:ok:server name 127.0.0.1 agent-addr site.com
//test:ok:server name 127.0.0.1 agent-port 1
//test:ok:server name 127.0.0.1 agent-port 65535
//test:ok:server name 127.0.0.1 allow-0rtt
//test:ok:server name 127.0.0.1 alpn h2
//test:ok:server name 127.0.0.1 alpn http/1.1
//test:ok:server name 127.0.0.1 alpn h2,http/1.1
//test:ok:server name 127.0.0.1 backup
//test:ok:server name 127.0.0.1 ca-file cert.crt
//test:ok:server name 127.0.0.1 check
//test:ok:server name 127.0.0.1 check-send-proxy
//test:ok:server name 127.0.0.1 check-alpn http/1.0
//test:ok:server name 127.0.0.1 check-alpn http/1.1,http/1.0
//test:ok:server name 127.0.0.1 check-proto h2
//test:ok:server name 127.0.0.1 check-ssl
//test:ok:server name 127.0.0.1 check-via-socks4
//test:ok:server name 127.0.0.1 ciphers ECDHE-RSA-AES128-GCM-SHA256
//test:ok:server name 127.0.0.1 ciphers ECDHE-ECDSA-CHACHA20-POLY1305:ECDHE-RSA-CHACHA20-POLY1305:ECDHE-ECDSA-AES128-GCM-SHA256:ECDHE-RSA-AES128-GCM-SHA256:ECDHE-ECDSA-AES256-GCM-SHA384:ECDHE-RSA-AES256-GCM-SHA384:DHE-RSA-AES128-GCM-SHA256:DHE-RSA-AES256-GCM-SHA384:ECDHE-ECDSA-AES128-SHA256:ECDHE-RSA-AES128-SHA256:ECDHE-ECDSA-AES128-SHA:ECDHE-RSA-AES256-SHA384:ECDHE-RSA-AES128-SHA:ECDHE-ECDSA-AES256-SHA384:ECDHE-ECDSA-AES256-SHA:ECDHE-RSA-AES256-SHA:DHE-RSA-AES128-SHA256:DHE-RSA-AES128-SHA:DHE-RSA-AES256-SHA256:DHE-RSA-AES256-SHA:ECDHE-ECDSA-DES-CBC3-SHA:ECDHE-RSA-DES-CBC3-SHA:EDH-RSA-DES-CBC3-SHA:AES128-GCM-SHA256:AES256-GCM-SHA384:AES128-SHA256:AES256-SHA256:AES128-SHA:AES256-SHA:DES-CBC3-SHA:!DSS
//test:ok:server name 127.0.0.1 ciphersuites ECDHE-ECDSA-CHACHA20-POLY1305:ECDHE-RSA-CHACHA20-POLY1305:ECDHE-ECDSA-AES128-GCM-SHA256:ECDHE-RSA-AES128-GCM-SHA256:ECDHE-ECDSA-AES256-GCM-SHA384:ECDHE-RSA-AES256-GCM-SHA384:DHE-RSA-AES128-GCM-SHA256:DHE-RSA-AES256-GCM-SHA384:ECDHE-ECDSA-AES128-SHA256:ECDHE-RSA-AES128-SHA256:ECDHE-ECDSA-AES128-SHA:ECDHE-RSA-AES256-SHA384:ECDHE-RSA-AES128-SHA:ECDHE-ECDSA-AES256-SHA384:ECDHE-ECDSA-AES256-SHA:ECDHE-RSA-AES256-SHA:DHE-RSA-AES128-SHA256:DHE-RSA-AES128-SHA:DHE-RSA-AES256-SHA256:DHE-RSA-AES256-SHA:ECDHE-ECDSA-DES-CBC3-SHA:ECDHE-RSA-DES-CBC3-SHA:EDH-RSA-DES-CBC3-SHA:AES128-GCM-SHA256:AES256-GCM-SHA384:AES128-SHA256:AES256-SHA256:AES128-SHA:AES256-SHA:DES-CBC3-SHA:!DSS
//test:ok:server name 127.0.0.1 cookie value
//test:ok:server name 127.0.0.1 crl-file file.pem
//test:ok:server name 127.0.0.1 crt cert.pem
//test:ok:server name 127.0.0.1 disabled
//test:ok:server name 127.0.0.1 enabled
//test:ok:server name 127.0.0.1 error-limit 50
//test:ok:server name 127.0.0.1 fall 30
//test:ok:server name 127.0.0.1 force-sslv3
//test:ok:server name 127.0.0.1 force-tlsv10
//test:ok:server name 127.0.0.1 force-tlsv11
//test:ok:server name 127.0.0.1 force-tlsv12
//test:ok:server name 127.0.0.1 force-tlsv13
//test:ok:server name 127.0.0.1 init-addr last,libc,none
//test:ok:server name 127.0.0.1 init-addr last,libc,none,127.0.0.1
//test:ok:server name 127.0.0.1 inter 1500ms
//test:ok:server name 127.0.0.1 fastinter 2500ms
//test:ok:server name 127.0.0.1 fastinter unknown
//test:ok:server name 127.0.0.1 downinter 3500ms
//test:ok:server name 127.0.0.1 log-proto legacy
//test:ok:server name 127.0.0.1 log-proto octet-count
//test:ok:server name 127.0.0.1 maxconn 1
//test:ok:server name 127.0.0.1 maxconn 50
//test:ok:server name 127.0.0.1 maxqueue 0
//test:ok:server name 127.0.0.1 maxqueue 1000
//test:ok:server name 127.0.0.1 max-reuse -1
//test:ok:server name 127.0.0.1 max-reuse 0
//test:ok:server name 127.0.0.1 max-reuse 1
//test:ok:server name 127.0.0.1 minconn 1
//test:ok:server name 127.0.0.1 minconn 50
//test:ok:server name 127.0.0.1 namespace test
//test:ok:server name 127.0.0.1 no-agent-check
//test:ok:server name 127.0.0.1 no-backup
//test:ok:server name 127.0.0.1 no-check
//test:ok:server name 127.0.0.1 no-check-ssl
//test:ok:server name 127.0.0.1 no-send-proxy-v2
//test:ok:server name 127.0.0.1 no-send-proxy-v2-ssl
//test:ok:server name 127.0.0.1 no-send-proxy-v2-ssl-cn
//test:ok:server name 127.0.0.1 no-ssl
//test:ok:server name 127.0.0.1 no-ssl-reuse
//test:ok:server name 127.0.0.1 no-sslv3
//test:ok:server name 127.0.0.1 no-tls-tickets
//test:ok:server name 127.0.0.1 no-tlsv10
//test:ok:server name 127.0.0.1 no-tlsv11
//test:ok:server name 127.0.0.1 no-tlsv12
//test:ok:server name 127.0.0.1 no-tlsv13
//test:ok:server name 127.0.0.1 no-verifyhost
//test:ok:server name 127.0.0.1 no-tfo
//test:ok:server name 127.0.0.1 non-stick
//test:ok:server name 127.0.0.1 npn http/1.1,http/1.0
//test:ok:server name 127.0.0.1 observe layer4
//test:ok:server name 127.0.0.1 observe layer7
//test:ok:server name 127.0.0.1 on-error fastinter
//test:ok:server name 127.0.0.1 on-error fail-check
//test:ok:server name 127.0.0.1 on-error sudden-death
//test:ok:server name 127.0.0.1 on-error mark-down
//test:ok:server name 127.0.0.1 on-marked-down shutdown-sessions
//test:ok:server name 127.0.0.1 on-marked-up shutdown-backup-session
//test:ok:server name 127.0.0.1 pool-max-conn -1
//test:ok:server name 127.0.0.1 pool-max-conn 0
//test:ok:server name 127.0.0.1 pool-max-conn 100
//test:ok:server name 127.0.0.1 pool-purge-delay 0
//test:ok:server name 127.0.0.1 pool-purge-delay 5
//test:ok:server name 127.0.0.1 pool-purge-delay 500
//test:ok:server name 127.0.0.1 port 27015
//test:ok:server name 127.0.0.1 port 27016
//test:ok:server name 127.0.0.1 proto h2
//test:ok:server name 127.0.0.1 redir http://image1.mydomain.com
//test:ok:server name 127.0.0.1 redir https://image1.mydomain.com
//test:ok:server name 127.0.0.1 rise 2
//test:ok:server name 127.0.0.1 rise 200
//test:ok:server name 127.0.0.1 resolve-opts allow-dup-ip
//test:ok:server name 127.0.0.1 resolve-opts ignore-weight
//test:ok:server name 127.0.0.1 resolve-opts allow-dup-ip,ignore-weight
//test:ok:server name 127.0.0.1 resolve-opts prevent-dup-ip,ignore-weight
//test:ok:server name 127.0.0.1 resolve-prefer ipv4
//test:ok:server name 127.0.0.1 resolve-prefer ipv6
//test:ok:server name 127.0.0.1 resolve-net 10.0.0.0/8
//test:ok:server name 127.0.0.1 resolve-net 10.0.0.0/8,10.0.0.0/16
//test:ok:server name 127.0.0.1 resolvers mydns
//test:ok:server name 127.0.0.1 send-proxy
//test:ok:server name 127.0.0.1 send-proxy-v2
//test:ok:server name 127.0.0.1 proxy-v2-options ssl
//test:ok:server name 127.0.0.1 proxy-v2-options ssl,cert-cn
//test:ok:server name 127.0.0.1 proxy-v2-options ssl,cert-cn,ssl-cipher,cert-sig,cert-key,authority,crc32c,unique-id
//test:ok:server name 127.0.0.1 send-proxy-v2-ssl
//test:ok:server name 127.0.0.1 send-proxy-v2-ssl-cn
//test:ok:server name 127.0.0.1 slowstart 2000ms
//test:ok:server name 127.0.0.1 sni TODO
//test:ok:server name 127.0.0.1 source TODO
//test:ok:server name 127.0.0.1 ssl
//test:ok:server name 127.0.0.1 ssl-max-ver SSLv3
//test:ok:server name 127.0.0.1 ssl-max-ver TLSv1.0
//test:ok:server name 127.0.0.1 ssl-max-ver TLSv1.1
//test:ok:server name 127.0.0.1 ssl-max-ver TLSv1.2
//test:ok:server name 127.0.0.1 ssl-max-ver TLSv1.3
//test:ok:server name 127.0.0.1 ssl-min-ver SSLv3
//test:ok:server name 127.0.0.1 ssl-min-ver TLSv1.0
//test:ok:server name 127.0.0.1 ssl-min-ver TLSv1.1
//test:ok:server name 127.0.0.1 ssl-min-ver TLSv1.2
//test:ok:server name 127.0.0.1 ssl-min-ver TLSv1.3
//test:ok:server name 127.0.0.1 ssl-reuse
//test:ok:server name 127.0.0.1 stick
//test:ok:server name 127.0.0.1 socks4 127.0.0.1:81
//test:ok:server name 127.0.0.1 tcp-ut 20ms
//test:ok:server name 127.0.0.1 tfo
//test:ok:server name 127.0.0.1 track TODO
//test:ok:server name 127.0.0.1 tls-tickets
//test:ok:server name 127.0.0.1 verify none
//test:ok:server name 127.0.0.1 verify required
//test:ok:server name 127.0.0.1 verifyhost site.com
//test:ok:server name 127.0.0.1 weight 1
//test:ok:server name 127.0.0.1 weight 128
//test:ok:server name 127.0.0.1 weight 256
//test:fail:server
type Server struct {
	Name    string
	Address string
	Params  []params.ServerOption
	Comment string
}

//sections:frontend,backend
//name:stick-table
//test:ok:stick-table type ip size 1m expire 5m store gpc0,conn_rate(30s)
//test:ok:stick-table type ip size 1m expire 5m store gpc0,conn_rate(30s) # comment
//test:ok:stick-table type string len 1000 size 1m expire 5m store gpc0,conn_rate(30s)
//test:ok:stick-table type string len 1000 size 1m expire 5m nopurge peers aaaaa store gpc0,conn_rate(30s)
//test:fail:stick-table type string len 1000 size 1m expire 5m something peers aaaaa store gpc0,conn_rate(30s)
//test:fail:stick-table type
//test:fail:stick-table
type StickTable struct {
	Type   string
	Length string
	Size   string

	Expire  string
	NoPurge bool
	Peers   string
	Store   string
	Comment string
}

//sections:global
//name:stats socket
//is-multiple:true
//test:ok:stats socket 127.0.0.1:8080
//test:ok:stats socket 127.0.0.1:8080 mode admin
//test:ok:stats socket /some/path/to/socket
//test:ok:stats socket /some/path/to/socket mode admin
//atest:fail:stats socket /some/path/to/socket mode
//test:fail:stats socket
type Socket struct {
	Path    string // can be address:port
	Params  []params.BindOption
	Comment string
}

//sections:backend
//name:stick
//is-multiple:true
//no-parse:true
//test:ok:stick on src table pop if !localhost
//test:ok:stick match src table pop if !localhost
//test:ok:stick store-request src table pop if !localhost
//test:fail:stick
type Stick struct {
	Type     string
	Pattern  string
	Table    string
	Cond     string
	CondTest string
	Comment  string
}

//sections:resolvers
//name:nameserver
//is-multiple:true
//test:ok:nameserver dns1 10.0.0.1:53
//test:ok:nameserver dns1 10.0.0.1:53 # comment
//test:fail:nameserver
type Nameserver struct {
	Name    string
	Address string
	Comment string
}

//sections:frontend
//name:use_backend
//is-multiple:true
//test:ok:use_backend test if TRUE
//test:ok:use_backend test if TRUE # deny
//test:ok:use_backend test # deny
//test:fail:use_backend
type UseBackend struct {
	Name     string
	Cond     string
	CondTest string
	Comment  string
}

//sections:userlist
//name:user
//is-multiple:true
//test:ok:user tiger password $6$k6y3o.eP$JlKBx(...)xHSwRv6J.C0/D7cV91 groups G1
//test:ok:user panda insecure-password elgato groups G1,G2
//test:ok:user bear insecure-password hello groups G2
//test:fail:user
type User struct {
	Name       string
	Password   string
	IsInsecure bool
	Groups     []string
	Comment    string
}

//sections:defaults,frontend
//name:unique-id-format
//test:ok:unique-id-format %{+X}o_%ci:%cp_%fi:%fp_%Ts_%rt:%pid
//test:ok:unique-id-format %{+X}o_%cp_%fi:%fp_%Ts_%rt:%pid
//test:ok:unique-id-format %{+X}o_%fi:%fp_%Ts_%rt:%pid
//test:fail:unique-id-format
type UniqueIDFormat struct {
	LogFormat string
	Comment   string
}

//sections:defaults,frontend
//name:unique-id-header
//test:ok:unique-id-header X-Unique-ID
//test:fail:unique-id-header
type UniqueIDHeader struct {
	Name    string
	Comment string
}

//sections:backend
//name:use-server
//is-multiple:true
//no-parse:true
//test:ok:use-server www if { req_ssl_sni -i www.example.com }
//test:ok:use-server www if { req_ssl_sni -i www.example.com } # comment
//test:fail:use-server
type UseServer struct {
	Name     string
	Cond     string
	CondTest string
	Comment  string
}

//sections:global
//name:lua-prepend-path
//is-multiple:true
//test:ok:lua-prepend-path /usr/share/haproxy-lua/?/init.lua
//test:ok:lua-prepend-path /usr/share/haproxy-lua/?/init.lua cpath
//test:fail:lua-prepend-path
type LuaPrependPath struct {
	Path    string
	Type    string
	Comment string
}

//sections:global
//name:lua-load
//is-multiple:true
//test:ok:lua-load /etc/haproxy/lua/foo.lua
//test:fail:lua-load
type LuaLoad struct {
	File    string
	Comment string
}

//sections:global
//name:ssl-engine
//test:ok:ssl-engine rdrand
//test:ok:ssl-engine rdrand ALL
//test:ok:ssl-engine rdrand RSA,DSA
//test:fail:ssl-engine
type SslEngine struct {
	Name       string
	Algorithms []string
}

//sections:global
//name:ssl-mode-async
//test:ok:ssl-mode-async
//test:fail:ssl-mode-async true
//test:fail:ssl-mode-async false
type SslModeAsync struct{}

//sections:global,listen,backend
//name:load-server-state-from-file
//test:ok:load-server-state-from-file global
//test:ok:load-server-state-from-file local
//test:ok:load-server-state-from-file none
//test:fail:load-server-state-from-file
//test:fail:load-server-state-from-file foo
//test:fail:load-server-state-from-file bar
type LoadServerStateFromFile struct {
	Argument string
}

//sections:defaults,frontend
//name:monitor-uri
//test:ok:monitor-uri /haproxy_test
//test:fail:monitor-uri
type MonitorURI struct {
	URI string
}

//sections:frontend
//name:monitor fail
//test:ok:monitor fail if no_db01 no_db02
//test:ok:monitor fail if ready_01 ready_02 ready_03
//test:ok:monitor fail unless backend_ready
//test:ok:monitor fail unless ready_01 ready_02 ready_03
//test:fail:monitor fail
//test:fail:monitor fail if
//test:fail:monitor unless
type MonitorFail struct {
	Condition string
	ACLList   []string
}

//sections:backend
//name:server-template
//is-multiple:true
//test:ok:server-template srv 1-3 google.com:80 check
//test:ok:server-template srv 3 google.com:80 check
//test:ok:server-template srv 3 google.com:80
//test:ok:server-template srv 3 google.com
//test:fail:server-template
//test:fail:server-template srv
//test:fail:server-template srv 3
//test:fail:server-template srv 1-3
type ServerTemplate struct {
	Prefix     string
	NumOrRange string
	Fqdn       string
	Params     []params.ServerOption
	Comment    string
}
