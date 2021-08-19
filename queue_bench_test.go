package queue_test

import (
	"fmt"
	"math/rand"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/min1324/queue"
)

type mapQueue string

const (
	opEnQueue = mapQueue("EnQueue")
	opDeQueue = mapQueue("DeQueue")
)

var mapQueues = [...]mapQueue{opEnQueue, opDeQueue}

type bench struct {
	setup func(*testing.B, QInterface)
	perG  func(b *testing.B, pb *testing.PB, i int, m QInterface)
}

func benchMap(b *testing.B, bench bench) {
	for _, m := range [...]QInterface{
		&queue.Queue{},
		&DRQueue{},
	} {
		b.Run(fmt.Sprintf("%T", m), func(b *testing.B) {
			m = reflect.New(reflect.TypeOf(m).Elem()).Interface().(QInterface)

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
		setup: func(_ *testing.B, m QInterface) {
		},

		perG: func(b *testing.B, pb *testing.PB, i int, m QInterface) {
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
		setup: func(b *testing.B, m QInterface) {
			for i := 0; i < prevsize; i++ {
				m.EnQueue(i)
			}
		},

		perG: func(b *testing.B, pb *testing.PB, i int, m QInterface) {
			for ; pb.Next(); i++ {
				m.DeQueue()
			}
		},
	})
}

func BenchmarkBalance(b *testing.B) {

	benchMap(b, bench{
		setup: func(_ *testing.B, m QInterface) {
			for i := 0; i < prevEnQueueSize; i++ {
				m.EnQueue(i)
			}
		},

		perG: func(b *testing.B, pb *testing.PB, i int, m QInterface) {
			for ; pb.Next(); i++ {
				m.EnQueue(i)
				m.DeQueue()
			}
		},
	})
}

func BenchmarkInterlace(b *testing.B) {

	benchMap(b, bench{
		setup: func(_ *testing.B, m QInterface) {
			for i := 0; i < prevEnQueueSize; i++ {
				m.EnQueue(i)
			}
		},

		perG: func(b *testing.B, pb *testing.PB, i int, m QInterface) {
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

func BenchmarkConcurrentDeQueue(b *testing.B) {

	benchMap(b, bench{
		setup: func(_ *testing.B, m QInterface) {
		},
		perG: func(b *testing.B, pb *testing.PB, i int, m QInterface) {
			var wg sync.WaitGroup
			exit := make(chan struct{}, 1)
			defer func() {
				close(exit)
				wg.Wait()
				exit = nil
			}()
			wg.Add(1)
			go func() {
				defer wg.Done()
				for {
					select {
					case <-exit:
						return
					default:
						m.EnQueue(1)
					}
				}
			}()
			for ; pb.Next(); i++ {
				m.DeQueue()
			}
		},
	})
}

func BenchmarkConcurrentEnQueue(b *testing.B) {

	benchMap(b, bench{
		setup: func(_ *testing.B, m QInterface) {
		},
		perG: func(b *testing.B, pb *testing.PB, i int, m QInterface) {
			var wg sync.WaitGroup
			exit := make(chan struct{}, 1)
			defer func() {
				close(exit)
				wg.Wait()
				exit = nil
			}()
			wg.Add(1)
			go func() {
				defer wg.Done()
				for {
					select {
					case <-exit:
						return
					default:
						m.DeQueue()
					}
				}
			}()
			for ; pb.Next(); i++ {
				m.EnQueue(1)
			}
		},
	})
}

func BenchmarkConcurrentRand(b *testing.B) {
	rand.Seed(time.Now().Unix())

	benchMap(b, bench{
		setup: func(_ *testing.B, m QInterface) {
		},
		perG: func(b *testing.B, pb *testing.PB, i int, m QInterface) {
			var wg sync.WaitGroup
			exit := make(chan struct{}, 1)
			var j uint64
			defer func() {
				close(exit)
				wg.Wait()
				exit = nil
			}()
			wg.Add(1)
			go func() {
				defer wg.Done()
				for {
					select {
					case <-exit:
						return
					default:
						if (j^9)&1 == 0 {
							m.EnQueue(j)
						} else {
							m.DeQueue()
						}
						atomic.AddUint64(&j, 1)
					}
				}
			}()
			wg.Add(1)
			go func() {
				defer wg.Done()
				for {
					select {
					case <-exit:
						return
					default:
						if (j^2)&1 == 0 {
							m.EnQueue(j)
						} else {
							m.DeQueue()
						}
						atomic.AddUint64(&j, 1)
					}
				}
			}()
			for ; pb.Next(); i++ {
				if (j^3)&1 == 0 {
					m.EnQueue(j)
				} else {
					m.DeQueue()
				}
				atomic.AddUint64(&j, 1)
			}
		},
	})
}
