package queue_test

import (
	"runtime"
	"sync"
	"testing"

	"github.com/min1324/queue"
)

// BenchmarkPushCapSmall_LockFree 小容量队列 LockFree Push 性能
func BenchmarkPushCapSmall_DRQueue(b *testing.B) {
	var q queue.DRQueue
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
func BenchmarkPushCapMedium_DRQueue(b *testing.B) {
	var q queue.DRQueue
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
func BenchmarkPushCapLarge_DRQueue(b *testing.B) {
	var q queue.DRQueue
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

// BenchmarkPush1Goroutine_Ring 单协程 LockFree Push 性能
func BenchmarkPush1Goroutine_Ring(b *testing.B) {
	var q queue.DRQueue
	q.Init(prevSize)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		q.Push(i)
	}
}

// BenchmarkPop1Goroutine_Ring 单协程 LockFree Pop 性能
func BenchmarkPop1Goroutine_Ring(b *testing.B) {
	var q queue.DRQueue
	q.Init(prevSize)
	for i := 0; i < prevSize; i++ {
		q.Push(i)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		q.Pop()
	}
}

// BenchmarkPushPop1Goroutine_Ring 单协程 LockFree 交替 Push/Pop 性能
func BenchmarkPushPop1Goroutine_Ring(b *testing.B) {
	var q queue.DRQueue
	q.Init(prevSize)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		q.Push(i)
		q.Pop()
	}
}

// BenchmarkProducerConsumer_LockFree 经典生产者-消费者模式 (LockFree)
func BenchmarkProducerConsumer_DRQueue(b *testing.B) {
	var q queue.DRQueue
	q.Init(prevSize)

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

// BenchmarkMultiProducerConsumer_Ring 多生产者多消费者模式 (LockFree)
func BenchmarkMultiProducerConsumer_Ring(b *testing.B) {
	var q queue.DRQueue
	q.Init(prevSize)

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
