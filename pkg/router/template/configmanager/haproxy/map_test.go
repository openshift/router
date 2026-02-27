package haproxy

import (
	"testing"

	"github.com/stretchr/testify/require"

	haproxytesting "github.com/openshift/router/pkg/router/template/configmanager/haproxy/testing"
)

// TestBuildHAProxyMaps tests haproxy maps.
func TestBuildHAProxyMaps(t *testing.T) {
	server := haproxytesting.StartFakeServerForTest(t)
	defer server.Stop()

	testCases := []struct {
		name            string
		sockFile        string
		failureExpected bool
	}{
		{
			name:            "empty socket",
			sockFile:        "",
			failureExpected: true,
		},
		{
			name:            "valid socket",
			sockFile:        server.SocketFile(),
			failureExpected: false,
		},
		{
			name:            "non-existent socket",
			sockFile:        "/non-existent/fake-haproxy.sock",
			failureExpected: true,
		},
	}

	for _, tc := range testCases {
		client := NewClient(tc.sockFile, 0)
		if client == nil {
			t.Errorf("TestBuildHAProxyMaps test case %s failed with no client.", tc.name)
		}

		haproxyMaps, err := buildHAProxyMaps(client)
		if tc.failureExpected {
			if err == nil {
				t.Errorf("TestBuildHAProxyMaps test case %s expected an error but got none.", tc.name)
			}
			continue
		}

		if err != nil {
			t.Errorf("TestBuildHAProxyMaps test case %s expected no error but got: %v", tc.name, err)
		}
		if len(haproxyMaps) == 0 {
			t.Errorf("TestBuildHAProxyMaps test case %s expected to get maps", tc.name)
		}
	}
}

// TestNewHAProxyMap tests a new haproxy map.
func TestNewHAProxyMap(t *testing.T) {
	server := haproxytesting.StartFakeServerForTest(t)
	defer server.Stop()

	testCases := []struct {
		name     string
		sockFile string
	}{
		{
			name:     "empty",
			sockFile: "",
		},
		{
			name:     "valid socket",
			sockFile: server.SocketFile(),
		},
		{
			name:     "non-existent socket",
			sockFile: "/non-existent/fake-haproxy.sock",
		},
	}

	for _, tc := range testCases {
		client := NewClient(tc.sockFile, 0)
		if client == nil {
			t.Errorf("TestNewHAProxyMap test case %s failed with no client.", tc.name)
		}

		if m := newHAProxyMap(tc.name, client); m == nil {
			t.Errorf("TestNewHAProxyMap test case %s expected a map but got none", tc.name)
		}
	}
}

// TestHAProxyMapRefresh tests haproxy map refresh.
func TestHAProxyMapRefresh(t *testing.T) {
	server := haproxytesting.StartFakeServerForTest(t)
	defer server.Stop()

	testCases := []struct {
		name            string
		sockFile        string
		mapName         string
		failureExpected bool
	}{
		{
			name:            "empty socket",
			sockFile:        "",
			mapName:         "empty.map",
			failureExpected: true,
		},
		{
			name:            "empty socket and valid map",
			sockFile:        "",
			mapName:         "/var/lib/haproxy/conf/os_sni_passthrough.map",
			failureExpected: true,
		},
		{
			name:            "valid socket and map",
			sockFile:        server.SocketFile(),
			mapName:         "/var/lib/haproxy/conf/os_http_be.map",
			failureExpected: false,
		},
		{
			name:            "valid socket but invalid map",
			sockFile:        server.SocketFile(),
			mapName:         "missing.map",
			failureExpected: true,
		},
		{
			name:            "valid socket but typo map",
			sockFile:        server.SocketFile(),
			mapName:         "/var/lib/haproxy/conf/os_http_be.map-1234",
			failureExpected: true,
		},
		{
			name:            "non-existent socket",
			sockFile:        "/non-existent/fake-haproxy.sock",
			mapName:         "non-existent.map",
			failureExpected: true,
		},
		{
			name:            "non-existent socket valid map",
			sockFile:        "/non-existent/fake-haproxy.sock",
			mapName:         "/var/lib/haproxy/conf/os_tcp_be.map",
			failureExpected: true,
		},
	}

	for _, tc := range testCases {
		client := NewClient(tc.sockFile, 0)
		if client == nil {
			t.Errorf("TestHAProxyMapRefresh test case %s failed with no client.", tc.name)
		}

		m := newHAProxyMap(tc.mapName, client)
		err := m.Refresh()
		if tc.failureExpected {
			if err == nil {
				t.Errorf("TestHAProxyMapRefresh test case %s expected an error but got none.", tc.name)
			}
			continue
		}

		if err != nil {
			t.Errorf("TestHAProxyMapRefresh test case %s expected no error but got: %v", tc.name, err)
		}
	}
}

