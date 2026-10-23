package queue_test

import (
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/min1324/queue"
)

// ============================================================
// 辅助常量
// ============================================================

const (
	// defaultTestCap 测试默认队列容量
	defaultTestCap = 1 << 8
	// stressTestCap 压力测试队列容量
	stressTestCap = 1 << 20
	// shortTestCap 短测试队列容量
	shortTestCap = 1 << 8
)

func perQueue(t *testing.T, f func(*testing.T, Interface)) {
	var arrQueue = [...]Interface{
		// &queue.DRQueue{},
		&queue.LockFree{},
		// &queue.SPSC{},
		// &queue.LockQueue{},
	}

	for _, q := range arrQueue {
		t.Run(fmt.Sprintf("%T", q), func(t *testing.T) {
			f(t, q)
		})
	}
}

// ============================================================
// 基础功能测试
// ============================================================

// TestNew 验证 New() 创建的队列初始状态
func TestNew(t *testing.T) {
	q := queue.New()
	if got := q.Len(); got != 0 {
		t.Fatalf("New().Len() = %d, want 0", got)
	}
	if got := q.Cap(); got != 0 {
		t.Fatalf("New().Cap() = %d, want 0 before initialization", got)
	}
	if got, ok := q.Pop(); ok || got != nil {
		t.Fatalf("Pop() on new queue = (%v, %v), want (nil, false)", got, ok)
	}
}

func TestInit(t *testing.T) {
	tests := []struct {
		name     string
		capacity int
		wantCap  int
	}{
		{name: "requested capacity", capacity: 8, wantCap: 8},
		{name: "rounds up to power of two", capacity: 9, wantCap: 9},
		{name: "default capacity", capacity: 0, wantCap: defaultTestCap},
		{name: "negative capacity uses default", capacity: -1, wantCap: defaultTestCap},
		{name: "1 capacity", capacity: 1, wantCap: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			perQueue(t, func(t *testing.T, q Interface) {
				q.Init(tt.capacity)
				if got := q.Cap(); got != tt.wantCap {
					t.Fatalf("Cap() = %d, want %d", got, tt.wantCap)
				}
				if got := q.Len(); got != 0 {
					t.Fatalf("Len() after Init() = %d, want 0", got)
				}
			})
		})
	}
}

func TestInitOnlyOnce(t *testing.T) {
	perQueue(t, func(t *testing.T, q Interface) {
		q.Init(4)
		q.Init(8)

		if got := q.Cap(); got != 4 {
			t.Fatalf("Cap() after a second Init() = %d, want 4", got)
		}
	})
}

func TestPushAutoInitializes(t *testing.T) {
	perQueue(t, func(t *testing.T, q Interface) {
		if !q.Push("value") {
			t.Fatal("Push() failed on an uninitialized queue")
		}
		if got := q.Cap(); got != defaultTestCap {
			t.Fatalf("Cap() after automatic initialization = %d, want %d", got, defaultTestCap)
		}
		if got, ok := q.Pop(); !ok || got != "value" {
			t.Fatalf("Pop() = (%v, %v), want (value, true)", got, ok)
		}
	})
}

func TestQueueFIFOAndCapacity(t *testing.T) {
	perQueue(t, func(t *testing.T, q Interface) {
		var cap = 5
		q.Init(cap)
		if q.Cap() != cap {
			t.Fatalf("Cap() = %d, want %d", q.Cap(), cap)
		}
		for i := 0; i < q.Cap(); i++ {
			if !q.Push(i) {
				t.Fatalf("Push(%d) failed before queue was full", i)
			}
		}
		if got := q.Len(); got != q.Cap() {
			t.Fatalf("Len() = %d after filling queue, want %d", got, q.Cap())
		}
		if q.Push("extra") {
			t.Fatal("Push() succeeded on a full queue")
		}

		for i := 0; i < 2; i++ {
			got, ok := q.Pop()
			if !ok || got != i {
				t.Fatalf("Pop() = (%v, %v), want (%d, true)", got, ok, i)
			}
		}

		for i := cap; i < cap+2; i++ {
			if !q.Push(i) {
				t.Fatalf("Push(%d) failed after freeing slots", i)
			}
		}

		for i := 2; i < cap+2; i++ {
			got, ok := q.Pop()
			if !ok || got != i {
				t.Fatalf("Pop() = (%v, %v), want (%d, true)", got, ok, i)
			}
		}
		if got := q.Len(); got != 0 {
			t.Fatalf("Len() after draining queue = %d, want 0", got)
		}
		if got, ok := q.Pop(); ok || got != nil {
			t.Fatalf("Pop() on empty queue = (%v, %v), want (nil, false)", got, ok)
		}
	})
}

