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

import "github.com/haproxytech/config-parser/v4/common"

//name:section
//no-sections:true
//dir:extra
//no-init:true
type Section struct {
	Name    string
	Comment string
}

//name:config-version
//no-sections:true
//dir:extra
//no-init:true
//no-get:true
type ConfigVersion struct {
	Value int64
}

//name:config-hash
//no-sections:true
//dir:extra
//no-init:true
//no-get:true
type ConfigHash struct {
	Value string
}

//name:comments
//no-sections:true
//dir:extra
//is-multiple:true
//no-init:true
//no-parse:true
type Comments struct {
	Value string
}

//name:unprocessed
//no-sections:true
//dir:extra
//is-multiple:true
//no-init:true
//no-parse:true
//test:skip
type UnProcessed struct {
	Value string
}

//name:simple-option
//no-sections:true
//struct-name:Option
//dir:simple
//no-init:true
type SimpleOption struct {
	NoOption bool
	Comment  string
}

//name:simple-timeout
//no-sections:true
//struct-name:Timeout
//dir:simple
//no-init:true
type SimpleTimeout struct {
	Value   string
	Comment string
}

//name:simple-word
//no-sections:true
//struct-name:Word
//dir:simple
//parser-type:StringC
type SimpleWord struct{}

//name:simple-number
//no-sections:true
//struct-name:Number
//dir:simple
//parser-type:Int64C
type SimpleNumber struct{}

//name:simple-string
//no-sections:true
//struct-name:String
//dir:simple
//parser-type:StringC
type SimpleString struct{}

//name:simple-string-slice
//no-sections:true
//struct-name:StringSlice
//dir:simple
//parser-type:StringSliceC
type SimpleStringSlice struct{}

//name:simple-string-kv
//no-sections:true
//struct-name:StringKeyValue
//dir:simple
//parser-type:StringKeyValueC
type SimpleStringKeyValue struct{}

//name:simple-time
//no-sections:true
//struct-name:Time
//dir:simple
//parser-type:StringC
type SimpleTime struct{}

//name:simple-time-two-words
//no-sections:true
//struct-name:TimeTwoWords
//dir:simple
//no-init:true
//parser-type:StringC
//test:skip
type TimeTwoWords struct{}

type Filter interface {
	Parse(parts []string, comment string) error
	Result() common.ReturnResultLine
}

//name:filter
//no-sections:true
//dir:filters
//is-multiple:true
//parser-type:Filter
//is-interface:true
//no-init:true
//no-parse:true
type Filters struct{}

type HTTPAction interface {
	Parse(parts []string, comment string) error
	String() string
	GetComment() string
}

