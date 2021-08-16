package queue_test

import (
	"fmt"
	"math/rand"
	"reflect"
	"runtime"
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

/*
1<< 20~28
1048576		20
2097152		21
4194304		22
8388608		23
16777216	24
33554432	25
67108864	26
134217728	27
268435456	28
*/
const queueMaxSize = 1 << 24    // queue max size
const prevEnQueueSize = 1 << 20 // queue previous EnQueue

func randCall(m QInterface) {
	op := mapQueues[rand.Intn(len(mapQueues))]
	switch op {
	case opEnQueue:
		m.EnQueue(1)
	case opDeQueue:
		m.DeQueue()
	default:
		panic("invalid mapQueue")
	}
}

type bench struct {
	setup func(*testing.B, QInterface)
	perG  func(b *testing.B, pb *testing.PB, i int, m QInterface)
}

func benchMap(b *testing.B, bench bench) {
	for _, m := range [...]QInterface{
		&queue.LLQueue{},
	} {
		b.Run(fmt.Sprintf("%T", m), func(b *testing.B) {
			m = reflect.New(reflect.TypeOf(m).Elem()).Interface().(QInterface)
			m.Init()

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
			// free
			m.Init()
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

func BenchmarkMostlyEnQueue(b *testing.B) {
	const bit = 4
	const mark = 1<<bit - 1
	benchMap(b, bench{
		setup: func(_ *testing.B, m QInterface) {
			for i := 0; i < prevEnQueueSize; i++ {
				m.EnQueue(i)
			}
		},

		perG: func(b *testing.B, pb *testing.PB, i int, m QInterface) {
			for ; pb.Next(); i++ {
				if mark == 0 {
					m.DeQueue()
				} else {
					m.EnQueue(i)
				}
			}
		},
	})
}

func BenchmarkMostlyDeQueue(b *testing.B) {
	const bit = 4
	const mark = 1<<bit - 1
	benchMap(b, bench{
		setup: func(_ *testing.B, m QInterface) {
			for i := 0; i < prevEnQueueSize; i++ {
				m.EnQueue(i)
			}
		},

		perG: func(b *testing.B, pb *testing.PB, i int, m QInterface) {
			for ; pb.Next(); i++ {
				if i&mark == 0 {
					m.EnQueue(i)
				} else {
					m.DeQueue()
				}
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

func BenchmarkCollision(b *testing.B) {

	benchMap(b, bench{
		setup: func(_ *testing.B, m QInterface) {
			for i := 0; i < prevEnQueueSize; i++ {
				m.EnQueue(i)
			}
		},

		perG: func(b *testing.B, pb *testing.PB, i int, m QInterface) {
			for ; pb.Next(); i++ {
				if i&1 == 0 {
					m.EnQueue(i)
				} else {
					m.DeQueue()
				}
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
			if _, ok := m.(*UnsafeQueue); ok {
				b.Skip("UnsafeQueue can not test concurrent.")
			}
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
			if _, ok := m.(*UnsafeQueue); ok {
				b.Skip("UnsafeQueue can not test concurrent.")
			}
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
	const stackSize = 1 << 10
	rand.Seed(time.Now().Unix())

	benchMap(b, bench{
		setup: func(_ *testing.B, m QInterface) {
			if _, ok := m.(*UnsafeQueue); ok {
				b.Skip("UnsafeQueue can not test concurrent.")
			}
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

func BenchmarkConcurrentMulRand(b *testing.B) {
	rand.Seed(time.Now().Unix())

	const size = 1 << 10
	const mod = size - 1
	var random [size]int

	for i := range random {
		random[i] = rand.Intn(10) & 1
	}

	benchMap(b, bench{
		setup: func(_ *testing.B, m QInterface) {
			if _, ok := m.(*UnsafeQueue); ok {
				b.Skip("UnsafeQueue can not test concurrent.")
			}
		},
		perG: func(b *testing.B, pb *testing.PB, i int, m QInterface) {
			exit := make(chan struct{}, 1)
			var wg sync.WaitGroup
			defer func() {
				close(exit)
				wg.Wait()
				exit = nil
			}()
			for g := int64(runtime.GOMAXPROCS(0)); g > 1; g-- {
				wg.Add(1)
				go func() {
					defer wg.Done()
					var j uint64
					for {
						select {
						case <-exit:
							return
						default:
							if random[atomic.AddUint64(&j, 1)&mod] == 0 {
								m.EnQueue(j)
							} else {
								m.DeQueue()
							}
						}
					}
				}()
			}
			for ; pb.Next(); i++ {
				if random[i&mod] == 0 {
					m.EnQueue(i)
				} else {
					m.DeQueue()
				}
			}
		},
	})
}
