package queue_test

import (
	"runtime"
	"sync"
	"testing"

	"github.com/min1324/queue"
)

// ============================================================
// 不同容量基准测试
// ============================================================

// BenchmarkPushCapSmall_LockFree 小容量队列 LockFree Push 性能
func BenchmarkPushCapSmall_LockFree(b *testing.B) {
	bPushCapSmall(b, &queue.LockFree{})
}

func BenchmarkPushCapSmall_MuxRing(b *testing.B) {
	bPushCapSmall(b, &queue.MuxRing{})
}

func bPushCapSmall(b *testing.B, q Interface) {
	q.Init(64)
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			if !q.Push(i) {
				q.Pop()
			}
			i++
		}
	})
}

// BenchmarkPushCapMedium_LockFree 中容量队列 LockFree Push 性能
func BenchmarkPushCapMedium_LockFree(b *testing.B) {
	bPushCapMedium(b, &queue.LockFree{})
}
func BenchmarkPushCapMedium_MuxRing(b *testing.B) {
	bPushCapMedium(b, &queue.MuxRing{})
}

func bPushCapMedium(b *testing.B, q Interface) {
	q.Init(4096)
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			if !q.Push(i) {
				q.Pop()
			}
			i++
		}
	})
}

// BenchmarkPushCapLarge_LockFree 大容量队列 LockFree Push 性能
func BenchmarkPushCapLarge_LockFree(b *testing.B) {
	bPushCapLarge(b, &queue.LockFree{})
}
func BenchmarkPushCapLarge_MuxRing(b *testing.B) {
	bPushCapLarge(b, &queue.MuxRing{})
}

func bPushCapLarge(b *testing.B, q Interface) {
	q.Init(1 << 20)
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			if !q.Push(i) {
				q.Pop()
			}
			i++
		}
	})
}

// ============================================================
// 不同并发度基准测试
// ============================================================

// BenchmarkPush1Goroutine_LockFree 单协程 LockFree Push 性能
func BenchmarkPush1Goroutine_LockFree(b *testing.B) {
	bPush1Goroutine(b, &queue.LockFree{})
}
func BenchmarkPush1Goroutine_MuxRing(b *testing.B) {
	bPush1Goroutine(b, &queue.MuxRing{})
}

func bPush1Goroutine(b *testing.B, q Interface) {
	q.Init(b.N)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		q.Push(i)
	}
}

// BenchmarkPop1Goroutine_LockFree 单协程 LockFree Pop 性能
func BenchmarkPop1Goroutine_LockFree(b *testing.B) {
	bPop1Goroutine(b, &queue.LockFree{})
}
func BenchmarkPop1Goroutine_MuxRing(b *testing.B) {
	bPop1Goroutine(b, &queue.MuxRing{})
}

func bPop1Goroutine(b *testing.B, q Interface) {
	q.Init(b.N)
	for i := 0; i < b.N; i++ {
		q.Push(i)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		q.Pop()
	}
}

// BenchmarkPushPop1Goroutine_LockFree 单协程 LockFree 交替 Push/Pop 性能
func BenchmarkPushPop1Goroutine_LockFree(b *testing.B) {
	bPushPop1Goroutine(b, &queue.LockFree{})
}
func BenchmarkPushPop1Goroutine_MuxRing(b *testing.B) {
	bPushPop1Goroutine(b, &queue.MuxRing{})
}

func bPushPop1Goroutine(b *testing.B, q Interface) {
	q.Init(prevCap)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		q.Push(i)
		q.Pop()
	}
}

// ============================================================
// 生产者-消费者模式基准测试
// ============================================================

// BenchmarkProducerConsumer_LockFree 经典生产者-消费者模式 (LockFree)
func BenchmarkProducerConsumer_LockFree(b *testing.B) {
	bProducerConsumer(b, &queue.LockFree{})
}
func BenchmarkProducerConsumer_MuxRing(b *testing.B) {
	bProducerConsumer(b, &queue.MuxRing{})
}