// TestHAProxyMapCommit tests haproxy map commit.
func TestHAProxyMapCommit(t *testing.T) {
	server := haproxytesting.StartFakeServerForTest(t)
	defer server.Stop()

	testCases := []struct {
		name     string
		sockFile string
		mapName  string
	}{
		{
			name:     "empty socket",
			sockFile: "",
			mapName:  "empty.map",
		},
		{
			name:     "empty socket valid map",
			sockFile: "",
			mapName:  "/var/lib/haproxy/conf/os_sni_passthrough.map",
		},
		{
			name:     "valid socket",
			sockFile: server.SocketFile(),
			mapName:  "/var/lib/haproxy/conf/os_http_be.map",
		},
		{
			name:     "valid socket but invalid map",
			sockFile: server.SocketFile(),
			mapName:  "missing.map",
		},
		{
			name:     "valid socket but typo map",
			sockFile: server.SocketFile(),
			mapName:  "/var/lib/haproxy/conf/os_http_be.map-1234",
		},
		{
			name:     "non-existent socket",
			sockFile: "/non-existent/fake-haproxy.sock",
			mapName:  "non-existent.map",
		},
		{
			name:     "non-existent socket valid map",
			sockFile: "/non-existent/fake-haproxy.sock",
			mapName:  "/var/lib/haproxy/conf/os_tcp_be.map",
		},
	}

	for _, tc := range testCases {
		client := NewClient(tc.sockFile, 0)
		if client == nil {
			t.Errorf("TestHAProxyMapCommit test case %s failed with no client.", tc.name)
		}

		m := newHAProxyMap(tc.mapName, client)
		if err := m.Commit(); err != nil {
			t.Errorf("TestHAProxyMapCommit test case %s expected no error but got: %v", tc.name, err)
		}
	}
}

// TestHAProxyMapName tests haproxy map returns its name.
func TestHAProxyMapName(t *testing.T) {
	server := haproxytesting.StartFakeServerForTest(t)
	defer server.Stop()

	testCases := []struct {
		name            string
		sockFile        string
		mapName         string
		failureExpected bool
	}{
		{
			name:            "empty socket",
			sockFile:        "",
			mapName:         "empty.map",
			failureExpected: true,
		},
		{
			name:            "empty socket valid map",
			sockFile:        "",
			mapName:         "/var/lib/haproxy/conf/os_sni_passthrough.map",
			failureExpected: true,
		},
		{
			name:            "valid socket",
			sockFile:        server.SocketFile(),
			mapName:         "/var/lib/haproxy/conf/os_http_be.map",
			failureExpected: false,
		},
		{
			name:            "valid socket but invalid map",
			sockFile:        server.SocketFile(),
			mapName:         "missing.map",
			failureExpected: true,
		},
		{
			name:            "valid socket but typo map",
			sockFile:        server.SocketFile(),
			mapName:         "/var/lib/haproxy/conf/os_http_be.map-1234",
			failureExpected: true,
		},
		{
			name:            "non-existent socket",
			sockFile:        "/non-existent/fake-haproxy.sock",
			mapName:         "non-existent.map",
			failureExpected: true,
		},
		{
			name:            "non-existent socket valid map",
			sockFile:        "/non-existent/fake-haproxy.sock",
			mapName:         "/var/lib/haproxy/conf/os_tcp_be.map",
			failureExpected: true,
		},
	}

	for _, tc := range testCases {
		client := NewClient(tc.sockFile, 0)
		if client == nil {
			t.Errorf("TestHAProxyMapRefresh test case %s failed with no client.", tc.name)
		}

		m := newHAProxyMap(tc.mapName, client)
		err := m.Refresh()
		if tc.failureExpected {
			if err == nil {
				t.Errorf("TestHAProxyMapRefresh test case %s expected an error but got none.", tc.name)
			}
			continue
		}

		if err != nil {
			t.Errorf("TestHAProxyMapRefresh test case %s expected no error but got: %v", tc.name, err)
		}
	}
}

