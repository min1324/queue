package queue_test

import (
	"fmt"
	"reflect"
	"sync/atomic"
	"testing"

	"github.com/min1324/queue"
)

type bench struct {
	setup func(*testing.B, Interface)
	perG  func(b *testing.B, pb *testing.PB, i int, m Interface)
}

func benchMap(b *testing.B, bench bench) {
	for _, m := range [...]Interface{
		&queue.Queue{},
		&DRQueue{},
	} {
		b.Run(fmt.Sprintf("%T", m), func(b *testing.B) {
			m = reflect.New(reflect.TypeOf(m).Elem()).Interface().(Interface)

			// setup
			if bench.setup != nil {
				if v, ok := m.(*queue.Queue); ok {
					v.OnceInit(prevEnQueueSize)
				}
				if v, ok := m.(*DRQueue); ok {
					v.OnceInit(prevEnQueueSize)
				}
				bench.setup(b, m)
			}

			b.ResetTimer()

			var i int64
			b.RunParallel(func(pb *testing.PB) {
				id := int(atomic.AddInt64(&i, 1) - 1)
				bench.perG(b, pb, (id * b.N), m)
			})
			// free
		})
	}
}

func BenchmarkEnQueue(b *testing.B) {
	benchMap(b, bench{
		setup: func(_ *testing.B, m Interface) {
		},

		perG: func(b *testing.B, pb *testing.PB, i int, m Interface) {
			for ; pb.Next(); i++ {
				m.EnQueue(i)
			}
		},
	})
}

func BenchmarkDeQueue(b *testing.B) {
	// 由于预存的数量<出队数量，无法准确测试dequeue
	const prevsize = 1 << 20
	benchMap(b, bench{
		setup: func(b *testing.B, m Interface) {
			for i := 0; i < prevsize; i++ {
				m.EnQueue(i)
			}
		},

		perG: func(b *testing.B, pb *testing.PB, i int, m Interface) {
			for ; pb.Next(); i++ {
				m.DeQueue()
			}
		},
	})
}

func BenchmarkBalance(b *testing.B) {

	benchMap(b, bench{
		setup: func(_ *testing.B, m Interface) {
			for i := 0; i < prevEnQueueSize; i++ {
				m.EnQueue(i)
			}
		},

		perG: func(b *testing.B, pb *testing.PB, i int, m Interface) {
			for ; pb.Next(); i++ {
				m.EnQueue(i)
				m.DeQueue()
			}
		},
	})
}

func BenchmarkInterlace(b *testing.B) {

	benchMap(b, bench{
		setup: func(_ *testing.B, m Interface) {
			for i := 0; i < prevEnQueueSize; i++ {
				m.EnQueue(i)
			}
		},

		perG: func(b *testing.B, pb *testing.PB, i int, m Interface) {
			j := 0
			for ; pb.Next(); i++ {
				j += (i & 1)
				if j&1 == 0 {
					m.EnQueue(i)
				} else {
					m.DeQueue()
				}
			}
		},
	})
}
