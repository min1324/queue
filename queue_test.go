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

func perQueue(t *testing.T, f func(t *testing.T, q Interface)) {
	var arrQueue = [...]Interface{
		&queue.MuxRing{},
		// &queue.LockFree{},
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
				t.Fatalf("Pop() = (%v, %v), want (%d, true),%v", got, ok, i, q)
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
		q.Push(3)
		if got, ok := q.Pop(); !ok || got != 3 {
			t.Fatalf("Pop() after Push(nil) = (%v, %v), want (3, true)", got, ok)
		}

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

func TestQueueDiffType(t *testing.T) {
	perQueue(t, func(t *testing.T, q Interface) {
		q.Init(8)
		if !q.Push(3) {
			t.Fatal("Push(3) failed")
		}
		if got, ok := q.Pop(); !ok || got != 3 {
			t.Fatalf("Pop() after Push(3) = (%v, %v), want (3, true)", got, ok)
		}

		if !q.Push("string") {
			t.Fatal("Push(string) failed")
		}

		if got, ok := q.Pop(); !ok || got != "string" {
			t.Fatalf("Pop() after Push(string) = (%v, %v), want (string, true)", got, ok)
		}
		if !q.Push(nil) {
			t.Fatal("Push(nil) failed")
		}
		if got, ok := q.Pop(); !ok || got != nil {
			t.Fatalf("Pop() after Push(nil) = (%v, %v), want (nil, true)", got, ok)
		}
		if _, ok := q.Pop(); ok {
			t.Fatal("Pop() succeeded after nil value was removed")
		}
		if !q.Push(true) {
			t.Fatal("Push(string) failed")
		}
		if got, ok := q.Pop(); !ok || got != true {
			t.Fatalf("Pop() after Push(true) = (%v, %v), want (true, true)", got, ok)
		}
	})
}

func TestConcurrentPushPop_LockFree(t *testing.T) {
	tConcurrentPushPop(t, &queue.LockFree{})
}

func TestConcurrentPushPop_DRQueue(t *testing.T) {
	tConcurrentPushPop(t, &queue.MuxRing{})
}

func tConcurrentPushPop(t *testing.T, q Interface) {
	const (
		producerCount    = 4
		consumerCount    = 4
		itemsPerProducer = 2000
		totalItems       = producerCount * itemsPerProducer
	)

	q.Init(shortTestCap)

	start := make(chan struct{})
	stop := make(chan struct{})
	results := make(chan int, totalItems)
	var consumed int64
	var produced int64
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
				atomic.AddInt64(&produced, 1)
			}
		}()
	}

	for consumer := 0; consumer < consumerCount; consumer++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			for {
				select {
				case <-stop:
					return
				default:
					if consumed >= totalItems {
						return
					}
					vals, i := q.BatchPop(1)
					if i > 0 {
						for _, val := range vals {
							results <- val.(int)
							atomic.AddInt64(&consumed, 1)
						}
					}
					// value, ok := q.Pop()
					// if ok {
					// 	results <- value.(int)
					// 	atomic.AddInt64(&consumed, 1)
					// }
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
		t.Fatal("concurrent Push/Pop did not complete within 10 seconds")
	}

	if produced != totalItems {
		t.Fatalf("produced %d values, want %d", produced, totalItems)
	}

	if produced != consumed {
		t.Fatalf("produced (%d) != consumed (%d)", produced, consumed)
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
}

func TestPushPopBatch_LockFree(t *testing.T) {
	tPushPopBatch(t, &queue.LockFree{})
}

func TestPushPopBatch_DRQueue(t *testing.T) {
	tPushPopBatch(t, &queue.MuxRing{})
}

// 标准测试：验证批量操作的正确性
func tPushPopBatch(t *testing.T, q Interface) {
	const queueCap = shortTestCap
	q.Init(queueCap)

	// 测试1: 基本批量推送和弹出
	testData := []any{1, 2, 3, 4, 5}
	pushed := q.BatchPush(testData...)
	if pushed != len(testData) {
		t.Errorf("测试1: Expected to push %d items, but pushed %d", len(testData), pushed)
	}

	// 批量弹出
	popped, actual := q.BatchPop(len(testData))
	if actual != len(testData) {
		t.Errorf("测试1: Expected to pop %d items, but popped %d", len(testData), actual)
	}

	// 验证数据一致性
	for i := 0; i < len(testData); i++ {
		if popped[i] != testData[i] {
			t.Errorf("测试1: Expected %v, got %v", testData[i], popped[i])
		}
	}

	// 测试2: 部分批量操作
	q.BatchPush(1, 2, 3, 4, 5)
	popped, actual = q.BatchPop(3)
	if actual != 3 {
		t.Errorf("测试2: Expected to pop 3 items, but popped %d", actual)
	}

	// 验证队列中剩余元素
	remaining, _ := q.BatchPop(10)
	if len(remaining) != 2 {
		t.Errorf("测试2: Expected 2 remaining items, got %d", len(remaining))
	}

	// 测试3: 填满队列
	fillData := make([]any, queueCap)
	for i := 0; i < queueCap; i++ {
		fillData[i] = i
	}

	pushed = q.BatchPush(fillData...)
	if pushed != queueCap {
		t.Errorf("测试3: Expected to push %d items, but pushed %d", queueCap, pushed)
	}

	// 测试4: 队列已满时的批量操作
	overflow := []any{100, 101, 102}
	pushed = q.BatchPush(overflow...)
	if pushed != 0 {
		t.Errorf("测试4: Expected to push 0 items (queue full), but pushed %d", pushed)
	}

	// 测试5: 从满队列批量弹出
	popped, actual = q.BatchPop(queueCap >> 2)
	if actual != queueCap>>2 {
		t.Errorf("测试5: Expected to pop %d items, but popped %d", queueCap/2, actual)
	}

	oldlen := q.Len()
	avl := q.Cap() - oldlen

	testData6 := make([]any, avl)
	for i := 0; i < len(testData6); i++ {
		testData6[i] = i + 1000
	}

	// 测试6: 在部分填充的队列中批量操作
	pushed = q.BatchPush(testData6...)
	if pushed != len(testData6) {
		t.Errorf("测试6: Expected to push %d items, but pushed %d", len(testData6), pushed)
	}

	_, actual = q.BatchPop(oldlen)
	if actual != oldlen {
		t.Errorf("测试65: Expected to pop %d items, but popped %d", oldlen, actual)
	}

	popped, actual = q.BatchPop(len(testData6))
	if len(popped) != len(testData6) || actual != len(testData6) {
		t.Errorf("测试6: Expected to pop %d items, but popped %d, actual %d", len(testData6), len(popped), actual)
	}

	for i, v := range popped {
		if v != testData6[i] {
			t.Errorf("测试6: Expected %v, got %v", testData6[i], v)
		}
	}
}

func TestPushPopBatchConcurrent_LockFree(t *testing.T) {
	pushPopBatchConcurrent(t, &queue.LockFree{})
}

func TestPushPopBatchConcurrent_DRQueue(t *testing.T) {
	pushPopBatchConcurrent(t, &queue.MuxRing{})
}

// 压力测试：验证并发环境下的批量操作性能
func pushPopBatchConcurrent(t *testing.T, q Interface) {
	const (
		numPusG       = 10
		numCumG       = 10
		numOperations = 100
		batchSize     = 10
		queueCap      = numOperations * batchSize * numPusG
	)

	q.Init(queueCap)
	var wg sync.WaitGroup
	popg := sync.WaitGroup{}

	stop := make(chan struct{})
	seenmap := sync.Map{}

	// 启动多个goroutine进行批量推送
	for i := 0; i < numPusG; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < numOperations; j++ {
				// 准备批量数据
				vals := make([]any, batchSize)
				for k := 0; k < batchSize; k++ {
					vals[k] = id*numOperations + j*batchSize + k
				}
				for q.BatchPush(vals...) == 0 {
					runtime.Gosched()
				}
			}
		}(i)
	}
	var totalPop uint64
	for i := 0; i < numCumG; i++ {
		popg.Add(1)
		go func(id int) {
			defer popg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					res, i := q.BatchPop(numOperations)
					if i > 0 {
						for _, v := range res {
							seenmap.Store(v, true)
						}
					}
					if atomic.AddUint64(&totalPop, uint64(i)) == queueCap {
						return
					}
				}
			}
		}(i)
	}

	go func() {
		select {
		case <-time.After(time.Second * 10):
			if stop != nil {
				close(stop)
			}
		case <-stop:
			return
		}
	}()

	wg.Wait()
	popg.Wait()

	// 验证队列最终为空
	got, remaining := q.BatchPop(queueCap)
	if remaining != 0 || len(got) != 0 {
		t.Errorf("Expected queue to be empty, but has %d,%d items", len(got), remaining)
	}
	if totalPop != queueCap {
		t.Errorf("Expected to pop %d items, but popped %d", queueCap, totalPop)
	}
	seenmap.Range(func(key, value any) bool {
		if !value.(bool) {
			t.Errorf("Missing value %v", key)
		}
		return true
	})
}

