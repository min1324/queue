//go:build tinygo

package queue

import "sync"

// 不支持 go:linkname 的运行时（如 TinyGo）的回退实现。
// 使用基于互斥锁的方式代替 procPin/procUnpin，速度较慢但可移植。
//
// 在标准 Go 运行时中，procPin 可防止协程在无锁队列的关键
// CAS+Store 操作序列中被抢占。由于 TinyGo 不支持 go:linkname
// 来访问这些内部运行时函数，我们使用互斥锁作为替代，
// 以确保这些关键区段的互斥访问。
//
// 性能说明：此回退实现相比原生 procPin 实现降低了并发性，
// 因为它通过单个互斥锁串行化了所有 Push/Pop 操作。
// 但它保证了正确性，对于需要 TinyGo 的场景（如 WebAssembly）已足够。

var fallbackMu sync.Mutex

// runtime_procPin 锁定回退互斥锁以防止关键区段的并发访问，
// 模拟协程绑定行为。
func runtime_procPin() {
	fallbackMu.Lock()
}

// runtime_procUnpin 解锁回退互斥锁，允许其他协程
// 继续执行其关键区段。
func runtime_procUnpin() {
	fallbackMu.Unlock()
}