func bProducerConsumer(b *testing.B, q Interface) {
	q.Init(prevCap)

	var wg sync.WaitGroup

	b.ResetTimer()

	// 生产者
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < b.N; i++ {
			for !q.Push(i) {
				runtime.Gosched()
			}
		}
	}()

	// 消费者
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < b.N; i++ {
			for {
				if _, ok := q.Pop(); ok {
					break
				}
				runtime.Gosched()
			}
		}
	}()

	wg.Wait()
}

// BenchmarkMultiProducerConsumer_LockFree 多生产者多消费者模式 (LockFree)
func BenchmarkMultiProducerConsumer_LockFree(b *testing.B) {
	bMultiProducerConsumer(b, &queue.LockFree{})
}
func BenchmarkMultiProducerConsumer_MuxRing(b *testing.B) {
	bMultiProducerConsumer(b, &queue.MuxRing{})
}

func bMultiProducerConsumer(b *testing.B, q Interface) {
	q.Init(prevCap)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			for !q.Push(i) {
				runtime.Gosched()
			}
			q.Pop()
			i++
		}
	})
}

// 性能测试：测量批量操作的吞吐量
func BenchmarkPushBatch_LockFree(b *testing.B) {
	bPushBatch(b, &queue.LockFree{})
}
func BenchmarkPushBatch_MuxRing(b *testing.B) {
	bPushBatch(b, &queue.MuxRing{})
}

func bPushBatch(b *testing.B, q Interface) {
	batchSize := 100
	var queueSize = batchSize * b.N
	q.Init(queueSize)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		vals := make([]any, batchSize)
		for j := 0; j < batchSize; j++ {
			vals[j] = i*batchSize + j
		}
		q.BatchPush(vals...)
	}
}
func BenchmarkPopBatch_LockFree(b *testing.B) {
	bPopBatch(b, &queue.LockFree{})
}
func BenchmarkPopBatch_MuxRing(b *testing.B) {
	bPopBatch(b, &queue.MuxRing{})
}

func bPopBatch(b *testing.B, q Interface) {
	batchSize := 100
	var queueSize = 100 * b.N
	q.Init(queueSize)

	// 预填充数据
	vals := make([]any, queueSize)
	for i := 0; i < queueSize; i++ {
		vals[i] = i
	}
	q.BatchPush(vals...)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		q.BatchPop(batchSize)
	}
}

func BenchmarkPushPopBatchConcurrent_LockFree(b *testing.B) {
	bPushPopBatchConcurrent(b, &queue.LockFree{})
}
func BenchmarkPushPopBatchConcurrent_MuxRing(b *testing.B) {
	bPushPopBatchConcurrent(b, &queue.MuxRing{})
}

func bPushPopBatchConcurrent(b *testing.B, q Interface) {
	var (
		queueSize = 1000
		batchSize = 10
	)

	q.Init(queueSize)

	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			// 准备批量数据
			vals := make([]any, batchSize)
			for j := 0; j < batchSize; j++ {
				vals[j] = i*batchSize + j
			}
			i++

			// 批量推送
			q.BatchPush(vals...)

			// 批量弹出
			q.BatchPop(batchSize)
		}
	})
}

// BenchmarkClean 测试 Clean 方法的性能
func BenchmarkClean_LockFree(b *testing.B) {
	q := &queue.LockFree{}
	q.Init(1000)

	// 预填充数据
	for i := 0; i < 1000; i++ {
		q.Push(i)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		q.Clean()
		// 重新填充数据
		for j := 0; j < 1000; j++ {
			q.Push(j)
		}
	}
}

func BenchmarkClean_MuxRing(b *testing.B) {
	q := &queue.MuxRing{}
	q.Init(1000)

	// 预填充数据
	for i := 0; i < 1000; i++ {
		q.Push(i)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		q.Clean()
		// 重新填充数据
		for j := 0; j < 1000; j++ {
			q.Push(j)
		}
	}
}

// BenchmarkCleanEmpty 测试关闭空队列的性能
func BenchmarkCleanEmpty_LockFree(b *testing.B) {
	q := &queue.LockFree{}
	q.Init(shortTestCap)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		q.Clean()
	}
}

func BenchmarkCleanEmpty_MuxRing(b *testing.B) {
	q := &queue.MuxRing{}
	q.Init(shortTestCap)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		q.Clean()
	}
}
