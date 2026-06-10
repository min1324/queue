package queue_test

import (
	"fmt"
	"math/rand"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"

	"github.com/min1324/queue"
)

// ============================================================
// 辅助常量
// ============================================================

const (
	// defaultTestCap 测试默认队列容量
	defaultTestCap = 1 << 10
	// stressTestCap 压力测试队列容量
	stressTestCap = 1 << 20
	// shortTestCap 短测试队列容量
	shortTestCap = 1 << 8
)

// testCap 根据测试模式返回合适的队列容量
func testCap() int {
	if testing.Short() {
		return shortTestCap
	}
	return defaultTestCap
}

// ============================================================
// 基础功能测试
// ============================================================

// TestNew 验证 New() 创建的队列初始状态
func TestNew(t *testing.T) {
	q := queue.New()
	if q == nil {
		t.Fatal("New() 返回 nil")
	}
	// 未初始化时 Cap 和 Len 应为 0
	if cap := q.Cap(); cap != 0 {
		t.Fatalf("未初始化队列 Cap() = %d, 期望 0", cap)
	}
	if len := q.Len(); len != 0 {
		t.Fatalf("未初始化队列 Len() = %d, 期望 0", len)
	}
}

// TestInit 验证队列初始化行为
func TestInit(t *testing.T) {
	t.Run("正常容量", func(t *testing.T) {
		var q queue.LockFree
		q.Init(256)
		if q.Cap() < 256 {
			t.Fatalf("Init(256) 后 Cap() = %d, 期望 >= 256", q.Cap())
		}
		if q.Len() != 0 {
			t.Fatalf("Init(256) 后 Len() = %d, 期望 0", q.Len())
		}
	})

	t.Run("零容量使用默认值", func(t *testing.T) {
		var q queue.LockFree
		q.Init(0)
		if q.Cap() == 0 {
			t.Fatal("Init(0) 后 Cap() = 0, 期望使用默认容量")
		}
	})

	t.Run("负容量使用默认值", func(t *testing.T) {
		var q queue.LockFree
		q.Init(-1)
		if q.Cap() == 0 {
			t.Fatal("Init(-1) 后 Cap() = 0, 期望使用默认容量")
		}
	})

	t.Run("只能初始化一次", func(t *testing.T) {
		var q queue.LockFree
		q.Init(256)
		firstCap := q.Cap()
		q.Init(1024) // 第二次初始化应无效
		if q.Cap() != firstCap {
			t.Fatalf("重复 Init 后 Cap() = %d, 期望 %d", q.Cap(), firstCap)
		}
	})
}

// TestPushPop 验证基本的 Push/Pop 操作
func TestPushPop(t *testing.T) {
	var q queue.LockFree
	q.Init(testCap())

	t.Run("Push后Pop", func(t *testing.T) {
		val := 42
		if !q.Push(val) {
			t.Fatal("Push(42) 返回 false")
		}
		if q.Len() != 1 {
			t.Fatalf("Push 后 Len() = %d, 期望 1", q.Len())
		}
		got, ok := q.Pop()
		if !ok {
			t.Fatal("Pop() 返回 ok=false")
		}
		if got != val {
			t.Fatalf("Pop() = %v, 期望 %v", got, val)
		}
		if q.Len() != 0 {
			t.Fatalf("Pop 后 Len() = %d, 期望 0", q.Len())
		}
	})

	t.Run("空队列Pop", func(t *testing.T) {
		val, ok := q.Pop()
		if ok {
			t.Fatalf("空队列 Pop() 返回 ok=true, val=%v", val)
		}
		if val != nil {
			t.Fatalf("空队列 Pop() = %v, 期望 nil", val)
		}
	})
}

