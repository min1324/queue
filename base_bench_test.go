package queue_test

import (
	"fmt"
	"reflect"
	"sync/atomic"
	"testing"

	"github.com/min1324/queue"
)

// ============================================================
// 基准测试框架
// ============================================================

// bench 定义基准测试的结构
type bench struct {
	setup func(b *testing.B, m Interface)
	perG  func(b *testing.B, pb *testing.PB, id int, m Interface)
}

// benchMap 对多种队列实现运行相同的基准测试
func benchMap(b *testing.B, bench bench) {
	implementations := [...]Interface{
		&queue.DoubleRing{},
		&queue.LockFree{},

		// &queue.SPSC{},
		// &queue.LockQueue{},
	}

	for _, m := range implementations {
		b.Run(fmt.Sprintf("%T", m), func(b *testing.B) {
			// 创建新的队列实例，避免状态污染
			m = reflect.New(reflect.TypeOf(m).Elem()).Interface().(Interface)

			if bench.setup != nil {
				bench.setup(b, m)
			}

			b.ResetTimer()
			var idCounter int64
			b.RunParallel(func(pb *testing.PB) {
				id := int(atomic.AddInt64(&idCounter, 1) - 1)
				bench.perG(b, pb, id, m)
			})
		})
	}
}

// ============================================================
// Push 基准测试
// ============================================================

// BenchmarkPushFull 满队列场景下的 Push 性能
func BenchmarkPushFull(b *testing.B) {
	benchMap(b, bench{
		setup: func(_ *testing.B, m Interface) {
			m.Init(64)
			for i := 0; i < 64; i++ {
				m.Push(i)
			}
		},
		perG: func(b *testing.B, pb *testing.PB, id int, m Interface) {
			i := id * b.N
			for ; pb.Next(); i++ {
				m.Push(i)
			}
		},
	})
}

// BenchmarkPushEmpty 空队列场景下的 Push 性能
func BenchmarkPushEmpty(b *testing.B) {
	benchMap(b, bench{
		setup: func(_ *testing.B, m Interface) {
			m.Init(prevSize)
		},
		perG: func(b *testing.B, pb *testing.PB, id int, m Interface) {
			i := id * b.N
			for ; pb.Next(); i++ {
				m.Push(i)
			}
		},
	})
}

// ============================================================
// Pop 基准测试
// ============================================================

// BenchmarkPopEmpty 空队列场景下的 Pop 性能
func BenchmarkPopEmpty(b *testing.B) {
	benchMap(b, bench{
		setup: func(_ *testing.B, m Interface) {
			m.Init(0)
		},
		perG: func(b *testing.B, pb *testing.PB, id int, m Interface) {
			for pb.Next() {
				m.Pop()
			}
		},
	})
}

// BenchmarkPopFull 满队列场景下的 Pop 性能
func BenchmarkPopFull(b *testing.B) {
	benchMap(b, bench{
		setup: func(_ *testing.B, m Interface) {
			m.Init(prevSize)
			for i := 0; i < prevSize; i++ {
				m.Push(i)
			}
		},
		perG: func(b *testing.B, pb *testing.PB, id int, m Interface) {
			for pb.Next() {
				m.Pop()
			}
		},
	})
}

// ============================================================
// 混合操作基准测试
// ============================================================

// BenchmarkBalance 均衡的 Push/Pop 操作性能
func BenchmarkBalanceShortCap(b *testing.B) {
	benchMap(b, bench{
		setup: func(_ *testing.B, m Interface) {
			m.Init(1 << 5)
			// 预填充少量元素，确保 Pop 不会总是失败
			for i := 0; i < 2; i++ {
				m.Push(i)
			}
		},
		perG: func(b *testing.B, pb *testing.PB, id int, m Interface) {
			i := id * b.N
			for ; pb.Next(); i++ {
				m.Pop()
				m.Push(i)
			}
		},
	})
}

// BenchmarkBalance 均衡的 Push/Pop 操作性能
func BenchmarkBalance(b *testing.B) {
	benchMap(b, bench{
		setup: func(_ *testing.B, m Interface) {
			m.Init(prevSize)
			// 预填充少量元素，确保 Pop 不会总是失败
			for i := 0; i < 2; i++ {
				m.Push(i)
			}
		},
		perG: func(b *testing.B, pb *testing.PB, id int, m Interface) {
			i := id * b.N
			for ; pb.Next(); i++ {
				m.Pop()
				m.Push(i)
			}
		},
	})
}

// BenchmarkPushHeavy Push 密集型操作性能（3:1 比例）
func BenchmarkPushHeavy(b *testing.B) {
	benchMap(b, bench{
		setup: func(_ *testing.B, m Interface) {
			m.Init(prevSize)
			// 预填充少量元素，确保 Pop 不会总是失败
			for i := 0; i < 2; i++ {
				m.Push(i)
			}
		},
		perG: func(b *testing.B, pb *testing.PB, id int, m Interface) {
			i := id * b.N
			for ; pb.Next(); i++ {
				m.Push(i)
				m.Push(i + 1)
				m.Push(i + 2)
				m.Pop()
			}
		},
	})
}

// BenchmarkPopHeavy Pop 密集型操作性能（1:3 比例）
func BenchmarkPopHeavy(b *testing.B) {
	benchMap(b, bench{
		setup: func(_ *testing.B, m Interface) {
			m.Init(prevSize)
			// 预填充大量元素
			for i := 0; i < prevSize; i++ {
				m.Push(i)
			}
		},
		perG: func(b *testing.B, pb *testing.PB, id int, m Interface) {
			i := id * b.N
			for ; pb.Next(); i++ {
				m.Push(i)
				m.Pop()
				m.Pop()
				m.Pop()
			}
		},
	})
}