//sections:frontend,backend
//name:http-request
//struct-name:Requests
//dir:http
//is-multiple:true
//parser-type:HTTPAction
//is-interface:true
//no-init:true
//no-parse:true
//test:fail:http-request
//test:fail:http-request capture req.cook_cnt(FirstVisit),bool strlen 10
//test:ok:http-request capture req.cook_cnt(FirstVisit),bool len 10
//test:ok:http-request deny deny_status 0 unless { src 127.0.0.1 }
//test:ok:http-request set-map(map.lst) %[src] %[req.hdr(X-Value)] if value
//test:ok:http-request set-map(map.lst) %[src] %[req.hdr(X-Value)]
//test:fail:http-request set-map(map.lst) %[src]
//test:ok:http-request add-acl(map.lst) [src]
//test:fail:http-request add-acl(map.lst)
//test:ok:http-request add-header X-value value
//test:"ok":http-request add-header Authorization Basic\ eC1oYXByb3h5LXJlY3J1aXRzOlBlb3BsZSB3aG8gZGVjb2RlIG1lc3NhZ2VzIG9mdGVuIGxvdmUgd29ya2luZyBhdCBIQVByb3h5LiBEbyBub3QgYmUgc2h5LCBjb250YWN0IHVz
//test:"ok":http-request add-header Authorisation "Basic eC1oYXByb3h5LXJlY3J1aXRzOlBlb3BsZSB3aG8gZGVjb2RlIG1lc3NhZ2VzIG9mdGVuIGxvdmUgd29ya2luZyBhdCBIQVByb3h5LiBEbyBub3QgYmUgc2h5LCBjb250YWN0IHVz"
//test:fail:http-request add-header X-value
//test:ok:http-request cache-use cache-name
//test:ok:http-request cache-use cache-name if FALSE
//test:fail:http-request cache-use
//test:fail:http-request cache-use if FALSE
//test:ok:http-request del-acl(map.lst) [src]
//test:fail:http-request del-acl(map.lst)
//test:ok:http-request allow
//test:ok:http-request auth
//test:ok:http-request del-header X-value
//test:fail:http-request del-header
//test:ok:http-request del-map(map.lst) %[src] if ! value
//test:ok:http-request del-map(map.lst) %[src]
//test:fail:http-request del-map(map.lst)
//test:ok:http-request deny
//test:ok:http-request disable-l7-retry
//test:ok:http-request disable-l7-retry if FALSE
//test:ok:http-request early-hint hint %[src]
//test:ok:http-request early-hint hint %[src] if FALSE
//test:ok:http-request early-hint if FALSE
//test:fail:http-request early-hint hint
//test:fail:http-request early-hint hint if FALSE
//test:ok:http-request lua.foo
//test:ok:http-request lua.foo if FALSE
//test:ok:http-request lua.foo param
//test:ok:http-request lua.foo param param2
//test:fail:http-request lua.
//test:fail:http-request lua. if FALSE
//test:fail:http-request lua. param
//test:ok:http-request redirect prefix https://mysite.com
//test:fail:http-request redirect prefix
//test:ok:http-request reject
//test:ok:http-request replace-header User-agent curl foo
//test:fail:http-request replace-header User-agent curl
//test:ok:http-request replace-path (.*) /foo
//test:fail:http-request replace-path (.*)
//test:ok:http-request replace-uri ^http://(.*) https://1
//test:ok:http-request replace-uri ^http://(.*) https://1 if FALSE
//test:fail:http-request replace-uri ^http://(.*)
//test:fail:http-request replace-uri
//test:fail:http-request replace-uri ^http://(.*) if FALSE
//test:ok:http-request replace-value X-Forwarded-For ^192.168.(.*)$ 172.16.1
//test:fail:http-request replace-value X-Forwarded-For ^192.168.(.*)$
//test:ok:http-request sc-inc-gpc0(1)
//test:ok:http-request sc-inc-gpc0(1) if FALSE
//test:fail:http-request sc-inc-gpc0
//test:ok:http-request sc-inc-gpc1(1)
//test:ok:http-request sc-inc-gpc1(1) if FALSE
//test:fail:http-request sc-inc-gpc1
//test:ok:http-request sc-set-gpt0(1) hdr(Host),lower
//test:ok:http-request sc-set-gpt0(1) 10
//test:ok:http-request sc-set-gpt0(1) hdr(Host),lower if FALSE
//test:fail:http-request sc-set-gpt0(1)
//test:fail:http-request sc-set-gpt0
//test:fail:http-request sc-set-gpt0(1) if FALSE
//test:ok:http-request send-spoe-group engine group
//test:fail:http-request send-spoe-group engine
//test:ok:http-request set-header X-value value
//test:fail:http-request set-header X-value
//test:ok:http-request set-log-level silent
//test:fail:http-request set-log-level
//test:ok:http-request set-mark 20
//test:ok:http-request set-mark 0x1Ab
//test:fail:http-request set-mark
//test:ok:http-request set-nice 0
//test:ok:http-request set-nice 0 if FALSE
//test:fail:http-request set-nice
//test:ok:http-request set-method POST
//test:ok:http-request set-method POST if FALSE
//test:fail:http-request set-method
//test:ok:http-request set-path /%[hdr(host)]%[path]
//test:fail:http-request set-path
//test:ok:http-request set-priority-class req.hdr(priority)
//test:ok:http-request set-priority-class req.hdr(priority) if FALSE
//test:fail:http-request set-priority-class
//test:ok:http-request set-priority-offset req.hdr(offset)
//test:ok:http-request set-priority-offset req.hdr(offset) if FALSE
//test:fail:http-request set-priority-offset
//test:ok:http-request set-query %[query,regsub(%3D,=,g)]
//test:fail:http-request set-query
//test:ok:http-request set-src hdr(src)
//test:ok:http-request set-src hdr(src) if FALSE
//test:fail:http-request set-src
//test:ok:http-request set-src-port hdr(port)
//test:ok:http-request set-src-port hdr(port) if FALSE
//test:fail:http-request set-src-port
//test:ok:http-request set-tos 0 if FALSE
//test:ok:http-request set-tos 0
//test:fail:http-request set-tos
//test:ok:http-request set-uri /%[hdr(host)]%[path]
//test:fail:http-request set-uri
//test:ok:http-request set-var(req.my_var) req.fhdr(user-agent),lower
//test:fail:http-request set-var(req.my_var)
//test:ok:http-request silent-drop
//test:ok:http-request silent-drop if FALSE
//test:ok:http-request strict-mode on
//test:ok:http-request strict-mode on if FALSE
//test:fail:http-request strict-mode
//test:fail:http-request strict-mode if FALSE
//test:ok:http-request tarpit
//test:ok:http-request track-sc0 src
//test:fail:http-request track-sc0
//test:ok:http-request track-sc1 src
//test:fail:http-request track-sc1
//test:ok:http-request track-sc2 src
//test:fail:http-request track-sc2
//test:ok:http-request unset-var(req.my_var)
//test:ok:http-request unset-var(req.my_var) if FALSE
//test:fail:http-request unset-var(req.)
//test:fail:http-request unset-var(req)
//test:ok:http-request wait-for-handshake
//test:ok:http-request wait-for-handshake if FALSE
//test:ok:http-request do-resolve(txn.myip,mydns) hdr(Host),lower
//test:ok:http-request do-resolve(txn.myip,mydns) hdr(Host),lower if { var(txn.myip) -m found }
//test:ok:http-request do-resolve(txn.myip,mydns) hdr(Host),lower unless { var(txn.myip) -m found }
//test:ok:http-request do-resolve(txn.myip,mydns,ipv4) hdr(Host),lower
//test:ok:http-request do-resolve(txn.myip,mydns,ipv6) hdr(Host),lower
//test:fail:http-request do-resolve(txn.myip)
//test:fail:http-request do-resolve(txn.myip,mydns)
//test:fail:http-request do-resolve(txn.myip,mydns,ipv4)
//test:ok:http-request set-dst var(txn.myip)
//test:ok:http-request set-dst var(txn.myip) if { var(txn.myip) -m found }
//test:ok:http-request set-dst var(txn.myip) unless { var(txn.myip) -m found }
//test:fail:http-request set-dst
//test:ok:http-request set-dst-port hdr(x-port)
//test:ok:http-request set-dst-port hdr(x-port) if { var(txn.myip) -m found }
//test:ok:http-request set-dst-port hdr(x-port) unless { var(txn.myip) -m found }
//test:ok:http-request set-dst-port int(4000)
//test:fail:http-request set-dst-port
//test:"ok":http-request return status 200 content-type "text/plain" string "My content" if { var(txn.myip) -m found }
//test:"ok":http-request return status 200 content-type "text/plain" string "My content" unless { var(txn.myip) -m found }
//test:"ok":http-request return content-type "text/plain" string "My content" if { var(txn.myip) -m found }
//test:"ok":http-request return content-type 'text/plain' string 'My content' if { var(txn.myip) -m found }
//test:"ok":http-request return content-type "text/plain" lf-string "Hello, you are: %[src]" if { var(txn.myip) -m found }
//test:"ok":http-request return content-type "text/plain" file /my/fancy/response/file if { var(txn.myip) -m found }
//test:"ok":http-request return content-type "text/plain" lf-file /my/fancy/lof/format/response/file if { var(txn.myip) -m found }
//test:"ok":http-request return content-type "text/plain" string "My content" hdr X-value value if { var(txn.myip) -m found }
//test:"ok":http-request return content-type "text/plain" string "My content" hdr X-value x-value hdr Y-value y-value if { var(txn.myip) -m found }
//test:ok:http-request return status 400 default-errorfiles if { var(txn.myip) -m found }
//test:ok:http-request return status 400 errorfile /my/fancy/errorfile if { var(txn.myip) -m found }
//test:ok:http-request return status 400 errorfiles myerror if { var(txn.myip) -m found }
//test:"ok":http-request return content-type "text/plain" lf-string "Hello, you are: %[src]"