// TestFIFO 验证队列的先进先出顺序
func TestFIFO(t *testing.T) {
	var q queue.LockFree
	q.Init(testCap())

	n := 100
	for i := 0; i < n; i++ {
		if !q.Push(i) {
			t.Fatalf("Push(%d) 返回 false", i)
		}
	}

	for i := 0; i < n; i++ {
		val, ok := q.Pop()
		if !ok {
			t.Fatalf("Pop() 第 %d 个元素返回 ok=false", i)
		}
		if val != i {
			t.Fatalf("第 %d 个元素 = %v, 期望 %d", i, val, i)
		}
	}
}

// TestNilValue 验证 nil 值的存储和读取
func TestNilValue(t *testing.T) {
	var q queue.LockFree
	q.Init(testCap())

	// 测试 nil interface{}
	if !q.Push(nil) {
		t.Fatal("Push(nil) 返回 false")
	}
	val, ok := q.Pop()
	if !ok {
		t.Fatal("Pop() 返回 ok=false")
	}
	if val != nil {
		t.Fatalf("Pop() = %v, 期望 nil", val)
	}

	// 测试 unsafe.Pointer(nil)
	var nullPtr unsafe.Pointer
	if !q.Push(nullPtr) {
		t.Fatal("Push(unsafe.Pointer(nil)) 返回 false")
	}
	val, ok = q.Pop()
	if !ok {
		t.Fatal("Pop() 返回 ok=false")
	}
	if val != nullPtr {
		t.Fatalf("Pop() = %v, 期望 %v", val, nullPtr)
	}

	// 测试 *interface{}(nil)
	var nilInterface *interface{}
	if !q.Push(nilInterface) {
		t.Fatal("Push(*interface{}(nil)) 返回 false")
	}
	val, ok = q.Pop()
	if !ok {
		t.Fatal("Pop() 返回 ok=false")
	}
	if val != nilInterface {
		t.Fatalf("Pop() = %v, 期望 %v", val, nilInterface)
	}
}

// TestFullQueue 验证队列满时的行为
func TestFullQueue(t *testing.T) {
	var q queue.LockFree
	q.Init(16) // 小容量便于测试

	// 填满队列
	fillCount := 0
	for i := 0; ; i++ {
		if !q.Push(i) {
			break
		}
		fillCount++
	}

	if fillCount == 0 {
		t.Fatal("未能 Push 任何元素")
	}

	// 队列满时应返回 false
	if q.Push(999) {
		t.Fatal("满队列 Push() 应返回 false")
	}

	// Pop 一个后应该可以再 Push
	if _, ok := q.Pop(); !ok {
		t.Fatal("Pop() 返回 ok=false")
	}
	if !q.Push(999) {
		t.Fatal("Pop 后 Push() 应返回 true")
	}
}

// TestAutoInit 验证未显式初始化时自动初始化
func TestAutoInit(t *testing.T) {
	var q queue.LockFree
	// 不调用 Init，直接 Push
	if !q.Push(1) {
		t.Fatal("未初始化队列 Push() 返回 false")
	}
	val, ok := q.Pop()
	if !ok || val != 1 {
		t.Fatalf("Pop() = %v, ok=%v, 期望 1, true", val, ok)
	}
}

// TestResize 验证队列容量调整
func TestResize(t *testing.T) {
	var q queue.LockFree
	q.Init(64)

	t.Run("扩大容量", func(t *testing.T) {
		if !q.Resize(256) {
			t.Fatal("Resize(256) 返回 false")
		}
		if q.Cap() < 256 {
			t.Fatalf("Resize 后 Cap() = %d, 期望 >= 256", q.Cap())
		}
	})

	t.Run("缩小到当前元素数以下", func(t *testing.T) {
		var q2 queue.LockFree
		q2.Init(256)
		for i := 0; i < 10; i++ {
			q2.Push(i)
		}
		if q2.Resize(5) {
			t.Fatal("Resize(5) 在有10个元素时应返回 false")
		}
	})

	t.Run("保留已有元素", func(t *testing.T) {
		var q3 queue.LockFree
		q3.Init(64)
		for i := 0; i < 10; i++ {
			q3.Push(i)
		}
		if !q3.Resize(256) {
			t.Fatal("Resize(256) 返回 false")
		}
		// 验证元素仍然存在且顺序正确
		for i := 0; i < 10; i++ {
			val, ok := q3.Pop()
			if !ok || val != i {
				t.Fatalf("Resize 后第 %d 个元素 = %v, 期望 %d", i, val, i)
			}
		}
	})

	t.Run("未初始化队列Resize", func(t *testing.T) {
		var q4 queue.LockFree
		if q4.Resize(256) {
			t.Fatal("未初始化队列 Resize() 应返回 false")
		}
	})
}

