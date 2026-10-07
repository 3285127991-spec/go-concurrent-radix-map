package radixmap

import (
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
)

func mustPut(t *testing.T, m *Map, key, value string) {
	t.Helper()
	if err := m.Put(key, value); err != nil {
		t.Fatalf("Put(%q): %v", key, err)
	}
}

func keysOf(entries []Entry) []string {
	if len(entries) == 0 {
		return nil
	}
	keys := make([]string, len(entries))
	for i, e := range entries {
		keys[i] = e.Key
	}
	return keys
}

// checkNormalized fails the test if any non-root node has no value and
// exactly one child, which would mean the trie is not normalized.
func checkNormalized(t *testing.T, m *Map) {
	t.Helper()
	var walk func(n *node, isRoot bool)
	walk = func(n *node, isRoot bool) {
		if !isRoot && !n.hasValue && len(n.children) == 1 {
			t.Errorf("node %q has no value and a single child (not merged)", n.segment)
		}
		for _, c := range n.children {
			walk(c, false)
		}
	}
	walk(m.root, true)
}

func TestPutSplitAndGet(t *testing.T) {
	m := New()
	// These inserts force several compressed-edge splits.
	pairs := []Entry{
		{"romane", "1"}, {"romanus", "2"}, {"rubens", "3"},
		{"ruber", "4"}, {"rubicon", "5"}, {"rubicundus", "6"},
	}
	for _, p := range pairs {
		mustPut(t, m, p.Key, p.Value)
	}
	if m.Len() != len(pairs) {
		t.Fatalf("Len = %d, want %d", m.Len(), len(pairs))
	}
	for _, p := range pairs {
		if v, ok := m.Get(p.Key); !ok || v != p.Value {
			t.Errorf("Get(%q) = %q, %v; want %q, true", p.Key, v, ok, p.Value)
		}
	}
	if _, ok := m.Get("rom"); ok {
		t.Error("Get(rom) found a split node without a value")
	}
	checkNormalized(t, m)
}

func TestPutUpdateAndEmptyKey(t *testing.T) {
	m := New()
	mustPut(t, m, "go", "old")
	mustPut(t, m, "go", "new")
	if m.Len() != 1 {
		t.Fatalf("Len = %d, want 1 after updating the same key", m.Len())
	}
	if v, _ := m.Get("go"); v != "new" {
		t.Errorf("Get(go) = %q, want %q", v, "new")
	}
	if err := m.Put("", "x"); !errors.Is(err, ErrEmptyKey) {
		t.Errorf("Put empty key: err = %v, want ErrEmptyKey", err)
	}
	if _, ok := m.Get(""); ok {
		t.Error("Get of empty key should not find anything")
	}
}

func TestLongestPrefix(t *testing.T) {
	m := New()
	for _, k := range []string{"romane", "romanus", "rub", "rubicon"} {
		mustPut(t, m, k, "v-"+k)
	}
	cases := []struct {
		input   string
		wantKey string
		found   bool
	}{
		{"romanus", "romanus", true},    // exact match
		{"romanusXYZ", "romanus", true}, // input extends past the key
		{"rubicons", "rubicon", true},   // input extends past a leaf
		{"rubic", "rub", true},          // input ends mid-edge: "rubicon" is longer
		{"rom", "", false},              // ends mid-edge before any value
		{"unknown", "", false},          // no shared prefix
		{"", "", false},                 // empty input
	}
	for _, c := range cases {
		e, ok := m.LongestPrefix(c.input)
		if ok != c.found || (ok && e.Key != c.wantKey) {
			t.Errorf("LongestPrefix(%q) = %q, %v; want %q, %v", c.input, e.Key, ok, c.wantKey, c.found)
		}
		if ok && e.Value != "v-"+e.Key {
			t.Errorf("LongestPrefix(%q) value = %q, want %q", c.input, e.Value, "v-"+e.Key)
		}
	}
}

