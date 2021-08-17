package queue_test

import (
	"context"
	"fmt"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"

	"github.com/min1324/queue"
)

type queueStruct struct {
	setup func(*testing.T, QInterface)
	perG  func(*testing.T, QInterface)
}

func queueMap(t *testing.T, test queueStruct) {
	for _, m := range [...]QInterface{
		&queue.LRQueue{},
	} {
		t.Run(fmt.Sprintf("%T", m), func(t *testing.T) {
			m = reflect.New(reflect.TypeOf(m).Elem()).Interface().(QInterface)
			if test.setup != nil {
				test.setup(t, m)
			}
			test.perG(t, m)
		})
	}
}

func TestInit(t *testing.T) {

	queueMap(t, queueStruct{
		setup: func(t *testing.T, s QInterface) {
		},
		perG: func(t *testing.T, s QInterface) {
			// 初始化测试，
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
			s.EnQueue("a")
			s.EnQueue("a")
			s.EnQueue("a")
			s.Init()
			if s.Size() != 0 {
				t.Fatalf("after EnQueue Init err,size!=0,%d", s.Size())
			}
			if v, ok := s.DeQueue(); ok {
				t.Fatalf("after EnQueue Init DeQueue != nil :%v", v)
			}

			s.Init()
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
			s.Init()
			var n = 10
			var esum, dsum int
			for i := 0; i < n; i++ {
				if s.EnQueue(i) {
					esum++
				}
			}
			if s.Size() != esum {
				t.Fatalf("Size want:%d, real:%v", esum, s.Size())
			}
			tk := time.NewTicker(time.Second * 5)
			defer tk.Stop()
			exit := false
			for !exit {
				select {
				case <-tk.C:
					t.Fatalf("size DeQueue timeout,")
					exit = true
				default:
					_, ok := s.DeQueue()
					if ok {
						dsum++
						tk.Reset(time.Second)
					}
					if s.Size() == 0 {
						exit = true
					}
				}
			}
			if dsum != esum {
				t.Fatalf("Size enqueue:%d, dequeue:%d,size:%d", esum, dsum, s.Size())
			}

			// 储存顺序测试,数组队列可能满
			// stack顺序反过来
			s.Init()
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

			s.Init()
			// 空值测试
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
		setup: func(t *testing.T, s QInterface) {
		},
		perG: func(t *testing.T, s QInterface) {
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
		setup: func(t *testing.T, s QInterface) {
		},
		perG: func(t *testing.T, s QInterface) {
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
		setup: func(t *testing.T, s QInterface) {
			if _, ok := s.(*UnsafeQueue); ok {
				t.Skip("UnsafeQueue can not test concurrent.")
			}
		},
		perG: func(t *testing.T, s QInterface) {
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
		setup: func(t *testing.T, s QInterface) {
			if _, ok := s.(*UnsafeQueue); ok {
				t.Skip("UnsafeQueue can not test concurrent.")
			}
		},
		perG: func(t *testing.T, s QInterface) {
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
	const maxGo, maxNum = 64, 1 << 20
	const maxSize = maxGo * maxNum

	queueMap(t, queueStruct{
		setup: func(t *testing.T, s QInterface) {
			if _, ok := s.(*UnsafeQueue); ok {
				t.Skip("UnsafeQueue can not test concurrent.")
			}
		},
		perG: func(t *testing.T, s QInterface) {
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
		setup: func(t *testing.T, s QInterface) {
			if _, ok := s.(*UnsafeQueue); ok {
				t.Skip("UnsafeQueue can not test concurrent.")
			}
		},
		perG: func(t *testing.T, s QInterface) {
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
							v, ok := s.DeQueue()
							if ok {
								if v == nil {
									t.Fatal("err:v nil")
								}
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