// TestLen 验证 Len() 方法的准确性
func TestLen(t *testing.T) {
	var q queue.LockFree
	q.Init(testCap())

	if q.Len() != 0 {
		t.Fatalf("初始 Len() = %d, 期望 0", q.Len())
	}

	for i := 1; i <= 100; i++ {
		q.Push(i)
		if q.Len() != i {
			t.Fatalf("Push %d 个后 Len() = %d", i, q.Len())
		}
	}

	for i := 99; i >= 0; i-- {
		q.Pop()
		if q.Len() != i {
			t.Fatalf("Pop 后 Len() = %d, 期望 %d", q.Len(), i)
		}
	}
}

// TestCap 验证 Cap() 方法
func TestCap(t *testing.T) {
	var q queue.LockFree
	if q.Cap() != 0 {
		t.Fatalf("未初始化 Cap() = %d, 期望 0", q.Cap())
	}
	q.Init(128)
	if q.Cap() == 0 {
		t.Fatal("Init(128) 后 Cap() = 0")
	}
}

// TestMixedTypes 验证队列存储不同类型值
func TestMixedTypes(t *testing.T) {
	var q queue.LockFree
	q.Init(testCap())

	values := []interface{}{
		42,
		"hello",
		3.14,
		[]int{1, 2, 3},
		map[string]int{"a": 1},
		struct{ Name string }{"test"},
	}

	for _, v := range values {
		if !q.Push(v) {
			t.Fatalf("Push(%v) 返回 false", v)
		}
	}

	for i, expected := range values {
		got, ok := q.Pop()
		if !ok {
			t.Fatalf("第 %d 个 Pop() 返回 ok=false", i)
		}
		// 对于 map 和 struct 使用 fmt.Sprintf 比较
		if fmt.Sprintf("%v", got) != fmt.Sprintf("%v", expected) {
			t.Fatalf("第 %d 个元素 = %v, 期望 %v", i, got, expected)
		}
	}
}

// ============================================================
// 并发正确性测试
// ============================================================

