package queue_test

import (
	"context"
	"fmt"
	"math/rand"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"testing/quick"
	"time"
	"unsafe"

	"github.com/min1324/queue"
)

type mapOp string

const (
	opEnQueue = mapOp("EnQueue")
	opDeQueue = mapOp("DeQueue")
)

var mapOps = [...]mapOp{opEnQueue, opDeQueue}

// mapCall is a quick.Generator for calls on mapInterface.
type mapCall struct {
	op mapOp
	k  interface{}
}

type mapResult struct {
	value interface{}
	ok    bool
}

func (c mapCall) apply(m Interface) (interface{}, bool) {
	switch c.op {
	case opEnQueue:
		return c.k, m.EnQueue(c.k)
	case opDeQueue:
		return m.DeQueue()
	default:
		panic("invalid mapOp")
	}
}

func randValue(r *rand.Rand) interface{} {
	b := make([]byte, r.Intn(4))
	for i := range b {
		b[i] = 'a' + byte(rand.Intn(26))
	}
	return string(b)
}

func (mapCall) Generate(r *rand.Rand, size int) reflect.Value {
	c := mapCall{op: mapOps[rand.Intn(len(mapOps))], k: randValue(r)}
	return reflect.ValueOf(c)
}

func applyCalls(m Interface, calls []mapCall) (results []mapResult, final map[interface{}]interface{}) {
	for _, c := range calls {
		v, ok := c.apply(m)
		results = append(results, mapResult{v, ok})
	}

	final = make(map[interface{}]interface{})

	for m.Size() > 0 {
		v, ok := m.DeQueue()
		final[v] = ok
	}
	return results, final
}

func applyMap(calls []mapCall) ([]mapResult, map[interface{}]interface{}) {
	var q queue.Queue
	q.OnceInit(prevEnQueueSize)
	return applyCalls(&q, calls)
}

func applyMutexMap(calls []mapCall) ([]mapResult, map[interface{}]interface{}) {
	var q DRQueue
	q.OnceInit(prevEnQueueSize)
	return applyCalls(&q, calls)
}

func TestMatchesMutex(t *testing.T) {
	if err := quick.CheckEqual(applyMap, applyMutexMap, nil); err != nil {
		t.Error(err)
	}
}

type queueStruct struct {
	setup func(*testing.T, Interface)
	perG  func(*testing.T, Interface)
}

func queueMap(t *testing.T, test queueStruct) {
	for _, m := range [...]Interface{
		&queue.Queue{},
		&DRQueue{},
	} {
		t.Run(fmt.Sprintf("%T", m), func(t *testing.T) {
			m = reflect.New(reflect.TypeOf(m).Elem()).Interface().(Interface)
			if test.setup != nil {
				if v, ok := m.(*queue.Queue); ok {
					v.OnceInit(prevEnQueueSize)
				}
				if v, ok := m.(*DRQueue); ok {
					v.OnceInit(prevEnQueueSize)
				}
				test.setup(t, m)
			}
			test.perG(t, m)
		})
	}
}

