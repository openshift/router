package haproxytime_test

import (
	"testing"

	"github.com/openshift/router/pkg/router/template/util/haproxytime"
)

// Run as: go test -bench=. -test.run=BenchmarkParseDuration -benchmem -count=1 -benchtime=1s
//
//	% go test -bench=. -test.run=BenchmarkParseDuration -benchmem -count=1 -benchtime=1s
//	goos: linux
//	goarch: amd64
//	pkg: github.com/openshift/router/pkg/router/template/util/haproxytime
//	cpu: 11th Gen Intel(R) Core(TM) i7-1165G7 @ 2.80GHz
//	BenchmarkParseDuration-8   	 4414666	       275.6 ns/op	     112 B/op	       2 allocs/op
//	PASS
//	ok  	github.com/openshift/router/pkg/router/template/util/haproxytime	1.495s
//
// If you modify ParseDuration() and fictitiously remove the regexp
// and the associated handling of numericPart and unitPart and just
// assume the numericPart=input then the following results show the
// cost of parsing with regular expressions.
//
//	% go test -bench=. -test.run=BenchmarkParseDuration -benchmem -count=1 -benchtime=1s
//	goos: linux
//	goarch: amd64
//	pkg: github.com/openshift/router/pkg/router/template/util/haproxytime
//	cpu: 11th Gen Intel(R) Core(TM) i7-1165G7 @ 2.80GHz
//	BenchmarkParseDuration-8   	72849655	        16.44 ns/op	       0 B/op	       0 allocs/op
//	PASS
//	ok  	github.com/openshift/router/pkg/router/template/util/haproxytime	1.217s
func BenchmarkParseDuration(b *testing.B) {
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, err := haproxytime.ParseDuration("2147483647")
		if err != nil {
			b.Fatal(err)
		}
	}
}