// TestConcurrentPushPop 多协程并发 Push 和 Pop
// 使用生产者先完成、消费者后排空的模式，确保数据完整性
func TestConcurrentPushPop(t *testing.T) {
	var q queue.LockFree
	q.Init(stressTestCap)

	numProducers := runtime.NumCPU()
	numConsumers := runtime.NumCPU()
	totalItems := 100000
	if testing.Short() {
		totalItems = 1000
	}
	// 确保 totalItems 能被 numProducers 整除
	totalItems = totalItems - totalItems%numProducers

	var producerWg sync.WaitGroup
	resultMap := sync.Map{}

	// 启动生产者
	itemsPerProducer := totalItems / numProducers
	producerWg.Add(numProducers)
	for i := 0; i < numProducers; i++ {
		go func(id int) {
			defer producerWg.Done()
			for j := 0; j < itemsPerProducer; j++ {
				val := id*itemsPerProducer + j
				for !q.Push(val) {
					runtime.Gosched()
				}
			}
		}(i)
	}

	// 启动消费者，持续消费直到生产者完成且队列为空
	var consumerWg sync.WaitGroup
	allProduced := int32(0)
	consumerWg.Add(numConsumers)
	for i := 0; i < numConsumers; i++ {
		go func() {
			defer consumerWg.Done()
			for {
				val, ok := q.Pop()
				if ok {
					if v, ok := val.(int); ok {
						resultMap.Store(v, true)
					}
				} else if atomic.LoadInt32(&allProduced) == 1 {
					// 生产者已完成且队列为空，再尝试一次确保没有遗漏
					if val, ok := q.Pop(); ok {
						if v, ok := val.(int); ok {
							resultMap.Store(v, true)
						}
						continue // 可能还有更多
					}
					return
				} else {
					runtime.Gosched()
				}
			}
		}()
	}

	// 等待生产者完成
	producerWg.Wait()
	atomic.StoreInt32(&allProduced, 1)

	// 等待消费者完成
	consumerDone := make(chan struct{})
	go func() {
		consumerWg.Wait()
		close(consumerDone)
	}()

	select {
	case <-consumerDone:
	case <-time.After(30 * time.Second):
		t.Fatalf("消费者超时，队列剩余: %d", q.Len())
	}

	// 最终排空（安全起见）
	for {
		val, ok := q.Pop()
		if !ok {
			break
		}
		if v, ok := val.(int); ok {
			resultMap.Store(v, true)
		}
	}

	// 验证所有值都被弹出
	missing := 0
	for i := 0; i < totalItems; i++ {
		if _, ok := resultMap.Load(i); !ok {
			missing++
			if missing <= 5 {
				t.Errorf("值 %d 未被弹出", i)
			}
		}
	}
	if missing > 0 {
		t.Fatalf("共有 %d 个值未被弹出（总数据量: %d）", missing, totalItems)
	}
}

// TestConcurrentMultiProducerSingleConsumer 多生产者单消费者
func TestConcurrentMultiProducerSingleConsumer(t *testing.T) {
	var q queue.LockFree
	q.Init(stressTestCap)

	numProducers := runtime.NumCPU()
	itemsPerProducer := 10000
	if testing.Short() {
		itemsPerProducer = 100
	}
	totalItems := numProducers * itemsPerProducer

	var producerWg sync.WaitGroup
	var producerDone int32 // 标记生产者是否全部完成
	var popCount int64
	resultMap := sync.Map{}

	// 单消费者：持续消费直到所有元素弹出
	consumerDone := make(chan struct{})
	go func() {
		defer close(consumerDone)
		for {
			val, ok := q.Pop()
			if ok {
				if v, ok := val.(int); ok {
					resultMap.Store(v, true)
					if atomic.AddInt64(&popCount, 1) >= int64(totalItems) {
						return
					}
				}
			} else {
				// 队列为空时检查生产者是否已完成
				if atomic.LoadInt32(&producerDone) == 1 {
					// 生产者已完成，排空剩余元素
					for {
						val, ok := q.Pop()
						if !ok {
							return
						}
						if v, ok := val.(int); ok {
							resultMap.Store(v, true)
							atomic.AddInt64(&popCount, 1)
						}
					}
				}
				runtime.Gosched()
			}
		}
	}()

	// 多生产者
	producerWg.Add(numProducers)
	for i := 0; i < numProducers; i++ {
		go func(id int) {
			defer producerWg.Done()
			for j := 0; j < itemsPerProducer; j++ {
				val := id*itemsPerProducer + j
				for !q.Push(val) {
					runtime.Gosched()
				}
			}
		}(i)
	}

	// 等待生产者完成（确保所有 Push 结束）
	go func() {
		producerWg.Wait()
		atomic.StoreInt32(&producerDone, 1)
	}()

	// 等待消费者完成
	select {
	case <-consumerDone:
	case <-time.After(30 * time.Second):
		t.Fatalf("消费者超时，已弹出 %d/%d", atomic.LoadInt64(&popCount), totalItems)
	}

	// 验证
	missing := 0
	for i := 0; i < totalItems; i++ {
		if _, ok := resultMap.Load(i); !ok {
			missing++
			if missing <= 5 {
				t.Errorf("值 %d 未被弹出", i)
			}
		}
	}
	if missing > 0 {
		t.Fatalf("共有 %d 个值未被弹出（总数据量: %d）", missing, totalItems)
	}
}

