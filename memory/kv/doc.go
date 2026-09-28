// Package kv 提供 memory.KVStore 的可选后端：LocalFileKV（内存 map ＋ 单张原子快照，
// 只用于跨进程验证，无 fsync、不保证掉电安全）、RustVikingClient（封装 rustviking CLI
// 的 JSON 契约，range 由公共前缀扫描模拟）与 MockRustVikingClient（测试替身，扫描同样
// 按字典序）。
//
// 契约: docs/wiki/memory/memory-architecture.md#local-file-kv
package kv
