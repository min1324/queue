package queue_test

import (
	"fmt"
	"math/rand"
	"reflect"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"testing/quick"
	"unsafe"

	"github.com/min1324/queue"
)

type mapOp string

const (
	opPush = mapOp("Push")
	opPop  = mapOp("Pop")
)

var mapOps = [...]mapOp{opPush, opPop}

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
	case opPush:
		return c.k, m.Push(c.k)
	case opPop:
		return m.Pop()
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

func (mapCall) Generate(r *rand.Rand, Len int) reflect.Value {
	c := mapCall{op: mapOps[rand.Intn(len(mapOps))], k: randValue(r)}
	return reflect.ValueOf(c)
}

func applyCalls(m Interface, calls []mapCall) (results []mapResult, final map[interface{}]interface{}) {
	for _, c := range calls {
		v, ok := c.apply(m)
		results = append(results, mapResult{v, ok})
	}

	final = make(map[interface{}]interface{})

	for m.Len() > 0 {
		v, ok := m.Pop()
		final[v] = ok
	}
	return results, final
}

func applyQueue(calls []mapCall) ([]mapResult, map[interface{}]interface{}) {
	var q queue.LFQueue
	q.Init(prevSize)
	return applyCalls(&q, calls)
}

func applyMutexQueue(calls []mapCall) ([]mapResult, map[interface{}]interface{}) {
	var q DRQueue
	q.Init(prevSize)
	return applyCalls(&q, calls)
}

func TestMatchesMutex(t *testing.T) {
	if err := quick.CheckEqual(applyQueue, applyMutexQueue, nil); err != nil {
		t.Error(err)
	}
}

type queueStruct struct {
	setup func(*testing.T, Interface)
	perG  func(*testing.T, Interface)
}

func queueMap(t *testing.T, test queueStruct) {
	for _, m := range [...]Interface{
		&queue.LFQueue{},
		// &queue.LLQueue{},
		&DRQueue{},
	} {
		t.Run(fmt.Sprintf("%T", m), func(t *testing.T) {
			m.Init(prevSize)
			if test.setup != nil {
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
			if s.Len() != 0 {
				t.Fatalf("init Len != 0 :%d", s.Len())
			}

			if v, ok := s.Pop(); ok {
				t.Fatalf("init Pop != nil :%v", v)
			}
			s.Init(0)
			if s.Len() != 0 {
				t.Fatalf("Init err,Len!=0,%d", s.Len())
			}

			if v, ok := s.Pop(); ok {
				t.Fatalf("Init Pop != nil :%v", v)
			}

			// Push,Pop测试
			p := 1
			if !s.Push(p) {
				t.Fatalf("Push err,not ok")
			}
			if s.Len() != 1 {
				t.Fatalf("after Push err,Len!=1,%d", s.Len())
			}

			if v, ok := s.Pop(); !ok || v != p {
				t.Fatalf("Push want:%d, real:%v", p, v)
			}

			// Len 测试
			var n = 10
			var esum int
			for i := 0; i < n; i++ {
				if s.Push(i) {
					esum++
				}
			}
			if s.Len() != esum {
				t.Fatalf("Len want:%d, real:%v", esum, s.Len())
			}
			for i := 0; i < n; i++ {
				s.Pop()
			}
			if s.Len() != 0 {
				t.Fatalf("Len want:%d, real:%v", 0, s.Len())
			}

			// 储存顺序测试,数组队列可能满
			// stack顺序反过来
			array := [...]int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12}
			for i := range array {
				s.Push(i)
				array[i] = i // queue用这种
				// array[len(array)-i-1] = i  // stack用这种方式
			}
			for i := 0; i < len(array); i++ {
				v, ok := s.Pop()
				if !ok || v != array[i] {
					t.Fatalf("array want:%d, real:%v", array[i], v)
				}
			}

			// 空值测试
			s.Push(nil)
			if e, ok := s.Pop(); !ok || e != nil {
				t.Fatalf("Push nil want:%v, real:%v", nil, e)
			}

			var nullPtrs = unsafe.Pointer(nil)
			s.Push(nullPtrs)

			if v, ok := s.Pop(); !ok || nullPtrs != v {
				t.Fatalf("Push nil want:%v, real:%v", nullPtrs, v)
			}
			var null = new(interface{})
			s.Push(null)
			if v, ok := s.Pop(); !ok || null != v {
				t.Fatalf("Push nil want:%v, real:%v", null, v)
			}
		},
	})
}