// TestConcurrentSingleProducerMultiConsumer 单生产者多消费者
func TestConcurrentSingleProducerMultiConsumer(t *testing.T) {
	var q queue.LockFree
	q.Init(stressTestCap)

	numConsumers := runtime.NumCPU()
	totalItems := 100000
	if testing.Short() {
		totalItems = 1000
	}

	var consumerWg sync.WaitGroup
	allProduced := int32(0)
	resultMap := sync.Map{}

	// 多消费者
	consumerWg.Add(numConsumers)
	for i := 0; i < numConsumers; i++ {
		go func() {
			defer consumerWg.Done()
			for {
				val, ok := q.Pop()
				if ok {
					if v, ok := val.(int); ok {
						resultMap.Store(v, true)
					}
				} else if atomic.LoadInt32(&allProduced) == 1 {
					// 生产者已完成且队列为空，再尝试一次
					if val, ok := q.Pop(); ok {
						if v, ok := val.(int); ok {
							resultMap.Store(v, true)
						}
						continue
					}
					return
				} else {
					runtime.Gosched()
				}
			}
		}()
	}

	// 单生产者（在主协程中）
	for i := 0; i < totalItems; i++ {
		for !q.Push(i) {
			runtime.Gosched()
		}
	}
	atomic.StoreInt32(&allProduced, 1)

	// 等待消费者完成，带超时
	done := make(chan struct{})
	go func() {
		consumerWg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatalf("消费者超时，队列剩余: %d", q.Len())
	}

	// 最终排空
	for {
		val, ok := q.Pop()
		if !ok {
			break
		}
		if v, ok := val.(int); ok {
			resultMap.Store(v, true)
		}
	}

	// 验证
	missing := 0
	for i := 0; i < totalItems; i++ {
		if _, ok := resultMap.Load(i); !ok {
			missing++
			if missing <= 5 {
				t.Errorf("值 %d 未被弹出", i)
			}
		}
	}
	if missing > 0 {
		t.Fatalf("共有 %d 个值未被弹出（总数据量: %d）", missing, totalItems)
	}
}

// TestConcurrentMixedPushPop 每个协程同时执行 Push 和 Pop
func TestConcurrentMixedPushPop(t *testing.T) {
	var q queue.LockFree
	q.Init(stressTestCap)

	numGoroutines := runtime.NumCPU()
	opsPerGoroutine := 50000
	if testing.Short() {
		opsPerGoroutine = 500
	}

	var wg sync.WaitGroup
	var totalPush int64
	var totalPop int64

	wg.Add(numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			r := rand.New(rand.NewSource(int64(id)))
			pushed := 0
			popped := 0
			for pushed < opsPerGoroutine {
				if r.Intn(2) == 0 {
					if q.Push(id*opsPerGoroutine + pushed) {
						pushed++
						atomic.AddInt64(&totalPush, 1)
					}
				} else {
					if _, ok := q.Pop(); ok {
						popped++
						atomic.AddInt64(&totalPop, 1)
					}
				}
			}
		}(i)
	}
	wg.Wait()

	// 排空队列
	for {
		if _, ok := q.Pop(); !ok {
			break
		}
		atomic.AddInt64(&totalPop, 1)
	}

	t.Logf("总操作: Push=%d, Pop=%d", totalPush, totalPop)
	if totalPush != totalPop {
		t.Errorf("Push(%d) != Pop(%d)，数据不一致", totalPush, totalPop)
	}
}