func TestQueueNilValue(t *testing.T) {
	perQueue(t, func(t *testing.T, q Interface) {
		q.Init(2)

		if !q.Push(nil) {
			t.Fatal("Push(nil) failed")
		}
		got, ok := q.Pop()
		if !ok || got != nil {
			t.Fatalf("Pop() after Push(nil) = (%v, %v), want (nil, true)", got, ok)
		}
		if _, ok := q.Pop(); ok {
			t.Fatal("Pop() succeeded after nil value was removed")
		}
	})
}

func TestConcurrentPushPop(t *testing.T) {
	const (
		producerCount    = 4
		consumerCount    = 4
		itemsPerProducer = 2000
		totalItems       = producerCount * itemsPerProducer
	)

	perQueue(t, func(t *testing.T, q Interface) {
		q.Init(64)

		start := make(chan struct{})
		results := make(chan int, totalItems)
		stop := make(chan struct{})
		var consumed int64
		var workers sync.WaitGroup

		for producer := 0; producer < producerCount; producer++ {
			j := producer
			workers.Add(1)
			go func() {
				defer workers.Done()
				<-start
				for i := 0; i < itemsPerProducer; i++ {
					value := j*itemsPerProducer + i
					for !q.Push(value) {
						select {
						case <-stop:
							return
						default:
							runtime.Gosched()
						}
					}
				}
			}()
		}

		for consumer := 0; consumer < consumerCount; consumer++ {
			workers.Add(1)
			go func() {
				defer workers.Done()
				<-start
				for atomic.AddInt64(&consumed, 1) <= totalItems {
					for {
						value, ok := q.Pop()
						if ok {
							results <- value.(int)
							break
						}
						select {
						case <-stop:
							return
						default:
							runtime.Gosched()
						}
					}

				}
			}()
		}

		finished := make(chan struct{})
		go func() {
			workers.Wait()
			close(finished)
		}()
		close(start)

		select {
		case <-finished:
		case <-time.After(10 * time.Second):
			close(stop)
			<-finished
			t.Fatal("concurrent Push/Pop did not complete within 10 seconds")
		}

		if got := len(results); got != totalItems {
			t.Fatalf("consumed %d values, want %d", got, totalItems)
		}

		seen := make([]bool, totalItems)
		for i := 0; i < totalItems; i++ {
			value := <-results
			if value < 0 || value >= totalItems {
				t.Fatalf("received out-of-range value %d", value)
			}
			if seen[value] {
				t.Fatalf("received duplicate value %d", value)
			}
			seen[value] = true
		}
		for value, ok := range seen {
			if !ok {
				t.Fatalf("value %d was not received", value)
			}
		}

		if got := q.Len(); got != 0 {
			t.Fatalf("Len() after concurrent drain = %d, want 0", got)
		}
		if _, ok := q.Pop(); ok {
			t.Fatal("Pop() succeeded after all produced values were consumed")
		}
	})
}

// func TestQueueCounterWraparound(t *testing.T) {

// 	var q queue.LockFree
// 	q.Init(9)

// 	start := ^uint64(0) - 2
// 	q.Head().Store(start)
// 	q.Tail().Store(start)

// 	for i := 0; i < q.Cap(); i++ {
// 		if !q.Push(i) {
// 			t.Fatalf("Push(%d) failed before queue was full", i)
// 		}
// 	}
// 	head := q.Head().Load()
// 	tail := q.Tail().Load()

// 	if head <= tail {
// 		t.Fatalf("expected head (%d) > tail (%d) after counter wraparound", head, tail)
// 	}
// 	if got := q.Len(); got != q.Cap() {
// 		t.Fatalf("Len() = %d after filling wrapped queue, want %d,head=%d,tail=%d,tail-head=%d", got, q.Cap(), head, tail, tail-head)
// 	}
// 	if q.Push(q.Cap()) {
// 		t.Fatal("Push succeeded on full wrapped queue")
// 	}

// 	for i := 0; i < q.Cap(); i++ {
// 		got, ok := q.Pop()
// 		if !ok {
// 			t.Fatalf("Pop() %d returned ok=false", i)
// 		}
// 		if got != i {
// 			t.Fatalf("Pop() %d = %v, want %d", i, got, i)
// 		}
// 	}
// 	if got := q.Len(); got != 0 {
// 		t.Fatalf("Len() after draining wrapped queue = %d, want 0", got)
// 	}
// 	if _, ok := q.Pop(); ok {
// 		t.Fatal("Pop() succeeded on empty wrapped queue")
// 	}
// }
