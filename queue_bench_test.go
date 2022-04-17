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
		// &queue.LF{},
		&queue.LockFree{},
		&DRQueue{},
	} {
		b.Run(fmt.Sprintf("%T", m), func(b *testing.B) {
			m = reflect.New(reflect.TypeOf(m).Elem()).Interface().(Interface)
			m.Init(prevSize)
			// setup

			if bench.setup != nil {
				bench.setup(b, m)
			}
			b.ResetTimer()
			var i int64
			b.RunParallel(func(pb *testing.PB) {
				id := int(atomic.AddInt64(&i, 1) - 1)
				bench.perG(b, pb, (id * b.N), m)
			})
		})
	}
}

func BenchmarkFull(b *testing.B) {
	benchMap(b, bench{
		setup: func(_ *testing.B, m Interface) {
			// if _, ok := m.(*queue.LLQueue); ok {
			// 	b.Skip("LLQueue has quadratic running time.")
			// }
			for i := 0; i < prevSize; i++ {
				m.Push(i)
			}
		},

		perG: func(b *testing.B, pb *testing.PB, i int, m Interface) {
			for ; pb.Next(); i++ {
				m.Push(i)
			}
		},
	})
}

func BenchmarkEmpty(b *testing.B) {
	benchMap(b, bench{
		setup: func(_ *testing.B, m Interface) {
		},

		perG: func(b *testing.B, pb *testing.PB, i int, m Interface) {
			for ; pb.Next(); i++ {
				m.Pop()
			}
		},
	})
}

func BenchmarkBalance(b *testing.B) {

	benchMap(b, bench{
		setup: func(_ *testing.B, m Interface) {
			for i := 0; i < 2; i++ {
				m.Push(i)
			}
		},

		perG: func(b *testing.B, pb *testing.PB, i int, m Interface) {
			for ; pb.Next(); i++ {
				m.Pop()
				m.Push(i)
			}
		},
	})
}
