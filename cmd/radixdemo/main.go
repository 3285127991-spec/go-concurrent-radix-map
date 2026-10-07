// Command radixdemo demonstrates the radixmap library: node splits, value
// updates, longest-prefix and prefix queries, node merges after deletes,
// and read access after Close.
package main

import (
	"errors"
	"fmt"

	"radixmap"
)

func printEntries(label string, entries []radixmap.Entry) {
	fmt.Println(label)
	if len(entries) == 0 {
		fmt.Println("  (none)")
		return
	}
	for _, e := range entries {
		fmt.Printf("  %s => %s\n", e.Key, e.Value)
	}
}

func main() {
	m := radixmap.New()

	fmt.Println("== 1. Put: inserting keys forces compressed-edge splits ==")
	// "romane" then "romanus" split the shared "roman" prefix; the "rub*"
	// keys split repeatedly ("r" -> "ub" -> "e"/"ic" -> ...).
	keys := []string{"romane", "romanus", "rubens", "ruber", "rubicon", "rubicundus"}
	for i, k := range keys {
		if err := m.Put(k, fmt.Sprintf("v%d", i)); err != nil {
			panic(err)
		}
		fmt.Printf("  Put(%q) ok, Len=%d\n", k, m.Len())
	}

	fmt.Println("\n== 2. Put on an existing key only updates the value ==")
	if err := m.Put("romane", "updated"); err != nil {
		panic(err)
	}
	v, _ := m.Get("romane")
	fmt.Printf("  Get(romane) = %q, Len still %d\n", v, m.Len())

	fmt.Println("\n== 3. LongestPrefix ==")
	for _, in := range []string{"romanusXYZ", "rubic", "rubicon9", "unknown"} {
		if e, ok := m.LongestPrefix(in); ok {
			fmt.Printf("  LongestPrefix(%q) = %q => %q\n", in, e.Key, e.Value)
		} else {
			fmt.Printf("  LongestPrefix(%q) = not found\n", in)
		}
	}

	fmt.Println("\n== 4. Prefix returns matches in lexicographic order ==")
	printEntries("  Prefix(rub):", m.Prefix("rub"))
	printEntries("  Prefix(rubi)  (query ends mid-edge):", m.Prefix("rubi"))
	printEntries("  Prefix(nope):", m.Prefix("nope"))

	fmt.Println("\n== 5. Delete merges value-less single-child nodes back ==")
	if ok, err := m.Delete("rubens"); err != nil {
		panic(err)
	} else {
		fmt.Printf("  Delete(rubens) = %v, Len=%d\n", ok, m.Len())
	}
	printEntries("  Snapshot after delete:", m.Snapshot())

	fmt.Println("\n== 6. Close rejects writes but keeps reads ==")
	if err := m.Close(); err != nil {
		panic(err)
	}
	if err := m.Close(); err != nil { // idempotent
		panic(err)
	}
	if err := m.Put("new", "x"); errors.Is(err, radixmap.ErrClosed) {
		fmt.Println("  Put after Close: rejected with ErrClosed")
	}
	if _, err := m.Delete("ruber"); errors.Is(err, radixmap.ErrClosed) {
		fmt.Println("  Delete after Close: rejected with ErrClosed")
	}
	if v, ok := m.Get("ruber"); ok {
		fmt.Printf("  Get(ruber) after Close = %q\n", v)
	}
	fmt.Printf("  Len after Close = %d\n", m.Len())
	printEntries("  Snapshot after Close:", m.Snapshot())
}