func TestInit(t *testing.T) {

	queueMap(t, queueStruct{
		setup: func(t *testing.T, s Interface) {
		},
		perG: func(t *testing.T, s Interface) {
			// 初始化测试，
			if v, ok := s.(*queue.Queue); ok {
				if v.Cap() != prevEnQueueSize {
					t.Fatalf("init Cap != prevEnQueueSize :%d", prevEnQueueSize)
				}
			}
			if s.Size() != 0 {
				t.Fatalf("init size != 0 :%d", s.Size())
			}

			if v, ok := s.DeQueue(); ok {
				t.Fatalf("init DeQueue != nil :%v", v)
			}
			s.Init()
			if s.Size() != 0 {
				t.Fatalf("Init err,size!=0,%d", s.Size())
			}

			if v, ok := s.DeQueue(); ok {
				t.Fatalf("Init DeQueue != nil :%v", v)
			}

			// EnQueue,DeQueue测试
			p := 1
			s.EnQueue(p)
			if s.Size() != 1 {
				t.Fatalf("after EnQueue err,size!=1,%d", s.Size())
			}

			if v, ok := s.DeQueue(); !ok || v != p {
				t.Fatalf("EnQueue want:%d, real:%v", p, v)
			}

			// size 测试
			var n = 10
			var esum int
			for i := 0; i < n; i++ {
				if s.EnQueue(i) {
					esum++
				}
			}
			if s.Size() != esum {
				t.Fatalf("Size want:%d, real:%v", esum, s.Size())
			}
			for i := 0; i < n; i++ {
				s.DeQueue()
			}
			if s.Size() != 0 {
				t.Fatalf("Size want:%d, real:%v", 0, s.Size())
			}

			// 储存顺序测试,数组队列可能满
			// stack顺序反过来
			array := [...]int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12}
			for i := range array {
				s.EnQueue(i)
				array[i] = i // queue用这种
				// array[len(array)-i-1] = i  // stack用这种方式
			}
			for i := 0; i < len(array); i++ {
				v, ok := s.DeQueue()
				if !ok || v != array[i] {
					t.Fatalf("array want:%d, real:%v", array[i], v)
				}
			}

			// 空值测试
			s.EnQueue(nil)
			if e, ok := s.DeQueue(); !ok {
				t.Fatalf("EnQueue nil want:%v, real:%v", nil, e)
			}

			var nullPtrs = unsafe.Pointer(nil)
			s.EnQueue(nullPtrs)

			if v, ok := s.DeQueue(); !ok || nullPtrs != v {
				t.Fatalf("EnQueue nil want:%v, real:%v", nullPtrs, v)
			}
			var null = new(interface{})
			s.EnQueue(null)
			if v, ok := s.DeQueue(); !ok || null != v {
				t.Fatalf("EnQueue nil want:%v, real:%v", null, v)
			}
		},
	})
}

func TestEnQueue(t *testing.T) {
	const maxSize = 1 << 10
	var sum int64
	queueMap(t, queueStruct{
		setup: func(t *testing.T, s Interface) {
		},
		perG: func(t *testing.T, s Interface) {
			sum = 0
			for i := 0; i < maxSize; i++ {
				if s.EnQueue(i) {
					atomic.AddInt64(&sum, 1)
				}
			}

			if s.Size() != int(sum) {
				t.Fatalf("TestConcurrentEnQueue err,EnQueue:%d,real:%d", sum, s.Size())
			}
		},
	})
}

func TestDeQueue(t *testing.T) {
	const maxSize = 1 << 10
	var sum int64
	queueMap(t, queueStruct{
		setup: func(t *testing.T, s Interface) {
		},
		perG: func(t *testing.T, s Interface) {
			sum = 0
			for i := 0; i < maxSize; i++ {
				if s.EnQueue(i) {
					atomic.AddInt64(&sum, 1)
				}
			}

			var dsum int64
			for i := 0; i < maxSize; i++ {
				_, ok := s.DeQueue()
				if ok {
					atomic.AddInt64(&dsum, 1)
				}
			}

			if int64(s.Size())+dsum != sum {
				t.Fatalf("TestDeQueue err,EnQueue:%d,DeQueue:%d,size:%d", sum, dsum, s.Size())
			}
		},
	})
}

func TestConcurrentInit(t *testing.T) {
	const maxGo = 4
	var timeout = time.Second * 5

	queueMap(t, queueStruct{
		setup: func(t *testing.T, s Interface) {
		},
		perG: func(t *testing.T, s Interface) {
			var wg sync.WaitGroup
			ctx, cancle := context.WithTimeout(context.Background(), timeout)

			for i := 0; i < maxGo; i++ {
				wg.Add(1)
				go func(ctx context.Context) {
					defer wg.Done()
					for {
						select {
						case <-ctx.Done():
							return
						default:
							s.DeQueue()
							time.Sleep(time.Millisecond)
						}
					}
				}(ctx)
				wg.Add(1)
				go func(ctx context.Context) {
					defer wg.Done()
					for {
						select {
						case <-ctx.Done():
							return
						default:
							s.EnQueue(1)
						}
					}
				}(ctx)
				wg.Add(1)
				go func(ctx context.Context) {
					defer wg.Done()
					for {
						select {
						case <-ctx.Done():
							return
						default:
							s.Init()
							time.Sleep(time.Millisecond * 10)
						}
					}
				}(ctx)
			}
			time.Sleep(2 * time.Second)
			cancle()
			wg.Wait()
			size := s.Size()
			sum := 0
			for {
				_, ok := s.DeQueue()
				if !ok {
					break
				}
				sum++
			}
			if size != sum {
				t.Fatalf("Init Concurrent err,real:%d,size:%d,ret:%d", sum, size, s.Size())
			}
		},
	})
}