// TestHAProxyMapFind tests finding an entry in a haproxy map.
func TestHAProxyMapFind(t *testing.T) {
	server := haproxytesting.StartFakeServerForTest(t)
	defer server.Stop()

	testCases := []struct {
		name            string
		sockFile        string
		mapName         string
		keyName         string
		failureExpected bool
		entriesExpected bool
	}{
		{
			name:            "empty socket",
			sockFile:        "",
			mapName:         "empty.map",
			keyName:         "k1",
			failureExpected: true,
			entriesExpected: false,
		},
		{
			name:            "empty socket valid map and key",
			sockFile:        "",
			mapName:         "/var/lib/haproxy/conf/os_sni_passthrough.map",
			keyName:         `^route\.passthrough\.test(:[0-9]+)?(/.*)?$`,
			failureExpected: true,
			entriesExpected: false,
		},
		{
			name:            "empty socket valid map and invalid key",
			sockFile:        "",
			mapName:         "/var/lib/haproxy/conf/os_sni_passthrough.map",
			keyName:         "non-existent-key",
			failureExpected: true,
			entriesExpected: false,
		},
		{
			name:            "valid socket, map and key",
			sockFile:        server.SocketFile(),
			mapName:         "/var/lib/haproxy/conf/os_http_be.map",
			keyName:         `^route\.allow-http\.test(:[0-9]+)?(/.*)?$`,
			failureExpected: false,
			entriesExpected: true,
		},
		{
			name:            "valid socket but invalid map",
			sockFile:        server.SocketFile(),
			mapName:         "missing.map",
			keyName:         `^route\.allow-http\.test(:[0-9]+)?(/.*)?$`,
			failureExpected: true,
			entriesExpected: false,
		},
		{
			name:            "valid socket but invalid map and key",
			sockFile:        server.SocketFile(),
			mapName:         "missing.map",
			keyName:         "invalid-key",
			failureExpected: true,
			entriesExpected: false,
		},
		{
			name:            "valid socket but invalid key",
			sockFile:        server.SocketFile(),
			mapName:         "/var/lib/haproxy/conf/os_http_be.map",
			keyName:         "invalid-key",
			failureExpected: false,
			entriesExpected: false,
		},
		{
			name:            "valid socket but typo map",
			sockFile:        server.SocketFile(),
			mapName:         "/var/lib/haproxy/conf/os_http_be.map-1234",
			keyName:         `^route\.allow-http\.test(:[0-9]+)?(/.*)?$`,
			failureExpected: true,
			entriesExpected: false,
		},
		{
			name:            "non-existent socket",
			sockFile:        "/non-existent/fake-haproxy.sock",
			mapName:         "non-existent.map",
			keyName:         "invalid-key",
			failureExpected: true,
			entriesExpected: false,
		},
		{
			name:            "non-existent socket valid map",
			sockFile:        "/non-existent/fake-haproxy.sock",
			mapName:         "/var/lib/haproxy/conf/os_tcp_be.map",
			keyName:         "invalid-key",
			failureExpected: true,
			entriesExpected: false,
		},
		{
			name:            "non-existent socket invalid map",
			sockFile:        "/non-existent/fake-haproxy.sock",
			mapName:         "404.map",
			keyName:         `^reencrypt\.blueprints\.org(:[0-9]+)?(/.*)?$`,
			failureExpected: true,
			entriesExpected: false,
		},
		{
			name:            "non-existent socket valid map and key",
			sockFile:        "/non-existent/fake-haproxy.sock",
			mapName:         "/var/lib/haproxy/conf/os_tcp_be.map",
			keyName:         `^reencrypt\.blueprints\.org(:[0-9]+)?(/.*)?$`,
			failureExpected: true,
			entriesExpected: false,
		},
	}

	for _, tc := range testCases {
		client := NewClient(tc.sockFile, 0)
		if client == nil {
			t.Errorf("TestHAProxyMapFind test case %s failed with no client.", tc.name)
		}

		// Ensure server is in clean state for test.
		server.Reset()

		m := newHAProxyMap(tc.mapName, client)
		entries, err := m.Find(tc.keyName)
		if tc.failureExpected {
			if err == nil {
				t.Errorf("TestHAProxyMapFind test case %s expected an error but got none.", tc.name)
			}
			continue
		}

		if err != nil {
			t.Errorf("TestHAProxyMapFind test case %s expected no error but got: %v", tc.name, err)
		}
		if tc.entriesExpected && len(entries) < 1 {
			t.Errorf("TestHAProxyMapFind test case %s expected to find an entry but got: %v", tc.name, len(entries))
		}
	}
}