type HTTPRequests struct{}

//name:http-response
//sections:frontend,backend
//struct-name:Responses
//dir:http
//is-multiple:true
//parser-type:HTTPAction
//is-interface:true
//no-init:true
//no-parse:true
//test:fail:http-response
//test:ok:http-response capture res.hdr(Server) id 0
//test:ok:http-response set-map(map.lst) %[src] %[res.hdr(X-Value)] if value
//test:ok:http-response set-map(map.lst) %[src] %[res.hdr(X-Value)]
//test:fail:http-response set-map(map.lst) %[src]
//test:ok:http-response add-acl(map.lst) [src]
//test:fail:http-response add-acl(map.lst)
//test:ok:http-response add-header X-value value
//test:fail:http-response add-header X-value
//test:ok:http-response del-acl(map.lst) [src]
//test:fail:http-response del-acl(map.lst)
//test:ok:http-response allow
//test:ok:http-response del-header X-value
//test:fail:http-response del-header
//test:ok:http-response del-map(map.lst) %[src] if ! value
//test:ok:http-response del-map(map.lst) %[src]
//test:fail:http-response del-map(map.lst)
//test:ok:http-response deny
//test:ok:http-response lua.foo
//test:ok:http-response lua.foo if FALSE
//test:ok:http-response lua.foo param
//test:ok:http-response lua.foo param param2
//test:fail:http-response lua.
//test:fail:http-response lua. if FALSE
//test:fail:http-response lua. param
//test:ok:http-response redirect prefix https://mysite.com
//test:fail:http-response redirect prefix
//test:ok:http-response replace-header User-agent curl foo
//test:fail:http-response replace-header User-agent curl
//test:ok:http-response replace-value X-Forwarded-For ^192.168.(.*)$ 172.16.1
//test:fail:http-response replace-value X-Forwarded-For ^192.168.(.*)$
//test:ok:http-response sc-inc-gpc0(1)
//test:ok:http-response sc-inc-gpc0(1) if FALSE
//test:fail:http-response sc-inc-gpc0
//test:ok:http-response sc-inc-gpc1(1)
//test:ok:http-response sc-inc-gpc1(1) if FALSE
//test:fail:http-response sc-inc-gpc1
//test:ok:http-response sc-set-gpt0(1) hdr(Host),lower
//test:ok:http-response sc-set-gpt0(1) 10
//test:ok:http-response sc-set-gpt0(1) hdr(Host),lower if FALSE
//test:fail:http-response sc-set-gpt0(1)
//test:fail:http-response sc-set-gpt0
//test:fail:http-response sc-set-gpt0(1) if FALSE
//test:ok:http-response send-spoe-group engine group
//test:fail:http-response send-spoe-group engine
//test:ok:http-response set-header X-value value
//test:fail:http-response set-header X-value
//test:ok:http-response set-log-level silent
//test:fail:http-response set-log-level
//test:ok:http-response set-mark 20
//test:ok:http-response set-mark 0x1Ab
//test:fail:http-response set-mark
//test:ok:http-response set-nice 0
//test:ok:http-response set-nice 0 if FALSE
//test:fail:http-response set-nice
//test:ok:http-response set-status 503
//test:fail:http-response set-status
//test:ok:http-response set-tos 0 if FALSE
//test:ok:http-response set-tos 0
//test:fail:http-response set-tos
//test:ok:http-response set-var(req.my_var) res.fhdr(user-agent),lower
//test:fail:http-response set-var(req.my_var)
//test:ok:http-response silent-drop
//test:ok:http-response silent-drop if FALSE
//test:ok:http-response unset-var(req.my_var)
//test:ok:http-response unset-var(req.my_var) if FALSE
//test:fail:http-response unset-var(req.)
//test:fail:http-response unset-var(req)
//test:ok:http-response track-sc0 src if FALSE
//test:ok:http-response track-sc0 src table tr if FALSE
//test:ok:http-response track-sc0 src
//test:fail:http-response track-sc0
//test:ok:http-response track-sc1 src if FALSE
//test:ok:http-response track-sc1 src table tr if FALSE
//test:ok:http-response track-sc1 src
//test:fail:http-response track-sc1
//test:ok:http-response track-sc2 src if FALSE
//test:ok:http-response track-sc2 src table tr if FALSE
//test:ok:http-response track-sc2 src
//test:fail:http-response track-sc2
//test:ok:http-response strict-mode on
//test:ok:http-response strict-mode on if FALSE
//test:fail:http-response strict-mode
//test:fail:http-response strict-mode if FALSE
type HTTPResponses struct{}

