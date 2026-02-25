package haproxy

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"k8s.io/apimachinery/pkg/util/sets"

	templaterouter "github.com/openshift/router/pkg/router/template"
	templateutil "github.com/openshift/router/pkg/router/template/util"
)

const (
	// listMapHeader is the header added if required to the "show map"
	// output from haproxy, so that we can parse the CSV output.
	// Note: This should match the CSV tags used in mapListEntry.
	showMapListHeader = "id (file) description"

	// showMapHeader is the header we add to the "show map $name"
	// output from haproxy, so that we can parse the CSV output.
	// Note: This should match the CSV tags used in HAProxyMapEntry.
	showMapHeader = "id name value"
)

type mapListEntry struct {
	ID     string `csv:"id"`
	Name   string `csv:"(file)"`
	Unused string `csv:"-"`
}

// HAPrroxyMapEntry is an entry in HAProxyMap.
type HAProxyMapEntry struct {
	// ID is the internal haproxy id associated with this map entry.
	// It is required for deleting map entries.
	ID string `csv:"id"`

	// Name is the entry key.
	Name string `csv:"name"`

	// Value is the entry value.
	Value string `csv:"value"`
}

// HAProxyMap is a structure representing an haproxy map.
type HAProxyMap struct {
	// name is the haproxy specific name for this map.
	name string

	// client is the haproxy dynamic API client.
	client *Client

	// entries are the haproxy map entries.
	// Note: This is _not_ a hashtable/map/dict as it can have
	// duplicate entries with the same key.
	entries []*HAProxyMapEntry

	// dirty indicates the state of the map.
	dirty bool
}

// buildHAProxyMaps builds and returns a list of haproxy maps.
// Note: Maps are lazily populated based on their usage.
func buildHAProxyMaps(c *Client) ([]*HAProxyMap, error) {
	entries := []*mapListEntry{}
	converter := NewCSVConverter(showMapListHeader, &entries, fixupMapListOutput)

	if _, err := c.RunCommand("show map", converter); err != nil {
		return []*HAProxyMap{}, err
	}

	maps := make([]*HAProxyMap, len(entries))
	for k, v := range entries {
		m := newHAProxyMap(v.Name, c)
		maps[k] = m
	}

	return maps, nil
}

// newHAProxyMap returns a new HAProxyMap representing a haproxy map.
func newHAProxyMap(name string, client *Client) *HAProxyMap {
	return &HAProxyMap{
		name:    name,
		client:  client,
		entries: make([]*HAProxyMapEntry, 0),
		dirty:   true,
	}
}

// Refresh refreshes the data in this haproxy map.
func (m *HAProxyMap) Refresh() error {
	cmd := fmt.Sprintf("show map %s", m.name)
	// using an empty slice instead of m.entries, this avoids
	// leftover items in the end in case the list shrinks.
	entries := []*HAProxyMapEntry{}
	converter := NewCSVConverter(showMapHeader, &entries, nil)
	if _, err := m.client.RunCommand(cmd, converter); err != nil {
		return err
	}

	m.entries = entries
	m.dirty = false
	return nil
}

// Commit commits all the pending changes made to this haproxy map.
// We do map changes "in-band" as that's handled dynamically by haproxy.
func (m *HAProxyMap) Commit() error {
	// noop
	return nil
}

// Name returns the name of this map.
func (m *HAProxyMap) Name() string {
	return m.name
}

// Find returns a list of matching entries in the haproxy map.
func (m *HAProxyMap) Find(k string) ([]HAProxyMapEntry, error) {
	found := make([]HAProxyMapEntry, 0)

	if m.dirty {
		if err := m.Refresh(); err != nil {
			return found, err
		}
	}

	for _, entry := range m.entries {
		if entry.Name == k {
			clonedEntry := HAProxyMapEntry{
				ID:    entry.ID,
				Name:  entry.Name,
				Value: entry.Value,
			}
			found = append(found, clonedEntry)
		}
	}

	return found, nil
}

