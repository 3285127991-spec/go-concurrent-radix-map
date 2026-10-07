// Package radixmap provides a concurrency-safe map from string keys to
// string values backed by a compressed radix trie.
package radixmap

import (
	"errors"
	"sort"
	"strings"
	"sync"
)

// ErrClosed is returned by Put and Delete after the map has been closed.
var ErrClosed = errors.New("radixmap: map is closed")

// ErrEmptyKey is returned by Put for empty keys; only non-empty keys are stored.
var ErrEmptyKey = errors.New("radixmap: empty key")

// Entry is a single key/value pair stored in the map.
type Entry struct {
	Key   string
	Value string
}

// node is one node of the compressed radix trie. The full key of a node is
// the concatenation of the segments on the path from the root to the node.
type node struct {
	segment  string
	value    string
	hasValue bool
	children map[byte]*node // keyed by the first byte of the child segment
}

func newNode(segment string) *node {
	return &node{segment: segment, children: make(map[byte]*node)}
}

// Map is a concurrency-safe radix trie mapping string keys to string values.
// The zero value is not usable; create one with New.
type Map struct {
	mu     sync.RWMutex
	root   *node
	size   int
	closed bool
}

// New returns an empty Map ready for use.
func New() *Map {
	return &Map{root: newNode("")}
}

// commonPrefixLen returns the length of the longest common prefix of a and b.
func commonPrefixLen(a, b string) int {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	i := 0
	for i < n && a[i] == b[i] {
		i++
	}
	return i
}

// Put stores key/value, updating the value in place if key already exists.
// It fails with ErrEmptyKey for an empty key and with ErrClosed after Close.
func (m *Map) Put(key, value string) error {
	if key == "" {
		return ErrEmptyKey
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return ErrClosed
	}

	current := m.root
	remaining := key
	for {
		first := remaining[0]
		child, ok := current.children[first]
		if !ok {
			leaf := newNode(remaining)
			leaf.value, leaf.hasValue = value, true
			current.children[first] = leaf
			m.size++
			return nil
		}
		common := commonPrefixLen(child.segment, remaining)
		if common == len(child.segment) {
			if common == len(remaining) {
				// Exact key match: only the value is updated.
				if !child.hasValue {
					m.size++
				}
				child.value, child.hasValue = value, true
				return nil
			}
			current = child
			remaining = remaining[common:]
			continue
		}
		// Partial overlap: split the compressed edge at the common prefix.
		split := newNode(child.segment[:common])
		child.segment = child.segment[common:]
		split.children[child.segment[0]] = child
		current.children[first] = split
		if common == len(remaining) {
			// The new key ends exactly at the split point.
			split.value, split.hasValue = value, true
		} else {
			leaf := newNode(remaining[common:])
			leaf.value, leaf.hasValue = value, true
			split.children[remaining[common]] = leaf
		}
		m.size++
		return nil
	}
}

// find locates the node whose full key equals key. It returns the chain of
// nodes from the root down to that node and whether the key was fully
// matched. The last element of path is the matching node itself.
func (m *Map) find(key string) (path []*node, ok bool) {
	if key == "" {
		return nil, false
	}
	current := m.root
	path = []*node{current}
	remaining := key
	for len(remaining) > 0 {
		child, found := current.children[remaining[0]]
		if !found || len(remaining) < len(child.segment) ||
			child.segment != remaining[:len(child.segment)] {
			return nil, false
		}
		current = child
		path = append(path, current)
		remaining = remaining[len(child.segment):]
	}
	return path, true
}

// Get returns the value stored for key and whether key exists.
func (m *Map) Get(key string) (string, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	path, ok := m.find(key)
	if !ok || !path[len(path)-1].hasValue {
		return "", false
	}
	return path[len(path)-1].value, true
}

// Delete removes key and reports whether it existed. It fails with ErrClosed
// after Close. Internal nodes left without a value and a single child are
// merged back so the trie stays normalized.
func (m *Map) Delete(key string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return false, ErrClosed
	}
	path, ok := m.find(key)
	if !ok || !path[len(path)-1].hasValue {
		return false, nil
	}
	target := path[len(path)-1]
	target.value, target.hasValue = "", false
	m.size--

	// Normalize upwards: remove value-less leaves and merge value-less
	// single-child nodes into their only child.
	for i := len(path) - 1; i > 0; i-- {
		current, parent := path[i], path[i-1]
		if current.hasValue || len(current.children) >= 2 {
			break
		}
		if len(current.children) == 0 {
			delete(parent.children, current.segment[0])
		} else {
			for _, child := range current.children {
				child.segment = current.segment + child.segment
				parent.children[current.segment[0]] = child
			}
		}
	}
	return true, nil
}

// LongestPrefix returns the entry whose key is the longest prefix of input
// among all stored keys. The second result is false when no stored key is a
// prefix of input.
func (m *Map) LongestPrefix(input string) (Entry, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	current := m.root
	remaining := input
	key := ""
	var best *Entry
	for len(remaining) > 0 {
		child, ok := current.children[remaining[0]]
		if !ok || len(remaining) < len(child.segment) ||
			child.segment != remaining[:len(child.segment)] {
			// No matching child, or input ends in the middle of a
			// compressed edge: the node past that edge has a key longer
			// than input, so it cannot be a prefix of input.
			break
		}
		key += child.segment
		remaining = remaining[len(child.segment):]
		current = child
		if current.hasValue {
			entry := Entry{Key: key, Value: current.value}
			best = &entry
		}
	}
	if best == nil {
		return Entry{}, false
	}
	return *best, true
}

// Prefix returns all entries whose key starts with prefix, ordered by key in
// lexicographic (byte) order. An empty prefix returns every entry.
func (m *Map) Prefix(prefix string) []Entry {
	m.mu.RLock()
	defer m.mu.RUnlock()
	current := m.root
	remaining := prefix
	base := "" // full key of current's parent
	for len(remaining) > 0 {
		child, ok := current.children[remaining[0]]
		if !ok {
			return nil
		}
		if len(remaining) < len(child.segment) {
			// The query ends in the middle of a compressed edge; every
			// key below the child shares the queried prefix.
			if !strings.HasPrefix(child.segment, remaining) {
				return nil
			}
			base += current.segment
			current = child
			break
		}
		if child.segment != remaining[:len(child.segment)] {
			return nil
		}
		remaining = remaining[len(child.segment):]
		base += current.segment
		current = child
	}
	return collect(current, base)
}

// collect returns every entry in the subtree rooted at n in lexicographic
// key order. keyPrefix is the full key of n's parent.
func collect(n *node, keyPrefix string) []Entry {
	key := keyPrefix + n.segment
	var out []Entry
	if n.hasValue {
		out = append(out, Entry{Key: key, Value: n.value})
	}
	if len(n.children) > 0 {
		firsts := make([]byte, 0, len(n.children))
		for b := range n.children {
			firsts = append(firsts, b)
		}
		sort.Slice(firsts, func(i, j int) bool { return firsts[i] < firsts[j] })
		for _, b := range firsts {
			out = append(out, collect(n.children[b], key)...)
		}
	}
	return out
}

// Snapshot returns an independent copy of all entries, ordered by key in
// lexicographic order.
func (m *Map) Snapshot() []Entry {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return collect(m.root, "")
}

// Len returns the number of keys stored in the map.
func (m *Map) Len() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.size
}

// Close marks the map closed: subsequent Put and Delete calls fail with
// ErrClosed, while Get, LongestPrefix, Prefix, Snapshot and Len keep working.
// Close is idempotent and always returns nil.
func (m *Map) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	return nil
}