// TestHAProxyMapSyncEntries tests adding/replacing/removing entries in a haproxy map.
func TestHAProxyMapSyncEntries(t *testing.T) {
	server := haproxytesting.StartFakeServerForTest(t)
	defer server.Stop()

	testCases := []struct {
		name            string
		currentEntries  []string
		newEntries      configEntryMap
		add             bool
		expectedEntries []string
	}{
		{
			name:            "add simple pattern",
			currentEntries:  []string{},
			newEntries:      configEntryMap{"k": "v"},
			add:             true,
			expectedEntries: []string{"1 k v"},
		},
		{
			name:            "remove simple pattern",
			currentEntries:  []string{"1 k v"},
			newEntries:      configEntryMap{"k": "v"},
			add:             true,
			expectedEntries: []string{"1 k v"},
		},
		{
			name:            "replace value",
			currentEntries:  []string{"1 k v1"},
			newEntries:      configEntryMap{"k": "v2"},
			add:             true,
			expectedEntries: []string{"1 k v2"},
		},
		{
			name:            "remove non-existing key",
			currentEntries:  []string{"1 k1 v"},
			newEntries:      configEntryMap{"k2": "v"},
			add:             false,
			expectedEntries: []string{"1 k1 v"},
		},
		{
			name:            "remove non-matching value",
			currentEntries:  []string{"1 k v1"},
			newEntries:      configEntryMap{"k": "v2"},
			add:             false,
			expectedEntries: nil,
		},
		{
			name: "add and reorder",
			currentEntries: []string{
				`1 ^sub\.route\.test(:[0-9]+)?(/.*)?$ be_edge_http:default:test-http1`,
				`2 ^route\.test(:[0-9]+)?(/.*)?$ be_edge_http:default:test-http2`,
			},
			newEntries: configEntryMap{
				`^something-else\.test(:[0-9]+)?(/.*)?$`: `be_edge_http:default:test-http3`,
			},
			add: true,
			expectedEntries: []string{
				`1 ^sub\.route\.test(:[0-9]+)?(/.*)?$ be_edge_http:default:test-http1`,
				`2 ^something-else\.test(:[0-9]+)?(/.*)?$ be_edge_http:default:test-http3`,
				`3 ^route\.test(:[0-9]+)?(/.*)?$ be_edge_http:default:test-http2`,
			},
		},
	}

	const customMapName = "custom.map"

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			client := NewClient(server.SocketFile(), 0)
			if client == nil {
				t.Errorf("TestHAProxyMapAdd test case %s failed with no client.", tc.name)
			}

			// Ensure server is in clean state for test.
			server.Reset()
			server.SetCustomMap(customMapName, tc.currentEntries)

			m := newHAProxyMap(customMapName, client)
			err := m.SyncEntries(tc.newEntries, tc.add)
			require.NoError(t, err)
			require.Equal(t, tc.expectedEntries, server.ReadMapContent(customMapName))
		})
	}
}
