# Queue 性能测试报告

## 测试环境

| 项目 | 值 |
|------|------|
| 操作系统 | Windows |
| 架构 | amd64 |
| CPU | 12th Gen Intel(R) Core(TM) i5-12490F |
| 核心数 | 12 |
| Go 版本 | go1.x |
| 测试时间 | benchtime=500ms |

## 测试对象

| 队列实现 | 说明 |
|----------|------|
| **LockFree** | 无锁环形队列，基于 CAS 原子操作 |
| **DRQueue** | 双锁环形队列，入队/出队各一把互斥锁 |

---

## 1. 并行操作（LockFree vs DRQueue）

### 1.1 Push 性能

| 场景 | LockFree | DRQueue | LockFree 优势 |
|------|----------|---------|--------------|
| Push (满队列) | 2.594 ns/op, 8 B/op, 0 allocs | 3.338 ns/op, 8 B/op, 0 allocs | 快 29% |
| Push (空队列) | 66.92 ns/op, 7 B/op, 0 allocs | 81.22 ns/op, 24 B/op, 1 alloc | 快 21% |

### 1.2 Pop 性能

| 场景 | LockFree | DRQueue | LockFree 优势 |
|------|----------|---------|--------------|
| Pop (空队列) | 0.2573 ns/op, 0 B/op, 0 allocs | 0.2612 ns/op, 0 B/op, 0 allocs | 持平 |
| Pop (满队列) | 3.061 ns/op, 0 B/op, 0 allocs | 4.498 ns/op, 0 B/op, 0 allocs | 快 47% |

### 1.3 混合操作

| 场景 | LockFree | DRQueue | LockFree 优势 |
|------|----------|---------|--------------|
| 均衡 Push/Pop (1:1) | 140.0 ns/op, 8 B/op, 0 allocs | 230.8 ns/op, 24 B/op, 1 alloc | **快 65%** |
| Push 密集 (3:1) | 266.8 ns/op, 24 B/op, 2 allocs | 302.4 ns/op, 71 B/op, 5 allocs | 快 13% |
| Pop 密集 (1:3) | 242.2 ns/op, 7 B/op, 0 allocs | 282.6 ns/op, 23 B/op, 1 alloc | 快 17% |

---

## 2. 不同容量（并行 Push）

| 容量 | LockFree | DRQueue | LockFree 优势 |
|------|----------|---------|--------------|
| 64 (Small) | 63.36 ns/op, 7 B/op, 0 allocs | 116.2 ns/op, 15 B/op, 1 alloc | **快 83%** |
| 4096 (Medium) | 64.17 ns/op, 7 B/op, 0 allocs | 109.8 ns/op, 16 B/op, 1 alloc | **快 71%** |
| 1M (Large) | 68.11 ns/op, 7 B/op, 0 allocs | 105.2 ns/op, 17 B/op, 1 alloc | **快 54%** |

> 容量对 LockFree 性能影响极小（63~68 ns），DRQueue 在小容量下竞争更激烈（116 ns）。

---

## 3. 单协程操作

| 操作 | LockFree | DRQueue | LockFree 优势 |
|------|----------|---------|--------------|
| Push | 12.74 ns/op, 7 B/op, 0 allocs | 30.07 ns/op, 15 B/op, 1 alloc | **快 136%** |
| Pop | 1.611 ns/op, 0 B/op, 0 allocs | 1.961 ns/op, 0 B/op, 0 allocs | 快 22% |
| Push+Pop | 43.90 ns/op, 7 B/op, 0 allocs | 68.96 ns/op, 23 B/op, 1 alloc | **快 57%** |

> 单协程场景下 LockFree 优势最大，Push 操作快 136%，主要因为无锁路径避免了 mutex 开销。

---

## 4. 生产者-消费者模式

| 模式 | LockFree | DRQueue | LockFree 优势 |
|------|----------|---------|--------------|
| 单生产者-单消费者 | 65.25 ns/op, 7 B/op, 0 allocs | 135.7 ns/op, 23 B/op, 1 alloc | **快 108%** |
| 多生产者-多消费者 | 133.1 ns/op, 7 B/op, 0 allocs | 220.7 ns/op, 23 B/op, 1 alloc | **快 66%** |