func TestPrefix(t *testing.T) {
	m := New()
	for _, k := range []string{"rubicundus", "ruber", "romane", "rubens", "romanus", "rubicon"} {
		mustPut(t, m, k, "v")
	}
	cases := []struct {
		prefix string
		want   []string
	}{
		{"rub", []string{"rubens", "ruber", "rubicon", "rubicundus"}},
		{"rubi", []string{"rubicon", "rubicundus"}}, // query ends mid-edge
		{"rubicon", []string{"rubicon"}},            // prefix is a full key
		{"roman", []string{"romane", "romanus"}},
		{"r", []string{"romane", "romanus", "rubens", "ruber", "rubicon", "rubicundus"}},
		{"", []string{"romane", "romanus", "rubens", "ruber", "rubicon", "rubicundus"}},
		{"nope", nil},
		{"rubiconx", nil}, // prefix longer than any key below
	}
	for _, c := range cases {
		got := keysOf(m.Prefix(c.prefix))
		if !reflect.DeepEqual(got, c.want) && !(got == nil && c.want == nil) {
			t.Errorf("Prefix(%q) keys = %v, want %v", c.prefix, got, c.want)
		}
	}
}

func TestDeleteAndMerge(t *testing.T) {
	m := New()
	for _, k := range []string{"romane", "romanus", "rubens", "ruber", "rubicon", "rubicundus"} {
		mustPut(t, m, k, "v")
	}

	// Removing a leaf lets its value-less parent merge with its only child.
	if ok, _ := m.Delete("rubens"); !ok {
		t.Fatal("Delete(rubens) = false, want true")
	}
	checkNormalized(t, m)
	if _, ok := m.Get("rubens"); ok {
		t.Error("Get(rubens) after delete should miss")
	}
	for _, k := range []string{"ruber", "rubicon", "rubicundus"} {
		if _, ok := m.Get(k); !ok {
			t.Errorf("Get(%q) lost after merge", k)
		}
	}

	// Delete the rest and confirm the trie empties out completely.
	for _, k := range []string{"romane", "romanus", "ruber", "rubicon", "rubicundus"} {
		if ok, _ := m.Delete(k); !ok {
			t.Fatalf("Delete(%q) = false, want true", k)
		}
	}
	checkNormalized(t, m)
	if m.Len() != 0 {
		t.Fatalf("Len = %d, want 0", m.Len())
	}
	if got := m.Snapshot(); len(got) != 0 {
		t.Errorf("Snapshot after deleting everything = %v, want empty", got)
	}
	if ok, _ := m.Delete("rubens"); ok {
		t.Error("Delete of missing key = true, want false")
	}
	if ok, _ := m.Delete(""); ok {
		t.Error("Delete of empty key = true, want false")
	}
}

func TestPrefixBoundaries(t *testing.T) {
	m := New()
	// Keys where each is a strict prefix of the next: inserts must split
	// exactly at the end of the shorter key.
	for _, k := range []string{"abc", "ab", "a"} {
		mustPut(t, m, k, "v-"+k)
	}
	if m.Len() != 3 {
		t.Fatalf("Len = %d, want 3", m.Len())
	}
	for _, k := range []string{"a", "ab", "abc"} {
		if v, ok := m.Get(k); !ok || v != "v-"+k {
			t.Errorf("Get(%q) = %q, %v", k, v, ok)
		}
	}
	if got := keysOf(m.Prefix("ab")); !reflect.DeepEqual(got, []string{"ab", "abc"}) {
		t.Errorf("Prefix(ab) = %v, want [ab abc]", got)
	}
	if e, ok := m.LongestPrefix("abcd"); !ok || e.Key != "abc" {
		t.Errorf("LongestPrefix(abcd) = %q, %v; want abc, true", e.Key, ok)
	}

	// Deleting the middle key merges the remaining edge back together.
	if ok, _ := m.Delete("ab"); !ok {
		t.Fatal("Delete(ab) = false, want true")
	}
	checkNormalized(t, m)
	if _, ok := m.Get("ab"); ok {
		t.Error("Get(ab) after delete should miss")
	}
	if got := keysOf(m.Snapshot()); !reflect.DeepEqual(got, []string{"a", "abc"}) {
		t.Errorf("Snapshot = %v, want [a abc]", got)
	}
}

