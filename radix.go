// Package radixmap provides a concurrency-safe compressed radix trie
// that maps non-empty string keys to string values.
package radixmap

import (
	"errors"
	"sort"
	"strings"
	"sync"
)

var (
	// ErrClosed is returned by Put and Delete after Close has been called.
	ErrClosed = errors.New("radixmap: map is closed")
	// ErrEmptyKey is returned by Put for empty keys.
	ErrEmptyKey = errors.New("radixmap: key must be non-empty")
)

// Entry is a single key/value pair returned by Prefix and Snapshot.
type Entry struct {
	Key   string
	Value string
}

// node is a compressed radix trie node. label is the edge label from the
// parent to this node; children are kept sorted by their first label byte.
type node struct {
	label    string
	value    string
	hasValue bool
	children []*node
}

// Map is a concurrent-safe radix map. The zero value is not usable; use New.
type Map struct {
	mu     sync.RWMutex
	root   *node
	size   int
	closed bool
}

// New returns an empty Map.
func New() *Map {
	return &Map{root: &node{}}
}

// Put stores key/value. If key already exists only its value is updated.
// It fails with ErrEmptyKey for an empty key and ErrClosed after Close.
func (m *Map) Put(key, value string) error {
	if key == "" {
		return ErrEmptyKey
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return ErrClosed
	}

	cur := m.root
	rest := key
	for {
		child, idx := cur.findChild(rest[0])
		if child == nil {
			cur.insertChildAt(idx, &node{label: rest, value: value, hasValue: true})
			m.size++
			return nil
		}
		common := commonPrefixLen(rest, child.label)
		switch {
		case common == len(child.label) && common == len(rest):
			// Exact node: update the value in place.
			if !child.hasValue {
				m.size++
			}
			child.value = value
			child.hasValue = true
			return nil
		case common == len(child.label):
			// The whole edge matched; descend.
			cur = child
			rest = rest[common:]
		default:
			// Partial match: split the compressed edge at common.
			split := &node{label: child.label[:common]}
			child.label = child.label[common:]
			split.children = []*node{child}
			cur.children[idx] = split // same first byte, order preserved
			tail := rest[common:]
			if tail == "" {
				split.value = value
				split.hasValue = true
			} else {
				_, leafIdx := split.findChild(tail[0])
				split.insertChildAt(leafIdx, &node{label: tail, value: value, hasValue: true})
			}
			m.size++
			return nil
		}
	}
}

// Get returns the value stored for key and whether it was found.
func (m *Map) Get(key string) (string, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	n := m.root.find(key)
	if n == nil || !n.hasValue {
		return "", false
	}
	return n.value, true
}

// Delete removes key and reports whether it existed. The trie is
// re-normalized afterwards by merging nodes that lost their purpose.
func (m *Map) Delete(key string) bool {
	if key == "" {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return false
	}

	var path []*node
	cur := m.root
	rest := key
	for len(rest) > 0 {
		child, _ := cur.findChild(rest[0])
		if child == nil || !strings.HasPrefix(rest, child.label) {
			return false
		}
		path = append(path, child)
		rest = rest[len(child.label):]
		cur = child
	}
	if !cur.hasValue {
		return false
	}
	cur.hasValue = false
	cur.value = ""
	m.size--

	switch len(cur.children) {
	case 0:
		// Leaf lost its value: detach it from its parent.
		parent := m.root
		if len(path) >= 2 {
			parent = path[len(path)-2]
		}
		_, idx := parent.findChild(cur.label[0])
		parent.children = append(parent.children[:idx], parent.children[idx+1:]...)
		// The parent may now be a valueless node with a single child.
		if parent != m.root && !parent.hasValue && len(parent.children) == 1 {
			parent.mergeWithOnlyChild()
		}
	case 1:
		// Valueless node with one child: merge it with that child.
		cur.mergeWithOnlyChild()
	}
	return true
}

// LongestPrefix returns the entry whose key is the longest prefix of input
// among all stored keys. found is false when no stored key prefixes input.
func (m *Map) LongestPrefix(input string) (key, value string, found bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	cur := m.root
	rest := input
	var matched strings.Builder
	for len(rest) > 0 {
		child, _ := cur.findChild(rest[0])
		// Also false when input ends in the middle of a compressed edge.
		if child == nil || !strings.HasPrefix(rest, child.label) {
			break
		}
		matched.WriteString(child.label)
		rest = rest[len(child.label):]
		cur = child
		if cur.hasValue {
			key, value, found = matched.String(), cur.value, true
		}
	}
	return key, value, found
}

// Prefix returns all entries whose key starts with prefix, ordered by key.
// A prefix that ends in the middle of a compressed edge matches the whole
// subtree below that edge.
func (m *Map) Prefix(prefix string) []Entry {
	m.mu.RLock()
	defer m.mu.RUnlock()

	cur := m.root
	rest := prefix
	var base strings.Builder
	for len(rest) > 0 {
		child, _ := cur.findChild(rest[0])
		if child == nil {
			return nil
		}
		if len(rest) < len(child.label) {
			// Prefix ends mid-edge: the whole subtree matches.
			if !strings.HasPrefix(child.label, rest) {
				return nil
			}
			base.WriteString(child.label)
			cur = child
			break
		}
		if !strings.HasPrefix(rest, child.label) {
			return nil
		}
		base.WriteString(child.label)
		rest = rest[len(child.label):]
		cur = child
	}
	var out []Entry
	collect(cur, base.String(), &out)
	return out
}

// Snapshot returns an independent copy of all entries ordered by key.
func (m *Map) Snapshot() []Entry {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []Entry
	collect(m.root, "", &out)
	return out
}

// Len returns the number of stored keys.
func (m *Map) Len() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.size
}

// Close marks the map closed. Put and Delete are rejected afterwards while
// all read operations keep working. Close is idempotent.
func (m *Map) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
}

// find returns the node exactly matching key, or nil.
func (n *node) find(key string) *node {
	cur := n
	for len(key) > 0 {
		child, _ := cur.findChild(key[0])
		if child == nil || !strings.HasPrefix(key, child.label) {
			return nil
		}
		key = key[len(child.label):]
		cur = child
	}
	return cur
}

// findChild returns the child whose label starts with first and its index,
// or nil and the insertion index keeping children sorted.
func (n *node) findChild(first byte) (*node, int) {
	i := sort.Search(len(n.children), func(i int) bool {
		return n.children[i].label[0] >= first
	})
	if i < len(n.children) && n.children[i].label[0] == first {
		return n.children[i], i
	}
	return nil, i
}

func (n *node) insertChildAt(i int, c *node) {
	n.children = append(n.children, nil)
	copy(n.children[i+1:], n.children[i:])
	n.children[i] = c
}

func (n *node) mergeWithOnlyChild() {
	c := n.children[0]
	n.label += c.label
	n.value = c.value
	n.hasValue = c.hasValue
	n.children = c.children
}

// collect appends all entries in the subtree to out in lexicographic order;
// children are sorted, so a depth-first walk yields sorted keys.
func collect(n *node, key string, out *[]Entry) {
	if n.hasValue {
		*out = append(*out, Entry{Key: key, Value: n.value})
	}
	for _, c := range n.children {
		collect(c, key+c.label, out)
	}
}

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