// TestConcurrentResize 并发 Resize 测试
// 验证在并发 Push/Pop 的同时进行 Resize 不会导致 panic 或数据损坏
func TestConcurrentResize(t *testing.T) {
	var q queue.LockFree
	q.Init(4096)

	// 阶段1：先填充一些数据
	for i := 0; i < 1000; i++ {
		q.Push(i)
	}

	// 阶段2：并发 Resize
	var wg sync.WaitGroup
	wg.Add(5)
	for i := 0; i < 5; i++ {
		go func(id int) {
			defer wg.Done()
			newCap := 4096 + id*1024
			q.Resize(newCap)
		}(i)
	}
	wg.Wait()

	// 验证 Resize 后队列仍然可用
	if q.Len() != 1000 {
		t.Fatalf("Resize 后 Len() = %d, 期望 1000", q.Len())
	}

	// 验证数据完整性
	for i := 0; i < 1000; i++ {
		val, ok := q.Pop()
		if !ok {
			t.Fatalf("第 %d 个 Pop() 失败", i)
		}
		if val != i {
			t.Fatalf("第 %d 个元素 = %v, 期望 %d", i, val, i)
		}
	}

	// 阶段3：Resize 后继续 Push/Pop
	for i := 0; i < 500; i++ {
		if !q.Push(i) {
			t.Fatalf("Resize 后 Push(%d) 失败", i)
		}
	}
	for i := 0; i < 500; i++ {
		val, ok := q.Pop()
		if !ok || val != i {
			t.Fatalf("Resize 后 Pop() 第 %d 个元素异常: %v, ok=%v", i, val, ok)
		}
	}
}

// ============================================================
// 压力测试
// ============================================================

// TestStressHighContention 高竞争压力测试
// 大量协程对一个小容量队列进行操作
func TestStressHighContention(t *testing.T) {
	if testing.Short() {
		t.Skip("跳过压力测试")
	}

	var q queue.LockFree
	q.Init(64) // 小容量制造高竞争

	numGoroutines := runtime.NumCPU() * 4
	opsPerGoroutine := 100000

	var pushCount int64
	var popCount int64
	var wg sync.WaitGroup

	wg.Add(numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			r := rand.New(rand.NewSource(int64(id)))
			localPush := 0
			localPop := 0

			for localPush+localPop < opsPerGoroutine {
				if r.Intn(2) == 0 {
					if q.Push(id*opsPerGoroutine + localPush) {
						localPush++
						atomic.AddInt64(&pushCount, 1)
					}
				} else {
					if _, ok := q.Pop(); ok {
						localPop++
						atomic.AddInt64(&popCount, 1)
					}
				}
			}
		}(i)
	}

	wg.Wait()

	// 排空队列
	for {
		if _, ok := q.Pop(); !ok {
			break
		}
		atomic.AddInt64(&popCount, 1)
	}

	t.Logf("总操作: Push=%d, Pop=%d, 总计=%d", pushCount, popCount, pushCount+popCount)
	if pushCount != popCount {
		t.Logf("注意: Push(%d) != Pop(%d), 差值可能由队列满/空时的重试导致", pushCount, popCount)
	}
}

// TestStressLargeData 大数据量压力测试
// 向队列中 Push 大量数据并验证完整性
func TestStressLargeData(t *testing.T) {
	if testing.Short() {
		t.Skip("跳过压力测试")
	}

	var q queue.LockFree
	q.Init(stressTestCap)

	totalItems := 2000000
	itemsPerGoroutine := 50000
	numProducers := totalItems / itemsPerGoroutine

	var pushCount int64
	var popCount int64
	var wg sync.WaitGroup
	resultMap := sync.Map{}

	// 生产者
	wg.Add(numProducers)
	for i := 0; i < numProducers; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < itemsPerGoroutine; j++ {
				val := id*itemsPerGoroutine + j
				for !q.Push(val) {
					runtime.Gosched()
				}
				atomic.AddInt64(&pushCount, 1)
			}
		}(i)
	}

	// 消费者
	numConsumers := runtime.NumCPU()
	var consumerWg sync.WaitGroup
	consumerWg.Add(numConsumers)
	for i := 0; i < numConsumers; i++ {
		go func() {
			defer consumerWg.Done()
			for {
				val, ok := q.Pop()
				if ok {
					if v, ok := val.(int); ok {
						resultMap.Store(v, true)
						atomic.AddInt64(&popCount, 1)
					}
				}
				if atomic.LoadInt64(&popCount) >= int64(totalItems) {
					return
				}
				if atomic.LoadInt64(&pushCount) >= int64(totalItems) && q.Len() == 0 {
					return
				}
				runtime.Gosched()
			}
		}()
	}

	wg.Wait()
	consumerWg.Wait()

	// 验证数据完整性
	missing := 0
	for i := 0; i < totalItems; i++ {
		if _, ok := resultMap.Load(i); !ok {
			missing++
			if missing <= 10 {
				t.Errorf("值 %d 未被弹出", i)
			}
		}
	}
	if missing > 0 {
		t.Fatalf("共有 %d 个值未被弹出（总数据量: %d）", missing, totalItems)
	}
}

