package radixmap

import (
	"errors"
	"fmt"
	"sync"
	"testing"
)

func mustPut(t *testing.T, m *Map, key, value string) {
	t.Helper()
	if err := m.Put(key, value); err != nil {
		t.Fatalf("Put(%q, %q): %v", key, value, err)
	}
}

func entryKeys(entries []Entry) []string {
	keys := make([]string, len(entries))
	for i, e := range entries {
		keys[i] = e.Key
	}
	return keys
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestInsertSplit(t *testing.T) {
	m := New()
	keys := []string{"romane", "romanus", "romulus", "rubens", "ruber", "rubicon"}
	for i, k := range keys {
		mustPut(t, m, k, fmt.Sprintf("v%d", i))
	}
	if got := m.Len(); got != len(keys) {
		t.Fatalf("Len = %d, want %d", got, len(keys))
	}
	for i, k := range keys {
		if v, ok := m.Get(k); !ok || v != fmt.Sprintf("v%d", i) {
			t.Errorf("Get(%q) = %q, %v", k, v, ok)
		}
	}

	// Structural check: splits produced root -> "r" -> {"om", "ub"}.
	if len(m.root.children) != 1 || m.root.children[0].label != "r" {
		t.Fatalf("root children = %+v, want single child r", m.root.children)
	}
	r := m.root.children[0]
	if len(r.children) != 2 || r.children[0].label != "om" || r.children[1].label != "ub" {
		t.Fatalf("split structure first labels = %q, want om and ub",
			r.children[0].label)
	}

	// Re-inserting an existing key must not create a duplicate.
	mustPut(t, m, "ruber", "x")
	if got := m.Len(); got != len(keys) {
		t.Fatalf("Len after re-put = %d, want %d", got, len(keys))
	}
}

func TestPutUpdate(t *testing.T) {
	m := New()
	mustPut(t, m, "key", "v1")
	mustPut(t, m, "key", "v2")
	if v, ok := m.Get("key"); !ok || v != "v2" {
		t.Errorf("Get(key) = %q, %v; want v2, true", v, ok)
	}
	if got := m.Len(); got != 1 {
		t.Errorf("Len = %d, want 1", got)
	}

	if err := m.Put("", "x"); !errors.Is(err, ErrEmptyKey) {
		t.Errorf("Put(empty key) err = %v, want ErrEmptyKey", err)
	}
	if _, ok := m.Get(""); ok {
		t.Error("Get(empty key) found a value")
	}
	if m.Delete("") {
		t.Error("Delete(empty key) = true")
	}
}

func TestLongestPrefix(t *testing.T) {
	m := New()
	for _, k := range []string{"romane", "romanus", "romulus", "rubens", "ruber", "rubicon"} {
		mustPut(t, m, k, "v-"+k)
	}
	cases := []struct {
		input, wantKey, wantVal string
		found                   bool
	}{
		{"romanesque", "romane", "v-romane", true},
		{"romulus!", "romulus", "v-romulus", true},
		{"rubicon", "rubicon", "v-rubicon", true},
		{"rom", "", "", false},  // internal node without value
		{"ruba", "", "", false}, // diverges below a valueless node
		{"xyz", "", "", false},
		{"", "", "", false},
	}
	for _, c := range cases {
		k, v, ok := m.LongestPrefix(c.input)
		if k != c.wantKey || v != c.wantVal || ok != c.found {
			t.Errorf("LongestPrefix(%q) = (%q, %q, %v), want (%q, %q, %v)",
				c.input, k, v, ok, c.wantKey, c.wantVal, c.found)
		}
	}

	// Input ending in the middle of a compressed edge must not match the
	// longer key below, but a shorter key on the path still wins.
	m2 := New()
	mustPut(t, m2, "abc", "1")
	mustPut(t, m2, "abcdef", "2")
	if k, v, ok := m2.LongestPrefix("abcde"); !ok || k != "abc" || v != "1" {
		t.Errorf("LongestPrefix(abcde) = (%q, %q, %v), want (abc, 1, true)", k, v, ok)
	}
}

func TestPrefix(t *testing.T) {
	m := New()
	for _, k := range []string{"romane", "romanus", "romulus", "rubens", "ruber", "rubicon"} {
		mustPut(t, m, k, "v-"+k)
	}
	cases := []struct {
		prefix string
		want   []string
	}{
		{"rom", []string{"romane", "romanus", "romulus"}},
		{"roma", []string{"romane", "romanus"}},          // ends mid-edge of "an"
		{"ro", []string{"romane", "romanus", "romulus"}}, // ends mid-edge of "om"
		{"rub", []string{"rubens", "ruber", "rubicon"}},
		{"rubicon", []string{"rubicon"}},
		{"", []string{"romane", "romanus", "romulus", "rubens", "ruber", "rubicon"}},
		{"romx", nil},
		{"rx", nil},
		{"zz", nil},
	}
	for _, c := range cases {
		got := entryKeys(m.Prefix(c.prefix))
		if !equalStrings(got, c.want) {
			t.Errorf("Prefix(%q) = %v, want %v", c.prefix, got, c.want)
		}
	}
	// Values must ride along with the keys.
	for _, e := range m.Prefix("rubicon") {
		if e.Value != "v-rubicon" {
			t.Errorf("Prefix(rubicon) value = %q", e.Value)
		}
	}
}

func TestDeleteMerge(t *testing.T) {
	m := New()
	mustPut(t, m, "abcd", "1")
	mustPut(t, m, "abxy", "2")
	// Tree: root -> "ab"(no value) -> {"cd", "xy"}.
	if !m.Delete("abcd") {
		t.Fatal("Delete(abcd) = false")
	}
	// "ab" lost a child and has no value: merge into a single "abxy" node.
	if len(m.root.children) != 1 || m.root.children[0].label != "abxy" {
		t.Fatalf("after delete root children = %+v, want single abxy", m.root.children)
	}
	if v, ok := m.Get("abxy"); !ok || v != "2" {
		t.Errorf("Get(abxy) = %q, %v", v, ok)
	}
	if _, ok := m.Get("abcd"); ok {
		t.Error("Get(abcd) still found after delete")
	}
	if got := m.Len(); got != 1 {
		t.Errorf("Len = %d, want 1", got)
	}

	// Deleting the last key empties the trie completely.
	if !m.Delete("abxy") {
		t.Fatal("Delete(abxy) = false")
	}
	if m.Len() != 0 || len(m.root.children) != 0 {
		t.Errorf("tree not empty after deleting all keys")
	}
	if m.Delete("abxy") {
		t.Error("double delete reported success")
	}

	// A node that loses its value and has one child merges with that child.
	mustPut(t, m, "abc", "1")
	mustPut(t, m, "abcdef", "2")
	if !m.Delete("abc") {
		t.Fatal("Delete(abc) = false")
	}
	if len(m.root.children) != 1 || m.root.children[0].label != "abcdef" {
		t.Fatalf("after delete root children = %+v, want single abcdef", m.root.children)
	}
	if v, ok := m.Get("abcdef"); !ok || v != "2" {
		t.Errorf("Get(abcdef) = %q, %v", v, ok)
	}
}

func TestKeyPrefixOfAnotherKey(t *testing.T) {
	m := New()
	for _, k := range []string{"a", "ab", "abc", "abd", "b"} {
		mustPut(t, m, k, "v-"+k)
	}
	if got := m.Len(); got != 5 {
		t.Fatalf("Len = %d, want 5", got)
	}
	for _, k := range []string{"a", "ab", "abc", "abd", "b"} {
		if v, ok := m.Get(k); !ok || v != "v-"+k {
			t.Errorf("Get(%q) = %q, %v", k, v, ok)
		}
	}
	// A prefix that is itself a stored key.
	if got := entryKeys(m.Prefix("ab")); !equalStrings(got, []string{"ab", "abc", "abd"}) {
		t.Errorf("Prefix(ab) = %v", got)
	}
	// Exact key is the longest prefix of itself.
	if k, _, ok := m.LongestPrefix("abc"); !ok || k != "abc" {
		t.Errorf("LongestPrefix(abc) = (%q, %v)", k, ok)
	}
	// Deleting a key that prefixes other keys keeps the subtree intact.
	if !m.Delete("ab") {
		t.Fatal("Delete(ab) = false")
	}
	if _, ok := m.Get("ab"); ok {
		t.Error("Get(ab) still found")
	}
	if got := entryKeys(m.Prefix("ab")); !equalStrings(got, []string{"abc", "abd"}) {
		t.Errorf("Prefix(ab) after delete = %v", got)
	}
	if v, ok := m.Get("a"); !ok || v != "v-a" {
		t.Errorf("Get(a) = %q, %v", v, ok)
	}
}

func TestSnapshotAndClose(t *testing.T) {
	m := New()
	mustPut(t, m, "b", "2")
	mustPut(t, m, "a", "1")

	snap := m.Snapshot()
	if got := entryKeys(snap); !equalStrings(got, []string{"a", "b"}) {
		t.Fatalf("Snapshot keys = %v", got)
	}
	// The snapshot is independent of later mutations.
	mustPut(t, m, "c", "3")
	m.Delete("a")
	if len(snap) != 2 || snap[0].Key != "a" || snap[1].Key != "b" {
		t.Errorf("snapshot changed after mutations: %v", snap)
	}

	m.Close()
	m.Close() // idempotent
	if err := m.Put("d", "4"); !errors.Is(err, ErrClosed) {
		t.Errorf("Put after Close err = %v, want ErrClosed", err)
	}
	if m.Delete("b") {
		t.Error("Delete after Close = true")
	}
	// Reads still work after Close.
	if v, ok := m.Get("b"); !ok || v != "2" {
		t.Errorf("Get(b) after Close = %q, %v", v, ok)
	}
	if got := m.Len(); got != 2 { // "a" was deleted before Close
		t.Errorf("Len after Close = %d, want 2", got)
	}
	if got := entryKeys(m.Snapshot()); !equalStrings(got, []string{"b", "c"}) {
		t.Errorf("Snapshot after Close = %v", got)
	}
	if got := entryKeys(m.Prefix("c")); !equalStrings(got, []string{"c"}) {
		t.Errorf("Prefix(c) after Close = %v", got)
	}
	if k, _, ok := m.LongestPrefix("bx"); !ok || k != "b" {
		t.Errorf("LongestPrefix(bx) after Close = (%q, %v)", k, ok)
	}
}

func TestConcurrent(t *testing.T) {
	m := New()
	const workers = 8
	const perWorker = 50
	key := func(w, i int) string { return fmt.Sprintf("w%02d-%03d", w, i) }

	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < perWorker; i++ {
				k := key(w, i)
				if err := m.Put(k, "v"); err != nil {
					t.Errorf("Put(%q): %v", k, err)
					return
				}
				if _, ok := m.Get(k); !ok {
					t.Errorf("Get(%q) missing right after Put", k)
					return
				}
				m.LongestPrefix(k)
				m.Prefix(fmt.Sprintf("w%02d", w))
				m.Snapshot()
				m.Len()
			}
		}(w)
	}
	wg.Wait()
	if got := m.Len(); got != workers*perWorker {
		t.Fatalf("Len = %d, want %d", got, workers*perWorker)
	}

	// Concurrent deletes of disjoint keys.
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < perWorker; i += 2 {
				if !m.Delete(key(w, i)) {
					t.Errorf("Delete(%q) = false", key(w, i))
				}
			}
		}(w)
	}
	wg.Wait()
	if got := m.Len(); got != workers*perWorker/2 {
		t.Fatalf("Len after deletes = %d, want %d", got, workers*perWorker/2)
	}
	// The surviving half of the keys must be intact.
	for w := 0; w < workers; w++ {
		for i := 1; i < perWorker; i += 2 {
			if _, ok := m.Get(key(w, i)); !ok {
				t.Fatalf("lost key %q", key(w, i))
			}
		}
	}
}
