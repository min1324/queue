//go:build !tinygo

package queue

import _ "unsafe" // go:linkname 需要

// 此文件通过 go:linkname 提供运行时 procPin/procUnpin 函数。
// 这些是 Go 运行时的内部函数，用于在无锁队列的关键
// CAS+Store 操作序列中防止协程被抢占。
//
// 兼容性说明：
//   - 这些函数自 Go 1.0 起存在于标准 Go（gc）运行时中
//   - 它们在替代运行时（tinygo 等）中不可用
//   - 如果 Go 在未来版本中移除这些函数，此文件将无法编译，
//     从而提供早期检测机制
//   - queue.go 中的 verifyEfaceLayout() 检查提供了额外的
//     运行时安全保障，在 Init 时验证 eface 结构体布局

//go:linkname runtime_procPin runtime.procPin
func runtime_procPin()

//go:linkname runtime_procUnpin runtime.procUnpin
func runtime_procUnpin()
