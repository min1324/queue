package queue_test

// // ============================================================
// // 不同容量基准测试
// // ============================================================

// // BenchmarkPushCapSmall_LockFree 小容量队列 LockFree Push 性能
// func BenchmarkPushCapSmall_LockFree(b *testing.B) {
// 	bPushCapSmall(b, &queue.LockFree{})
// }

// func bPushCapSmall(b *testing.B, q Interface) {
// 	q.Init(64)
// 	b.ResetTimer()
// 	b.RunParallel(func(pb *testing.PB) {
// 		i := 0
// 		for pb.Next() {
// 			if !q.Push(i) {
// 				q.Pop()
// 			}
// 			i++
// 		}
// 	})
// }

// // BenchmarkPushCapMedium_LockFree 中容量队列 LockFree Push 性能
// func BenchmarkPushCapMedium_LockFree(b *testing.B) {
// 	bPushCapMedium(b, &queue.LockFree{})
// }
// func bPushCapMedium(b *testing.B, q Interface) {
// 	q.Init(4096)
// 	b.ResetTimer()
// 	b.RunParallel(func(pb *testing.PB) {
// 		i := 0
// 		for pb.Next() {
// 			if !q.Push(i) {
// 				q.Pop()
// 			}
// 			i++
// 		}
// 	})
// }

// // BenchmarkPushCapLarge_LockFree 大容量队列 LockFree Push 性能
// func BenchmarkPushCapLarge_LockFree(b *testing.B) {
// 	bPushCapLarge(b, &queue.LockFree{})
// }
// func bPushCapLarge(b *testing.B, q Interface) {
// 	q.Init(1 << 20)
// 	b.ResetTimer()
// 	b.RunParallel(func(pb *testing.PB) {
// 		i := 0
// 		for pb.Next() {
// 			if !q.Push(i) {
// 				q.Pop()
// 			}
// 			i++
// 		}
// 	})
// }

// // ============================================================
// // 不同并发度基准测试
// // ============================================================

// // BenchmarkPush1Goroutine_LockFree 单协程 LockFree Push 性能
// func BenchmarkPush1Goroutine_LockFree(b *testing.B) {
// 	bPush1Goroutine(b, &queue.LockFree{})
// }
// func bPush1Goroutine(b *testing.B, q Interface) {
// 	q.Init(prevSize)
// 	b.ResetTimer()
// 	for i := 0; i < b.N; i++ {
// 		q.Push(i)
// 	}
// }

// // BenchmarkPop1Goroutine_LockFree 单协程 LockFree Pop 性能
// func BenchmarkPop1Goroutine_LockFree(b *testing.B) {
// 	bPop1Goroutine(b, &queue.LockFree{})
// }
// func bPop1Goroutine(b *testing.B, q Interface) {
// 	q.Init(prevSize)
// 	for i := 0; i < prevSize; i++ {
// 		q.Push(i)
// 	}
// 	b.ResetTimer()
// 	for i := 0; i < b.N; i++ {
// 		q.Pop()
// 	}
// }

// // BenchmarkPushPop1Goroutine_LockFree 单协程 LockFree 交替 Push/Pop 性能
// func BenchmarkPushPop1Goroutine_LockFree(b *testing.B) {
// 	bPushPop1Goroutine(b, &queue.LockFree{})
// }

// func bPushPop1Goroutine(b *testing.B, q Interface) {
// 	q.Init(prevSize)
// 	b.ResetTimer()
// 	for i := 0; i < b.N; i++ {
// 		q.Push(i)
// 		q.Pop()
// 	}
// }

// // ============================================================
// // 生产者-消费者模式基准测试
// // ============================================================

// // BenchmarkProducerConsumer_LockFree 经典生产者-消费者模式 (LockFree)
// func BenchmarkProducerConsumer_LockFree(b *testing.B) {
// 	bProducerConsumer(b, &queue.LockFree{})
// }

// func bProducerConsumer(b *testing.B, q Interface) {
// 	q.Init(prevSize)

// 	var wg sync.WaitGroup

// 	b.ResetTimer()

// 	// 生产者
// 	wg.Add(1)
// 	go func() {
// 		defer wg.Done()
// 		for i := 0; i < b.N; i++ {
// 			for !q.Push(i) {
// 				runtime.Gosched()
// 			}
// 		}
// 	}()

// 	// 消费者
// 	wg.Add(1)
// 	go func() {
// 		defer wg.Done()
// 		for i := 0; i < b.N; i++ {
// 			for {
// 				if _, ok := q.Pop(); ok {
// 					break
// 				}
// 				runtime.Gosched()
// 			}
// 		}
// 	}()

// 	wg.Wait()
// }

// // BenchmarkMultiProducerConsumer_LockFree 多生产者多消费者模式 (LockFree)
// func BenchmarkMultiProducerConsumer_LockFree(b *testing.B) {
// 	bMultiProducerConsumer(b, &queue.LockFree{})
// }

// func bMultiProducerConsumer(b *testing.B, q Interface) {
// 	q.Init(prevSize)

// 	b.ResetTimer()
// 	b.RunParallel(func(pb *testing.PB) {
// 		i := 0
// 		for pb.Next() {
// 			for !q.Push(i) {
// 				runtime.Gosched()
// 			}
// 			q.Pop()
// 			i++
// 		}
// 	})
// }

// // 性能测试：测量批量操作的吞吐量
// func BenchmarkPushBatch_LockFree(b *testing.B) {
// 	bPushBatch(b, &queue.LockFree{})
// }

// func bPushBatch(b *testing.B, q Interface) {
// 	const queueSize = 1000
// 	q.Init(queueSize)
// 	batchSize := 100

// 	b.ResetTimer()
// 	for i := 0; i < b.N; i++ {
// 		vals := make([]any, batchSize)
// 		for j := 0; j < batchSize; j++ {
// 			vals[j] = i*batchSize + j
// 		}
// 		q.PushBetch(vals...)
// 	}
// }
// func BenchmarkPopBatch_LockFree(b *testing.B) {
// 	bPopBatch(b, &queue.LockFree{})
// }

// func bPopBatch(b *testing.B, q Interface) {
// 	const queueSize = 1000
// 	q.Init(queueSize)
// 	batchSize := 100

// 	// 预填充数据
// 	vals := make([]any, queueSize)
// 	for i := 0; i < queueSize; i++ {
// 		vals[i] = i
// 	}
// 	q.PushBetch(vals...)

// 	b.ResetTimer()
// 	for i := 0; i < b.N; i++ {
// 		q.PopBetch(batchSize)
// 	}
// }

// func BenchmarkPushPopBatchConcurrent_LockFree(b *testing.B) {
// 	bPushPopBatchConcurrent(b, &queue.LockFree{})
// }

// func bPushPopBatchConcurrent(b *testing.B, q Interface) {
// 	const (
// 		queueSize     = 1000
// 		numGoroutines = 4
// 		batchSize     = 10
// 	)

// 	q.Init(queueSize)

// 	b.RunParallel(func(pb *testing.PB) {
// 		i := 0
// 		for pb.Next() {
// 			// 准备批量数据
// 			vals := make([]any, batchSize)
// 			for j := 0; j < batchSize; j++ {
// 				vals[j] = i*batchSize + j
// 			}
// 			i++

// 			// 批量推送
// 			q.PushBetch(vals...)

// 			// 批量弹出
// 			q.PopBetch(batchSize)
// 		}
// 	})
// }