> 生产者-消费者是队列最典型的使用场景，LockFree 优势非常显著。

---

## 5. Resize

| 场景 | LockFree | DRQueue | 优势方 |
|------|----------|---------|--------|
| Resize (空队列) | 2556 ns/op, 23360 B/op, 3 allocs | 1433 ns/op, 11840 B/op, 3 allocs | **DRQueue 快 78%** |
| Resize (100元素) | 4842 ns/op, 23360 B/op, 3 allocs | 5130 ns/op, 13440 B/op, 103 allocs | LockFree 快 6% |

> 空队列 Resize 时 DRQueue 更快，因为双锁设计 Resize 只需获取两把锁。
> 带元素 Resize 时 DRQueue 的 entry 类型在迁移时产生大量堆分配（103 allocs），LockFree 反超。

---

## 6. 综合对比

### 6.1 速度对比总览

```
                    LockFree vs DRQueue 速度比
    ┌──────────────────────────────────────────────┐
    │                                              │
    │  单协程 Push        ████████████████████ 136% │
    │  生产者-消费者       ████████████████   108%  │
    │  小容量 Push         ████████████       83%   │
    │  中容量 Push         █████████         71%    │
    │  多生产者-消费者     █████████         66%    │
    │  均衡 Push/Pop      █████████         65%    │
    │  大容量 Push         ███████           54%    │
    │  单协程 PushPop      ███████           57%    │
    │  Pop (满队列)        █████             47%    │
    │  Push (满队列)       ███               29%    │
    │  单协程 Pop          ██                22%    │
    │  Push (空队列)       ██                21%    │
    │  Pop (空队列)        █                  0%    │
    │  Resize (空)         ████             -78%    │
    │  Resize (100元素)    █                  6%    │
    │                                              │
    └──────────────────────────────────────────────┘
```

### 6.2 内存分配对比

| 指标 | LockFree | DRQueue |
|------|----------|---------|
| Push 堆分配 | **0 次** | 1 次/操作 |
| Pop 堆分配 | 0 次 | 0 次 |
| Push 内存/操作 | 7~8 B | 15~24 B |
| Resize 内存 (空) | 23360 B | 11840 B |
| Resize 内存 (100元素) | 23360 B | 13440 B |
| Resize 堆分配 (100元素) | 3 次 | **103 次** |

### 6.3 关键发现

1. **LockFree 在几乎所有操作场景下均大幅优于 DRQueue**
   - 单协程 Push 快 **136%**，生产者-消费者快 **108%**
   - 小容量高竞争场景优势更明显（快 83%）

2. **内存分配优势显著**
   - LockFree 的 Push 操作**零堆分配**，DRQueue 每次 Push 有 1 次堆分配
   - 这对 GC 延迟敏感的应用至关重要

3. **容量对性能影响很小**
   - LockFree：63~68 ns（波动 8%）
   - DRQueue：105~116 ns（波动 10%）

4. **唯一例外：空队列 Resize**
   - DRQueue 快 78%，因为双锁设计 Resize 只需获取两把锁
   - 但带元素 Resize 时 DRQueue 产生 103 次堆分配，LockFree 反超

5. **Pop 空队列两者性能持平**
   - 均约 0.26 ns，因为空队列 Pop 直接返回，几乎无开销

---

## 7. 原始测试数据