//sections:defaults,backend
//name:http-check
//struct-name:Checks
//dir:http
//is-multiple:true
//parser-type:HTTPAction
//is-interface:true
//no-init:true
//no-parse:true
//test:ok:http-check comment testcomment
//test:ok:http-check connect
//test:ok:http-check connect default
//test:ok:http-check connect port 8080
//test:ok:http-check connect addr 8.8.8.8
//test:ok:http-check connect send-proxy
//test:ok:http-check connect via-socks4
//test:ok:http-check connect ssl
//test:ok:http-check connect sni haproxy.1wt.eu
//test:ok:http-check connect alpn h2,http/1.1
//test:ok:http-check connect proto h2
//test:ok:http-check connect linger
//test:ok:http-check connect comment testcomment
//test:ok:http-check connect port 443 addr 8.8.8.8 send-proxy via-socks4 ssl sni haproxy.1wt.eu alpn h2,http/1.1 linger proto h2 comment testcomment
//test:ok:http-check disable-on-404
//test:ok:http-check expect status 200
//test:ok:http-check expect min-recv 50 status 200
//test:ok:http-check expect comment testcomment status 200
//test:ok:http-check expect ok-status L7OK status 200
//test:ok:http-check expect error-status L7RSP status 200
//test:ok:http-check expect tout-status L7TOUT status 200
//test:ok:http-check expect on-success \"my-log-format\" status 200
//test:ok:http-check expect on-error \"my-log-format\" status 200
//test:ok:http-check expect status-code \"500\" status 200
//test:ok:http-check expect ! string SQL\\ Error
//test:ok:http-check expect ! rstatus ^5
//test:ok:http-check expect rstring <!--tag:[0-9a-f]*--></html>
//test:ok:http-check send meth GET
//test:ok:http-check send uri /health
//test:ok:http-check send ver \"HTTP/1.1\"
//test:ok:http-check send comment testcomment
//test:ok:http-check send meth GET uri /health ver \"HTTP/1.1\" hdr Host example.com hdr Accept-Encoding gzip body '{\"key\":\"value\"}'
//test:ok:http-check send uri-lf my-log-format body-lf 'my-log-format'
//test:ok:http-check send-state
//test:fail:http-check
//test:fail:http-check comment
//test:fail:http-check expect
//test:fail:http-check expect status
//test:fail:http-check expect comment testcomment
type HTTPCheck struct{}