func TestConcurrent(t *testing.T) {
	var wg sync.WaitGroup
	goNum := runtime.NumCPU()
	var max = 1000000
	var q queue.LFQueue
	q.Init(max)

	var args = make([]uint32, max)
	var result sync.Map
	var done uint32

	for i := range args {
		args[i] = uint32(i)
	}
	// Pop
	wg.Add(goNum)
	for i := 0; i < goNum; i++ {
		go func() {
			defer wg.Done()
			for {
				if atomic.LoadUint32(&done) == 1 && q.Len() == 0 {
					break
				}
				v, ok := q.Pop()
				if ok {
					result.Store(v, v)
				}
			}
		}()
	}
	// Push
	wg.Add(goNum)
	var gbCount uint32 = 0
	for i := 0; i < goNum; i++ {
		go func() {
			defer wg.Done()
			for {
				c := atomic.AddUint32(&gbCount, 1)
				if c >= uint32(max) {
					break
				}
				for !q.Push(c) {
				}
			}
		}()
	}
	// wait until finish Push
	for {
		c := atomic.LoadUint32(&gbCount)
		if c > uint32(max) {
			break
		}
		runtime.Gosched()
	}
	// wait until Pop
	atomic.StoreUint32(&done, 1)
	wg.Wait()

	if q.Len() > 0 {
		t.Fatalf("err Len !=0,siez:%d\n", q.Len())
	}

	// check
	for i := 1; i < max; i++ {
		e := args[i]
		v, ok := result.Load(e)
		if !ok {
			t.Errorf("err miss:%v,ok:%v,e:%v ", v, ok, e)
		}
	}
}

func TestLFQueue(t *testing.T) {
	var d queue.LFQueue
	d.Init(prevSize)
	testPoolPop(t, &d)
}

func testPoolPop(t *testing.T, d queue.Queue) {
	const P = 10
	var N int = 2e6
	if testing.Short() {
		N = 1e3
	}
	have := make([]int32, N)
	var stop int32
	var wg sync.WaitGroup
	record := func(val int) {
		atomic.AddInt32(&have[val], 1)
		if val == N-1 {
			atomic.StoreInt32(&stop, 1)
		}
	}

	// Start P-1 consumers.
	for i := 1; i < P; i++ {
		wg.Add(1)
		go func() {
			fail := 0
			for atomic.LoadInt32(&stop) == 0 {
				val, ok := d.Pop()
				if ok {
					fail = 0
					record(val.(int))
				} else {
					// Speed up the test by
					// allowing the pusher to run.
					if fail++; fail%100 == 0 {
						runtime.Gosched()
					}
				}
			}
			wg.Done()
		}()
	}

	// Start 1 producer.
	nPopHead := 0
	wg.Add(1)
	go func() {
		for j := 0; j < N; j++ {
			for !d.Push(j) {
				// Allow a popper to run.
				runtime.Gosched()
			}
			if j%10 == 0 {
				val, ok := d.Pop()
				if ok {
					nPopHead++
					record(val.(int))
				}
			}
		}
		wg.Done()
	}()
	wg.Wait()

	// Check results.
	for i, count := range have {
		if count != 1 {
			t.Errorf("expected have[%d] = 1, got %d", i, count)
		}
	}
	// Check that at least some PopHeads succeeded. We skip this
	// check in short mode because it's common enough that the
	// queue will stay nearly empty all the time and a PopTail
	// will happen during the window between every PushHead and
	// PopHead.
	if !testing.Short() && nPopHead == 0 {
		t.Errorf("popHead never succeeded")
	}
}