// SyncEntries merges current content from a HAProxy map, and changes applied in a route resource (newEntries).
// The new content is applied atomically, and in the correct order to avoid wrong match in case of path overlap.
func (m *HAProxyMap) SyncEntries(newEntries configEntryMap, add bool) error {
	if m.dirty {
		m.Refresh()
	}

	// m.entries[].(id;name(key);value) is a slice with the current state,
	// newEntries[k]v is a hashmap with the entries to be added/replaced or removed.
	// merge them together, based on `add` flag, and store the result on `lines[]`.

	var lines []string
	added := sets.NewString()
	for _, entry := range m.entries {
		currentValue := entry.Value
		if value, found := newEntries[entry.Name]; found {
			if add {
				// if adding, use the new content from the newEntries and mark as already added
				currentValue = string(value)
				// flag instead of delete() so we preserve the hashmap content
				added.Insert(entry.Name)
			} else {
				// if removing, remove from the final output
				currentValue = ""
			}
		}
		if currentValue != "" {
			lines = append(lines, entry.Name+" "+currentValue)
		}
	}
	if add {
		for k, v := range newEntries {
			if !added.Has(k) {
				lines = append(lines, k+" "+string(v))
			}
		}
	}

	// Sort entries to avoid wrong match, see https://issues.redhat.com/browse/OCPBUGS-75009
	lines = templateutil.SortMapPaths(lines, `^[^\.]*\.`)

	// atomically replacing a map is a three steps workflow:
	// - prepare map <name>: creates a new and empty version
	// - add map @<version> <name> <<: receives a payload with new content
	// - commit map @<version>: atomically replaces the new content

	// preparing and acquiring the transaction version
	prepareResponseRaw, err := m.client.Execute("prepare map " + m.name)
	if err != nil {
		return err
	}
	prepareResponse := strings.TrimSpace(string(prepareResponseRaw))
	versionStr := strings.TrimPrefix(prepareResponse, "New version created: ")
	version, _ := strconv.Atoi(versionStr)
	if version == 0 {
		return fmt.Errorf("unrecognized response preparing a new map: %q", prepareResponse)
	}

	// adding the new payload
	cmdAddMap := &strings.Builder{}
	_, _ = fmt.Fprintf(cmdAddMap, "add map @%d %s <<\n", version, m.name)
	for _, line := range lines {
		_, _ = fmt.Fprintln(cmdAddMap, line)
	}
	addMapResponseRaw, err := m.client.Execute(cmdAddMap.String())
	if err != nil {
		return err
	}
	addMapResponse := strings.TrimSpace(string(addMapResponseRaw))
	if addMapResponse != "" {
		return fmt.Errorf("unrecognized response adding new map content: %s", addMapResponse)
	}

	// We're going to commit, better to make cache dirty right here, instead of waiting for a false negative
	// from `commit` call. If commit response fails but HAProxy's commit succeed, we'd become inconsistent.
	m.dirty = true

	// commiting the new content
	commitMapResponseRaw, err := m.client.Execute(fmt.Sprintf("commit map @%d %s", version, m.name))
	if err != nil {
		return err
	}
	commitMapResponse := strings.TrimSpace(string(commitMapResponseRaw))
	if commitMapResponse != "" {
		return fmt.Errorf("unrecognized response commiting new map content: %s", commitMapResponse)
	}

	return nil
}

// Add adds a new key and value to the haproxy map and allows all previous
// entries in the map to be deleted (replaced).
func (m *HAProxyMap) Add(k string, v templaterouter.ServiceAliasConfigKey, replace bool) error {
	if replace {
		if err := m.Delete(k); err != nil {
			return err
		}
	}

	return m.addEntry(k, v)
}

// Delete removes all the matching keys from the haproxy map.
func (m *HAProxyMap) Delete(k string) error {
	entries, err := m.Find(k)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		if err := m.deleteEntry(entry.ID); err != nil {
			return err
		}
	}

	return nil
}

// DeleteEntry removes a specific haproxy map entry.
func (m *HAProxyMap) DeleteEntry(id string) error {
	return m.deleteEntry(id)
}

// addEntry adds a new haproxy map entry.
func (m *HAProxyMap) addEntry(k string, v templaterouter.ServiceAliasConfigKey) error {
	keyExpr := escapeKeyExpr(string(k))
	cmd := fmt.Sprintf("add map %s %s %s", m.name, keyExpr, v)
	responseBytes, err := m.client.Execute(cmd)
	if err != nil {
		return err
	}

	response := strings.TrimSpace(string(responseBytes))
	if len(response) > 0 {
		return fmt.Errorf("adding map %s entry %s: %v", m.name, keyExpr, string(response))
	}

	m.dirty = true
	return nil
}

// deleteEntry removes a specific haproxy map entry.
func (m *HAProxyMap) deleteEntry(id string) error {
	cmd := fmt.Sprintf("del map %s #%s", m.name, id)
	if _, err := m.client.Execute(cmd); err != nil {
		return err
	}

	m.dirty = true
	return nil
}

// escapeKeyExpr escapes meta characters in the haproxy map entry key name.
func escapeKeyExpr(k string) string {
	v := strings.Replace(k, `\`, `\\`, -1)
	return strings.Replace(v, `.`, `\.`, -1)
}

// Regular expression to fixup haproxy map list funky output.
var listMapOutputRE *regexp.Regexp = regexp.MustCompile(`(?m)^(-|)([0-9]*) \((.*)?\).*$`)

// fixupMapListOutput fixes up the funky output haproxy "show map" returns.
func fixupMapListOutput(data []byte) ([]byte, error) {
	replacement := []byte(`$1$2 $3 loaded`)
	return listMapOutputRE.ReplaceAll(data, replacement), nil
}