type TCPType interface {
	Parse(parts []string, comment string) error
	String() string
	GetComment() string
}

type TCPAction interface {
	Parse(command []string) error
	String() string
}

//name:tcp-request
//sections:frontend,backend
//struct-name:Requests
//dir:tcp
//is-multiple:true
//parser-type:TCPType
//is-interface:true
//no-init:true
//no-parse:true
//test:ok:tcp-request content accept
//test:ok:tcp-request content accept if !HTTP
//test:ok:tcp-request content reject
//test:ok:tcp-request content reject if !HTTP
//test:ok:tcp-request content capture req.payload(0,6) len 6
//test:ok:tcp-request content capture req.payload(0,6) len 6 if !HTTP
//test:ok:tcp-request content set-priority-class int(1)
//test:ok:tcp-request content set-priority-class int(1) if some_check
//test:ok:tcp-request content set-priority-offset int(10)
//test:ok:tcp-request content set-priority-offset int(10) if some_check
//test:ok:tcp-request content track-sc0 src
//test:ok:tcp-request content track-sc0 src if some_check
//test:ok:tcp-request content track-sc1 src
//test:ok:tcp-request content track-sc1 src if some_check
//test:ok:tcp-request content track-sc2 src
//test:ok:tcp-request content track-sc2 src if some_check
//test:ok:tcp-request content track-sc0 src table foo
//test:ok:tcp-request content track-sc0 src table foo if some_check
//test:ok:tcp-request content track-sc1 src table foo
//test:ok:tcp-request content track-sc1 src table foo if some_check
//test:ok:tcp-request content track-sc2 src table foo
//test:ok:tcp-request content track-sc2 src table foo if some_check
//test:ok:tcp-request content set-dst ipv4(10.0.0.1)
//test:ok:tcp-request content set-var(sess.src) src
//test:ok:tcp-request content set-var(sess.dn) ssl_c_s_dn
//test:ok:tcp-request content unset-var(sess.src)
//test:ok:tcp-request content unset-var(sess.dn)
//test:ok:tcp-request content silent-drop
//test:ok:tcp-request content silent-drop if !HTTP
//test:ok:tcp-request content send-spoe-group engine group
//test:ok:tcp-request content use-service lua.deny
//test:ok:tcp-request content use-service lua.deny if !HTTP
//test:ok:tcp-request content lua.foo
//test:ok:tcp-request content lua.foo param if !HTTP
//test:ok:tcp-request content lua.foo param param1
//test:ok:tcp-request connection accept
//test:ok:tcp-request connection accept if !HTTP
//test:ok:tcp-request connection reject
//test:ok:tcp-request connection reject if !HTTP
//test:ok:tcp-request connection expect-proxy layer4 if { src -f proxies.lst }
//test:ok:tcp-request connection expect-netscaler-cip layer4
//test:ok:tcp-request connection capture req.payload(0,6) len 6
//test:ok:tcp-request connection track-sc0 src
//test:ok:tcp-request connection track-sc0 src if some_check
//test:ok:tcp-request connection track-sc1 src
//test:ok:tcp-request connection track-sc1 src if some_check
//test:ok:tcp-request connection track-sc2 src
//test:ok:tcp-request connection track-sc2 src if some_check
//test:ok:tcp-request connection track-sc0 src table foo
//test:ok:tcp-request connection track-sc0 src table foo if some_check
//test:ok:tcp-request connection track-sc1 src table foo
//test:ok:tcp-request connection track-sc1 src table foo if some_check
//test:ok:tcp-request connection track-sc2 src table foo
//test:ok:tcp-request connection track-sc2 src table foo if some_check
//test:ok:tcp-request connection sc-inc-gpc0(2)
//test:ok:tcp-request connection sc-inc-gpc0(2) if is-error
//test:ok:tcp-request connection sc-inc-gpc1(2)
//test:ok:tcp-request connection sc-inc-gpc1(2) if is-error
//test:ok:tcp-request connection sc-set-gpt0(0) 1337
//test:ok:tcp-request connection sc-set-gpt0(0) 1337 if exceeds_limit
//test:ok:tcp-request connection set-src src,ipmask(24)
//test:ok:tcp-request connection set-src src,ipmask(24) if some_check
//test:ok:tcp-request connection set-src hdr(x-forwarded-for)
//test:ok:tcp-request connection set-src hdr(x-forwarded-for) if some_check
//test:ok:tcp-request connection lua.foo
//test:ok:tcp-request connection lua.foo param if !HTTP
//test:ok:tcp-request connection lua.foo param param1
//test:ok:tcp-request session accept
//test:ok:tcp-request session accept if !HTTP
//test:ok:tcp-request session reject
//test:ok:tcp-request session reject if !HTTP
//test:ok:tcp-request session track-sc0 src
//test:ok:tcp-request session track-sc0 src if some_check
//test:ok:tcp-request session track-sc1 src
//test:ok:tcp-request session track-sc1 src if some_check
//test:ok:tcp-request session track-sc2 src
//test:ok:tcp-request session track-sc2 src if some_check
//test:ok:tcp-request session track-sc0 src table foo
//test:ok:tcp-request session track-sc0 src table foo if some_check
//test:ok:tcp-request session track-sc1 src table foo
//test:ok:tcp-request session track-sc1 src table foo if some_check
//test:ok:tcp-request session track-sc2 src table foo
//test:ok:tcp-request session track-sc2 src table foo if some_check
//test:ok:tcp-request session sc-inc-gpc0(2)
//test:ok:tcp-request session sc-inc-gpc0(2) if is-error
//test:ok:tcp-request session sc-inc-gpc1(2)
//test:ok:tcp-request session sc-inc-gpc1(2) if is-error
//test:ok:tcp-request session sc-set-gpt0(0) 1337
//test:ok:tcp-request session sc-set-gpt0(0) 1337 if exceeds_limit
//test:ok:tcp-request session set-var(sess.src) src
//test:ok:tcp-request session set-var(sess.dn) ssl_c_s_dn
//test:ok:tcp-request session unset-var(sess.src)
//test:ok:tcp-request session unset-var(sess.dn)
//test:ok:tcp-request session silent-drop
//test:ok:tcp-request session silent-drop if !HTTP
//test:fail:tcp-request
//test:fail:tcp-request content
//test:fail:tcp-request connection
//test:fail:tcp-request session
//test:fail:tcp-request content lua.
//test:fail:tcp-request content lua. param
//test:fail:tcp-request connection lua.
//test:fail:tcp-request connection lua. param
//test:fail:tcp-request content track-sc0 src table
//test:fail:tcp-request content track-sc0 src table if some_check
//test:fail:tcp-request content track-sc1 src table
//test:fail:tcp-request content track-sc1 src table if some_check
//test:fail:tcp-request content track-sc2 src table
//test:fail:tcp-request content track-sc2 src table if some_check
//test:fail:tcp-request connection track-sc0 src table
//test:fail:tcp-request connection track-sc0 src table if some_check
//test:fail:tcp-request connection track-sc1 src table
//test:fail:tcp-request connection track-sc1 src table if some_check
//test:fail:tcp-request connection track-sc2 src table
//test:fail:tcp-request connection track-sc2 src table if some_check
//test:fail:tcp-request session track-sc0 src table
//test:fail:tcp-request session track-sc0 src table if some_check
//test:fail:tcp-request session track-sc1 src table
//test:fail:tcp-request session track-sc1 src table if some_check
//test:fail:tcp-request session track-sc2 src table
//test:fail:tcp-request session track-sc2 src table if some_check
type TCPRequests struct{}