func TestConcurrentEnQueue(t *testing.T) {
	const maxGo, maxNum = 4, 1 << 8

	queueMap(t, queueStruct{
		setup: func(t *testing.T, s Interface) {
		},
		perG: func(t *testing.T, s Interface) {
			var wg sync.WaitGroup
			var EnQueueSum int64
			for i := 0; i < maxGo; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					for i := 0; i < maxNum; i++ {
						if s.EnQueue(i) {
							atomic.AddInt64(&EnQueueSum, 1)
						}
					}
				}()
			}
			wg.Wait()
			var ret int64
			size := s.Size()
			for {
				_, ok := s.DeQueue()
				if !ok {
					break
				}
				ret += 1
			}
			if ret != int64(EnQueueSum) {
				t.Fatalf("TestConcurrentEnQueue err,EnQueue:%d,ret:%d,size:%d",
					EnQueueSum, ret, size)
			}
		},
	})
}

func TestConcurrentDeQueue(t *testing.T) {
	const maxGo, maxNum = 64, 1 << 15
	const maxSize = maxGo * maxNum

	queueMap(t, queueStruct{
		setup: func(t *testing.T, s Interface) {
		},
		perG: func(t *testing.T, s Interface) {
			var wg sync.WaitGroup
			var DeQueueSum int64
			var EnQueueSum int64
			for i := 0; i < maxSize; i++ {
				if s.EnQueue(i) {
					EnQueueSum += 1
				}
			}

			for i := 0; i < maxGo; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					for s.Size() > 0 {
						_, ok := s.DeQueue()
						if ok {
							atomic.AddInt64(&DeQueueSum, 1)
						}
					}
				}()
			}
			wg.Wait()
			var ret int64
			size := s.Size()
			for {
				_, ok := s.DeQueue()
				if !ok {
					break
				}
				ret += 1
			}
			if DeQueueSum+ret != int64(EnQueueSum) {
				t.Fatalf("TestConcurrentEnQueue err,EnQueue:%d,DeQueue:%d,ret:%d,sum:%d,size:%d",
					EnQueueSum, DeQueueSum, ret, ret+DeQueueSum, size)
			}
		},
	})
}

func TestConcurrentEnQueueDeQueue(t *testing.T) {
	const maxGo, maxNum = 8, 1 << 15
	queueMap(t, queueStruct{
		setup: func(t *testing.T, s Interface) {
		},
		perG: func(t *testing.T, s Interface) {
			var DeQueueWG sync.WaitGroup
			var EnQueueWG sync.WaitGroup

			exit := make(chan struct{}, maxGo)

			var sumEnQueue, sumDeQueue int64
			for i := 0; i < maxGo; i++ {
				EnQueueWG.Add(1)
				go func() {
					defer EnQueueWG.Done()
					for j := 0; j < maxNum; j++ {
						if s.EnQueue(j) {
							atomic.AddInt64(&sumEnQueue, 1)
						}
					}
				}()
				DeQueueWG.Add(1)
				go func() {
					defer DeQueueWG.Done()
					for {
						select {
						case <-exit:
							return
						default:
							_, ok := s.DeQueue()
							if ok {
								atomic.AddInt64(&sumDeQueue, 1)
							}
						}
					}
				}()
			}
			EnQueueWG.Wait()
			close(exit)
			DeQueueWG.Wait()
			exit = nil
			var ret int64
			size := s.Size()
			for {
				_, ok := s.DeQueue()
				if !ok {
					break
				}
				ret += 1
			}
			if sumDeQueue+ret != sumEnQueue {
				t.Fatalf("TestConcurrentEnQueueDeQueue err,EnQueue:%d,DeQueue:%d,sub:%d,ret:%d,sum:%d,size:%d",
					sumEnQueue, sumDeQueue, sumEnQueue-sumDeQueue, ret, sumDeQueue+ret, size)
			}
		},
	})
}