```
goos: windows
goarch: amd64
pkg: github.com/min1324/queue
cpu: 12th Gen Intel(R) Core(TM) i5-12490F

BenchmarkPushFull/*queue.LockFree-12          	228117538	         2.594 ns/op	       8 B/op	       0 allocs/op
BenchmarkPushFull/*queue_test.DRQueue-12      	196976152	         3.338 ns/op	       8 B/op	       0 allocs/op
BenchmarkPushEmpty/*queue.LockFree-12         	 8610148	        66.92 ns/op	       7 B/op	       0 allocs/op
BenchmarkPushEmpty/*queue_test.DRQueue-12     	 7279035	        81.22 ns/op	      24 B/op	       1 allocs/op
BenchmarkPopEmpty/*queue.LockFree-12          	1000000000	         0.2573 ns/op	       0 B/op	       0 allocs/op
BenchmarkPopEmpty/*queue_test.DRQueue-12      	1000000000	         0.2612 ns/op	       0 B/op	       0 allocs/op
BenchmarkPopFull/*queue.LockFree-12           	164033074	         3.061 ns/op	       0 B/op	       0 allocs/op
BenchmarkPopFull/*queue_test.DRQueue-12       	111452476	         4.498 ns/op	       0 B/op	       0 allocs/op
BenchmarkBalance/*queue.LockFree-12           	 4353794	       140.0 ns/op	       8 B/op	       0 allocs/op
BenchmarkBalance/*queue_test.DRQueue-12       	 2671454	       230.8 ns/op	      24 B/op	       1 allocs/op
BenchmarkPushHeavy/*queue.LockFree-12         	 2224339	       266.8 ns/op	      24 B/op	       2 allocs/op
BenchmarkPushHeavy/*queue_test.DRQueue-12     	 1960372	       302.4 ns/op	      71 B/op	       5 allocs/op
BenchmarkPopHeavy/*queue.LockFree-12          	 2495301	       242.2 ns/op	       7 B/op	       0 allocs/op
BenchmarkPopHeavy/*queue_test.DRQueue-12      	 2036148	       282.6 ns/op	      23 B/op	       1 allocs/op
BenchmarkPushCapSmall_LockFree-12             	 8856584	        63.36 ns/op	       7 B/op	       0 allocs/op
BenchmarkPushCapSmall_DRQueue-12              	 5216053	       116.2 ns/op	      15 B/op	       1 allocs/op
BenchmarkPushCapMedium_LockFree-12            	 8844756	        64.17 ns/op	       7 B/op	       0 allocs/op
BenchmarkPushCapMedium_DRQueue-12             	 5445144	       109.8 ns/op	      16 B/op	       1 allocs/op
BenchmarkPushCapLarge_LockFree-12             	 8901324	        68.11 ns/op	       7 B/op	       0 allocs/op
BenchmarkPushCapLarge_DRQueue-12              	 7132141	       105.2 ns/op	      17 B/op	       1 allocs/op
BenchmarkPush1Goroutine_LockFree-12           	40343138	        12.74 ns/op	       7 B/op	       0 allocs/op
BenchmarkPush1Goroutine_DRQueue-12            	17741679	        30.07 ns/op	      15 B/op	       1 allocs/op
BenchmarkPop1Goroutine_LockFree-12            	335956231	         1.611 ns/op	       0 B/op	       0 allocs/op
BenchmarkPop1Goroutine_DRQueue-12             	271358017	         1.961 ns/op	       0 B/op	       0 allocs/op
BenchmarkPushPop1Goroutine_LockFree-12        	13694034	        43.90 ns/op	       7 B/op	       0 allocs/op
BenchmarkPushPop1Goroutine_DRQueue-12         	 9085897	        68.96 ns/op	      23 B/op	       1 allocs/op
BenchmarkProducerConsumer_LockFree-12         	 9373681	        65.25 ns/op	       7 B/op	       0 allocs/op
BenchmarkProducerConsumer_DRQueue-12          	 4578439	       135.7 ns/op	      23 B/op	       1 allocs/op
BenchmarkMultiProducerConsumer_LockFree-12    	 4505634	       133.1 ns/op	       7 B/op	       0 allocs/op
BenchmarkMultiProducerConsumer_DRQueue-12     	 2734551	       220.7 ns/op	      23 B/op	       1 allocs/op
BenchmarkResize_LockFree-12                   	  243396	      2556 ns/op	   23360 B/op	       3 allocs/op
BenchmarkResize_DRQueue-12                    	  429648	      1433 ns/op	   11840 B/op	       3 allocs/op
BenchmarkResizeWithElements_LockFree-12       	  128497	      4842 ns/op	   23360 B/op	       3 allocs/op
BenchmarkResizeWithElements_DRQueue-12        	  110998	      5130 ns/op	   13440 B/op	     103 allocs/op
```
