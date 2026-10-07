// Command radixdemo demonstrates the radixmap library: node splits on
// insert, value updates, LongestPrefix, Prefix, node merges after Delete,
// and read access after Close.
package main

import (
	"fmt"
	"os"

	"radixmap"
)

func main() {
	// pf formats a single line and prints it with a trailing newline.
	pf := func(format string, args ...any) {
		fmt.Println(fmt.Sprintf(format, args...))
	}

	m := radixmap.New()

	put := func(key, value string) {
		if err := m.Put(key, value); err != nil {
			fmt.Println("Put failed:", err)
			os.Exit(1)
		}
		pf("Put(%q, %q) ok, Len=%d", key, value, m.Len())
	}

	fmt.Println("== 1. Insert keys (triggers node splits) ==")
	put("romane", "1")
	put("romanus", "2") // splits edge "romane" -> "roman" + {"e", "us"}
	put("romulus", "3") // splits node "roman"  -> "rom" + {"an", "ulus"}
	put("rubens", "4")  // splits node "rom"    -> "r" + {"om", "ubens"}
	put("ruber", "5")   // splits edge "ubens"  -> "ube" + {"ns", "r"}
	put("rubicon", "6") // splits node "ube"    -> "ub" + {"e", "icon"}

	fmt.Println()
	fmt.Println("== 2. Update an existing key ==")
	put("romane", "updated") // value replaced, Len unchanged
	if v, ok := m.Get("romane"); ok {
		pf("Get(%q) = %q", "romane", v)
	}

	fmt.Println()
	fmt.Println("== 3. LongestPrefix ==")
	for _, input := range []string{"romanesque", "rubiconx", "rom", "nope"} {
		if k, v, ok := m.LongestPrefix(input); ok {
			pf("LongestPrefix(%q) = (%q, %q)", input, k, v)
		} else {
			pf("LongestPrefix(%q) = not found", input)
		}
	}

	fmt.Println()
	fmt.Println("== 4. Prefix (lexicographic order) ==")
	for _, p := range []string{"rom", "rub", ""} {
		pf("Prefix(%q):", p)
		for _, e := range m.Prefix(p) {
			pf("  %s = %s", e.Key, e.Value)
		}
	}

	fmt.Println()
	fmt.Println("== 5. Delete keys (triggers node merges) ==")
	for _, k := range []string{"rubens", "ruber", "rubicon"} {
		pf("Delete(%q) = %v, Len=%d", k, m.Delete(k), m.Len())
	}
	// The whole "rub" branch collapsed; only rom* keys remain and the
	// valueless single-child nodes were merged away.
	fmt.Println("Snapshot after deletes:")
	for _, e := range m.Snapshot() {
		pf("  %s = %s", e.Key, e.Value)
	}

	fmt.Println()
	fmt.Println("== 6. Close ==")
	m.Close()
	m.Close() // idempotent
	if err := m.Put("new", "x"); err != nil {
		fmt.Println("Put after Close rejected:", err)
	}
	if !m.Delete("romane") {
		fmt.Println("Delete after Close rejected")
	}
	if v, ok := m.Get("romane"); ok {
		pf("Get after Close still works: Get(%q) = %q", "romane", v)
	}
	pf("Snapshot after Close: %d entries, Len=%d", len(m.Snapshot()), m.Len())
}