//name:tcp-response
//sections:frontend,backend
//struct-name:Responses
//dir:tcp
//is-multiple:true
//parser-type:TCPType
//is-interface:true
//no-init:true
//no-parse:true
//test:ok:tcp-response content lua.foo
//test:ok:tcp-response content lua.foo param if !HTTP
//test:ok:tcp-response content lua.foo param param1
//test:fail:tcp-response
//test:fail:tcp-response content lua.
//test:fail:tcp-response content lua. param
type TCPResponses struct{}

//name:redirect
//sections:frontend,backend
//dir:http
//is-multiple:true
//parser-type:HTTPAction
//is-interface:true
//no-init:true
//no-parse:true
//test:fail:redirect
//test:ok:redirect prefix http://www.bar.com code 301 if { hdr(host) -i foo.com }
type Redirect struct{}

type StatsSettings interface {
	Parse(parts []string, comment string) error
	String() string
	GetComment() string
}

//name:stats
//sections:defaults,frontend,backend
//struct-name:Stats
//dir:stats
//is-multiple:true
//parser-type:StatsSettings
//is-interface:true
//no-init:true
//no-parse:true
//test:fail:stats
//test:ok:stats admin if LOCALHOST
//test:ok:stats auth admin1:AdMiN123
//test:fail:stats auth admin1:
//test:fail:stats auth
//test:ok:stats enable
//test:ok:stats hide-version
//test:ok:stats show-legends
//test:fail:stats NON-EXISTS
//test:ok:stats maxconn 10
//test:fail:stats maxconn WORD
//test:ok:stats realm HAProxy\\ Statistics
//test:ok:stats refresh 10s
//test:fail:stats refresh
//test:ok:stats scope .
//test:fail:stats scope
//test:ok:stats show-desc Master node for Europe, Asia, Africa
//test:ok:stats show-node
//test:ok:stats show-node Europe-1
//test:ok:stats uri /admin?stats
//test:fail:stats uri
//test:ok:stats bind-process all
//test:ok:stats bind-process odd
//test:ok:stats bind-process even
//test:ok:stats bind-process 1 2 3 4
//test:ok:stats bind-process 1-4
//test:fail:stats bind-process none
//test:fail:stats bind-process 1+4
//test:fail:stats bind-process none-none
//test:fail:stats bind-process 1-4 1-3
//test:ok:stats http-request realm HAProxy\\ Statistics
//test:ok:stats http-request realm HAProxy\\ Statistics if something
//test:ok:stats http-request auth if something
//test:ok:stats http-request deny unless something
//test:ok:stats http-request allow
//test:fail:stats http-request
//test:fail:stats http-request none
type Stats struct{}