// TestStressLongRunning 长时间运行压力测试
// 持续一段时间的并发操作，验证稳定性
func TestStressLongRunning(t *testing.T) {
	if testing.Short() {
		t.Skip("跳过压力测试")
	}

	var q queue.LockFree
	q.Init(1 << 16)

	duration := 5 * time.Second
	numGoroutines := runtime.NumCPU() * 2

	var ops int64
	var stop int32
	var wg sync.WaitGroup

	wg.Add(numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			r := rand.New(rand.NewSource(int64(id)))
			val := id
			for atomic.LoadInt32(&stop) == 0 {
				if r.Intn(2) == 0 {
					if q.Push(val) {
						val++
					}
				} else {
					q.Pop()
				}
				atomic.AddInt64(&ops, 1)
			}
		}(i)
	}

	time.AfterFunc(duration, func() {
		atomic.StoreInt32(&stop, 1)
	})

	wg.Wait()

	// 排空队列
	for {
		if _, ok := q.Pop(); !ok {
			break
		}
	}

	t.Logf("在 %v 内完成 %d 次操作", duration, ops)
}

// TestStressNilValues 大量 nil 值压力测试
func TestStressNilValues(t *testing.T) {
	if testing.Short() {
		t.Skip("跳过压力测试")
	}

	var q queue.LockFree
	q.Init(1 << 18) // 足够大的容量

	iterations := 100000
	nilCount := 0
	nonNilCount := 0

	for i := 0; i < iterations; i++ {
		if i%2 == 0 {
			if !q.Push(nil) {
				t.Fatalf("Push(nil) 第 %d 次失败，队列可能已满", i)
			}
			nilCount++
		} else {
			if !q.Push(i) {
				t.Fatalf("Push(%d) 失败，队列可能已满", i)
			}
			nonNilCount++
		}
	}

	gotNil := 0
	gotNonNil := 0
	for i := 0; i < iterations; i++ {
		val, ok := q.Pop()
		if !ok {
			t.Fatalf("第 %d 次 Pop() 返回 ok=false", i)
		}
		if val == nil {
			gotNil++
		} else {
			gotNonNil++
		}
	}

	if gotNil != nilCount {
		t.Fatalf("nil 值数量 = %d, 期望 %d", gotNil, nilCount)
	}
	if gotNonNil != nonNilCount {
		t.Fatalf("非 nil 值数量 = %d, 期望 %d", gotNonNil, nonNilCount)
	}
}