func TestSnapshotAndClose(t *testing.T) {
	m := New()
	for _, k := range []string{"delta", "alpha", "charlie", "bravo"} {
		mustPut(t, m, k, "v-"+k)
	}
	snap := m.Snapshot()
	wantKeys := []string{"alpha", "bravo", "charlie", "delta"}
	if !reflect.DeepEqual(keysOf(snap), wantKeys) {
		t.Fatalf("Snapshot keys = %v, want %v", keysOf(snap), wantKeys)
	}
	// The snapshot is independent of later mutations.
	mustPut(t, m, "echo", "v-echo")
	if _, err := m.Delete("alpha"); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(keysOf(snap), wantKeys) || snap[0].Value != "v-alpha" {
		t.Errorf("Snapshot changed after mutations: %v", snap)
	}

	if err := m.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := m.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
	if err := m.Put("x", "y"); !errors.Is(err, ErrClosed) {
		t.Errorf("Put after Close: err = %v, want ErrClosed", err)
	}
	if _, err := m.Delete("bravo"); !errors.Is(err, ErrClosed) {
		t.Errorf("Delete after Close: err = %v, want ErrClosed", err)
	}
	// Reads keep working after Close.
	if v, ok := m.Get("bravo"); !ok || v != "v-bravo" {
		t.Errorf("Get(bravo) after Close = %q, %v", v, ok)
	}
	if got := keysOf(m.Prefix("c")); !reflect.DeepEqual(got, []string{"charlie"}) {
		t.Errorf("Prefix(c) after Close = %v", got)
	}
	if e, ok := m.LongestPrefix("charlie1"); !ok || e.Key != "charlie" {
		t.Errorf("LongestPrefix after Close = %q, %v", e.Key, ok)
	}
	if got := keysOf(m.Snapshot()); !reflect.DeepEqual(got, []string{"bravo", "charlie", "delta", "echo"}) {
		t.Errorf("Snapshot after Close = %v", got)
	}
	if m.Len() != 4 {
		t.Errorf("Len after Close = %d, want 4", m.Len())
	}
}

func TestConcurrentAccess(t *testing.T) {
	m := New()
	const writers = 8
	const perWriter = 100

	var readersWg, writersWg sync.WaitGroup
	stop := make(chan struct{})
	// Readers hammer the read APIs while writers run.
	for r := 0; r < 4; r++ {
		readersWg.Add(1)
		go func() {
			defer readersWg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				m.Get("w03/007")
				m.Prefix("w0")
				m.LongestPrefix("w05/050!")
				m.Snapshot()
				m.Len()
			}
		}()
	}
	for w := 0; w < writers; w++ {
		writersWg.Add(1)
		go func(w int) {
			defer writersWg.Done()
			for i := 0; i < perWriter; i++ {
				key := fmt.Sprintf("w%02d/%03d", w, i)
				if err := m.Put(key, fmt.Sprintf("v%d", i)); err != nil {
					t.Errorf("Put(%q): %v", key, err)
				}
			}
		}(w)
	}
	// Concurrent puts of the same key must not duplicate it.
	for d := 0; d < 16; d++ {
		writersWg.Add(1)
		go func(d int) {
			defer writersWg.Done()
			if err := m.Put("shared", fmt.Sprintf("d%d", d)); err != nil {
				t.Errorf("Put(shared): %v", err)
			}
		}(d)
	}

	writersWg.Wait()
	close(stop)
	readersWg.Wait()

	// Verify all disjoint keys landed exactly once.
	if got, want := m.Len(), writers*perWriter+1; got != want {
		t.Fatalf("Len = %d, want %d", got, want)
	}
	for w := 0; w < writers; w++ {
		for i := 0; i < perWriter; i++ {
			key := fmt.Sprintf("w%02d/%03d", w, i)
			if _, ok := m.Get(key); !ok {
				t.Fatalf("Get(%q) missing after concurrent writes", key)
			}
		}
	}
	if _, ok := m.Get("shared"); !ok {
		t.Fatal("Get(shared) missing")
	}

	// Concurrent deletes of disjoint keys.
	var dwg sync.WaitGroup
	for w := 0; w < writers; w++ {
		dwg.Add(1)
		go func(w int) {
			defer dwg.Done()
			for i := 0; i < perWriter; i += 2 {
				if ok, err := m.Delete(fmt.Sprintf("w%02d/%03d", w, i)); err != nil || !ok {
					t.Errorf("Delete(w%02d/%03d) = %v, %v", w, i, ok, err)
				}
			}
		}(w)
	}
	dwg.Wait()
	if got, want := m.Len(), writers*perWriter/2+1; got != want {
		t.Fatalf("Len after deletes = %d, want %d", got, want)
	}
	checkNormalized(t, m)
}
