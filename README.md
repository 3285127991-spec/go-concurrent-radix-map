# go-concurrent-radix-map

一个零依赖的 Go 并发 Radix Map（压缩 radix trie），把非空 string key 映射到
string value。只包含库、一个演示 CLI 和自动化测试，可在 Windows 本地直接运行。

## 功能

- 压缩 radix trie：边存字符串片段，部分公共前缀触发节点 split，删除后自动 merge，保持树规范化
- 并发安全：`sync.RWMutex` 保护，多 goroutine 可同时读写
- `LongestPrefix` / `Prefix` 正确处理查询结束在压缩边中间的情况
- `Prefix` / `Snapshot` 按 key 字典序返回
- `Close` 后拒绝写入（`Put`/`Delete`），读取照常，`Close` 可重复调用

## API

| 方法 | 说明 |
| --- | --- |
| `New() *Map` | 创建空 Map |
| `Put(key, value string) error` | 插入或更新；已存在 key 只更新 value；空 key 返回 `ErrEmptyKey`，Close 后返回 `ErrClosed` |
| `Get(key string) (string, bool)` | 查询单个 key |
| `Delete(key string) bool` | 删除并规范化（merge），返回是否存在 |
| `LongestPrefix(input string) (key, value string, found bool)` | 返回作为 input 前缀的最长 key 条目 |
| `Prefix(prefix string) []Entry` | 返回所有以 prefix 开头的条目，按 key 字典序 |
| `Snapshot() []Entry` | 返回全部条目的字典序独立副本 |
| `Len() int` | 返回 key 数量 |
| `Close()` | 关闭写入，可重复调用；读取方法不受影响 |

## 运行

需要本机安装 Go（Windows 下 `-race` 需要 gcc，如 MSYS2/MinGW）。

```sh
go test -race ./...     # 运行全部测试（含竞态检测）
go run ./cmd/radixdemo  # 演示 split / 更新 / LongestPrefix / Prefix / merge / Close
```

## 结构

- `radix.go`：压缩 radix trie 核心实现
- `radix_test.go`：8 个测试（split、更新、LongestPrefix、Prefix、Delete/merge、
  边界前缀、Snapshot/Close、基础并发）
- `cmd/radixdemo/main.go`：演示 CLI

## 设计要点

- 每个节点保存一条压缩边标签 `label`，子节点按首字节有序，查找用二分
- 插入时公共前缀只匹配到边中间就 split 出新的内部节点
- 删除后：无值叶子直接摘除；无值且只剩一个子节点的节点与子节点合并
- 子节点有序使深度优先遍历天然得到字典序，`Prefix`/`Snapshot` 无需再排序
- 数据只存在于内存：无持久化、无通配符、无网络、无数据库