// TestClean 测试 Close 方法的功能
func TestClean(t *testing.T) {
	perQueue(t, func(t *testing.T, q Interface) {
		// 测试1: 空队列
		q.Init(10)

		remaining, count := q.Clean()
		if count != 0 {
			t.Errorf("Expected 0 items, got %d", count)
		}
		if len(remaining) != 0 {
			t.Errorf("Expected empty slice, got %v", remaining)
		}
	})

	// 测试2: 非空队列
	perQueue(t, func(t *testing.T, q Interface) {

		q.Init(10)

		// 添加一些元素
		expected := []any{1, 2, 3, 4, 5}
		for _, val := range expected {
			if !q.Push(val) {
				t.Errorf("Failed to push value %v", val)
			}
		}

		// 关闭队列并获取剩余元素
		remaining, count := q.Clean()
		if count != len(expected) {
			t.Errorf("Expected %d items, got %d", len(expected), count)
		}

		// 验证元素是否一致
		if len(remaining) != len(expected) {
			t.Errorf("Expected %d elements, got %d", len(expected), len(remaining))
		}

		for i, val := range remaining {
			if val != expected[i] {
				t.Errorf("Expected %v at index %d, got %v", expected[i], i, val)
			}
		}
	})

	// 测试3: 部分消费的队列
	// t.Run("Partially Consumed Queue", func(t *testing.T) {
	perQueue(t, func(t *testing.T, q Interface) {
		q.Init(10)

		// 添加一些元素
		expected := []any{1, 2, 3, 4, 5}
		for _, val := range expected {
			if !q.Push(val) {
				t.Errorf("Failed to push value %v", val)
			}
		}

		// 消费部分元素
		consumed, _ := q.Pop()
		if consumed != 1 {
			t.Errorf("Expected to consume 1, got %v", consumed)
		}

		// 关闭队列并获取剩余元素
		remaining, count := q.Clean()
		if count != len(expected)-1 {
			t.Errorf("Expected %d items, got %d", len(expected)-1, count)
		}

		// 验证剩余元素是否正确
		if len(remaining) != len(expected)-1 {
			t.Errorf("Expected %d elements, got %d", len(expected)-1, len(remaining))
		}

		for i, val := range remaining {
			if val != expected[i+1] {
				t.Errorf("Expected %v at index %d, got %v", expected[i+1], i, val)
			}
		}
	})

	// 测试4: 并发场景下的关闭
	// t.Run("Concurrent Clean", func(t *testing.T) {
	perQueue(t, func(t *testing.T, q Interface) {
		q.Init(10)

		// 添加一些元素
		for i := 0; i < 5; i++ {
			if !q.Push(i) {
				t.Errorf("Failed to push value %d", i)
			}
		}

		// 启动一个 goroutine 进行关闭操作
		done := make(chan struct{})
		go func() {
			defer close(done)
			remaining, count := q.Clean()
			if count != 5 {
				t.Errorf("Expected 5 items, got %d", count)
			}
			if len(remaining) != 5 {
				t.Errorf("Expected 5 elements, got %d", len(remaining))
			}
		}()

		// 等待关闭操作完成
		<-done

		// 尝试再次关闭，应该返回空
		remaining, count := q.Clean()
		if count != 0 {
			t.Errorf("Expected 0 items after first Clean, got %d", count)
		}
		if len(remaining) != 0 {
			t.Errorf("Expected empty slice after first Clean, got %v", remaining)
		}
	})

	// 测试5: 包含 nil 值的队列
	// t.Run("Queue with nil values", func(t *testing.T) {
	perQueue(t, func(t *testing.T, q Interface) {
		q.Init(10)

		// 添加一些元素，包括 nil 值
		expected := []any{1, nil, 3, nil, 5}
		for _, val := range expected {
			if !q.Push(val) {
				t.Errorf("Failed to push value %v", val)
			}
		}

		// 关闭队列并获取剩余元素
		remaining, count := q.Clean()
		if count != len(expected) {
			t.Errorf("Expected %d items, got %d", len(expected), count)
		}

		// 验证元素是否一致
		if len(remaining) != len(expected) {
			t.Errorf("Expected %d elements, got %d", len(expected), len(remaining))
		}

		for i, val := range remaining {
			if val != expected[i] {
				t.Errorf("Expected %v at index %d, got %v", expected[i], i, val)
			}
		}
	})
}
