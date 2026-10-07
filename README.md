# radixmap

A concurrency-safe string-to-string map for Go, backed by a **compressed radix
trie** (not a per-character trie, never a full key scan). Pure standard
library, no third-party dependencies, no persistence, no network.

## Features

- Compressed edges: nodes store path segments, and inserts that share only a
  partial prefix **split** the edge; deletes **merge** value-less
  single-child nodes back so the trie stays normalized.
- Safe for concurrent use: a single `sync.RWMutex` guards all operations
  (verified with `go test -race`).
- Prefix queries handle queries that end in the middle of a compressed edge.

## API

```go
m := radixmap.New()

err := m.Put("rubicon", "v")     // ErrEmptyKey for "", ErrClosed after Close
value, ok := m.Get("rubicon")
deleted, err := m.Delete("rubicon")
entry, ok := m.LongestPrefix("rubicons") // longest stored key prefixing the input
entries := m.Prefix("rub")       // all matches, lexicographic order
all := m.Snapshot()              // independent sorted copy of every entry
n := m.Len()
err = m.Close()                  // idempotent; writes now fail with ErrClosed,
                                 // reads keep working
```

`Put` on an existing key only updates its value. `Entry` is a
`{Key, Value string}` pair.

## Layout

- `radix.go` — the library (package `radixmap`).
- `cmd/radixdemo/main.go` — CLI demo: edge splits, value update,
  `LongestPrefix`, `Prefix`, merge after delete, reads after `Close`.
- `radix_test.go` — eight tests: splits, updates, longest prefix, prefix
  listing, delete/merge, prefix boundaries, snapshot/close, concurrency.

## Run

```sh
go test -race ./...
go run ./cmd/radixdemo
```