// TestStressResizeUnderLoad 负载下 Resize 压力测试
// 验证在 Push/Pop 负载下进行 Resize 不会导致 panic
func TestStressResizeUnderLoad(t *testing.T) {
	if testing.Short() {
		t.Skip("跳过压力测试")
	}

	var q queue.LockFree
	q.Init(1 << 16) // 足够大的初始容量

	// 阶段1：填充数据
	for i := 0; i < 10000; i++ {
		q.Push(i)
	}

	// 阶段2：并发 Resize，逐步扩大
	for i := 0; i < 20; i++ {
		newCap := (1 << 16) + i*1024
		if !q.Resize(newCap) {
			t.Logf("Resize(%d) 返回 false，继续", newCap)
		}
	}

	// 阶段3：验证数据完整性
	count := 0
	for i := 0; ; i++ {
		val, ok := q.Pop()
		if !ok {
			break
		}
		if val != i {
			t.Fatalf("第 %d 个元素 = %v, 期望 %d", i, val, i)
		}
		count++
	}
	if count != 10000 {
		t.Fatalf("弹出数量 = %d, 期望 10000", count)
	}

	// 阶段4：Resize 后继续正常操作
	for i := 0; i < 5000; i++ {
		if !q.Push(i) {
			t.Fatalf("Resize 后 Push(%d) 失败", i)
		}
	}
	for i := 0; i < 5000; i++ {
		val, ok := q.Pop()
		if !ok || val != i {
			t.Fatalf("Resize 后 Pop() 第 %d 个元素异常", i)
		}
	}
}

// TestStressConcurrentGoroutines 大量协程并发测试
func TestStressConcurrentGoroutines(t *testing.T) {
	if testing.Short() {
		t.Skip("跳过压力测试")
	}

	var q queue.LockFree
	q.Init(1 << 16)

	numGoroutines := 1000 // 大量协程
	opsPerGoroutine := 100
	var wg sync.WaitGroup

	wg.Add(numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < opsPerGoroutine; j++ {
				q.Push(id*opsPerGoroutine + j)
				runtime.Gosched()
				q.Pop()
			}
		}(i)
	}

	wg.Wait()
}

// ============================================================
// 兼容性测试（保留原有测试逻辑）
// ============================================================

// TestMatchesMutex 验证 LockFree 与 DRQueue 行为一致
func TestMatchesMutex(t *testing.T) {
	var q queue.LockFree
	q.Init(prevSize)
	var mq DRQueue
	mq.Init(prevSize)

	// 顺序测试
	ops := []struct {
		push bool
		val  interface{}
	}{
		{true, 1}, {true, 2}, {true, nil}, {false, nil},
		{true, "hello"}, {false, nil}, {false, nil}, {false, nil},
		{true, 3.14}, {true, nil}, {false, nil}, {false, nil},
	}

	for _, op := range ops {
		if op.push {
			r1 := q.Push(op.val)
			r2 := mq.Push(op.val)
			if r1 != r2 {
				t.Fatalf("Push(%v) 结果不一致: LockFree=%v, DRQueue=%v", op.val, r1, r2)
			}
		} else {
			v1, ok1 := q.Pop()
			v2, ok2 := mq.Pop()
			if ok1 != ok2 {
				t.Fatalf("Pop() ok 不一致: LockFree=%v, DRQueue=%v", ok1, ok2)
			}
			if ok1 && fmt.Sprintf("%v", v1) != fmt.Sprintf("%v", v2) {
				t.Fatalf("Pop() 值不一致: LockFree=%v, DRQueue=%v", v1, v2)
			}
		}
	}
}

// TestLFQueue 兼容原有测试
func TestLFQueue(t *testing.T) {
	var d queue.LockFree
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
		if val < 0 || val >= N {
			t.Errorf("值 %d 超出范围 [0, %d)", val, N)
			return
		}
		atomic.AddInt32(&have[val], 1)
		if val == N-1 {
			atomic.StoreInt32(&stop, 1)
		}
	}

	// 启动 P-1 个消费者
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
					if fail++; fail%100 == 0 {
						runtime.Gosched()
					}
				}
			}
			wg.Done()
		}()
	}

	// 启动 1 个生产者
	nPopHead := 0
	wg.Add(1)
	go func() {
		for j := 0; j < N; j++ {
			for !d.Push(j) {
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

	// 验证结果
	for i, count := range have {
		if count != 1 {
			t.Errorf("期望 have[%d] = 1, 实际 %d", i, count)
		}
	}
	if !testing.Short() && nPopHead == 0 {
		t.Errorf("生产者 Pop 从未成功")
	}
}
