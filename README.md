# queue

[![Build Status](https://travis-ci.com/min1324/queue.svg?branch=main)](https://travis-ci.com/min1324/queue) [![codecov](https://codecov.io/gh/min1324/queue/branch/main/graph/badge.svg)](https://codecov.io/gh/min1324/queue) [![Go Report Card](https://goreportcard.com/badge/github.com/min1324/queue)](https://goreportcard.com/report/github.com/min1324/queue) [![GoDoc](https://godoc.org/github.com/min1324/queue?status.png)](https://godoc.org/github.com/min1324/queue)

一个高性能的无锁环形缓冲区队列的 Go 实现。

## 概述

队列是一种基础数据结构，支持两端操作：
- **Push（入队）**：在队列头部添加元素
- **Pop（出队）**：从队列尾部移除元素

本实现使用基于比较并交换（CAS）操作的[无锁算法][1]，在并发场景下提供卓越的性能，无需互斥锁的开销。

## 特性

- **无锁设计**：Push/Pop 操作无需互斥锁
- **高性能**：针对并发访问模式进行了优化
- **动态调整容量**：支持运行时调整队列容量
- **内存高效**：环形缓冲区实现，开销最小
- **类型安全**：通过 `interface{}` 类型支持任意类型
- **跨平台**：兼容标准 Go 和 TinyGo

## 安装

```bash
go get github.com/min1324/queue
```

## 快速入门

```go
package main

import (
    "fmt"
    "github.com/min1324/queue"
)

func main() {
    // 创建新队列
    q := queue.New()

    // 使用指定容量初始化（可选，首次 Push 时自动初始化）
    q.Init(1024)

    // 入队元素
    q.Push("hello")
    q.Push("world")

    // 出队元素
    if val, ok := q.Pop(); ok {
        fmt.Println(val) // 输出: hello
    }

    if val, ok := q.Pop(); ok {
        fmt.Println(val) // 输出: world
    }

    // 调整队列容量
    q.Resize(2048)

    // 获取队列信息
    fmt.Printf("容量: %d, 长度: %d\n", q.Cap(), q.Len())
}
```

## API 参考

### Queue 接口

```go
type Queue interface {
    Interface

    // Cap 返回队列的容量（最多可容纳的元素数量）。
    // 如果队列未初始化，返回 0。
    Cap() int

    // Len 返回队列中当前的元素数量。
    // 注意：在并发访问下，此值可能是近似值。
    Len() int

    // Init 使用指定容量初始化队列。
    // 此方法只能调用一次，后续调用无效。
    // 如果 cap < 1，将使用默认容量（256）。
    // 如果未显式调用，将在首次 Push 操作时自动调用。
    Init(cap int)

    // Resize 将队列容量更改为 newCap。
    // 如果无法调整容量则返回 false（例如 newCap 小于当前元素数量）。
    // 此方法可安全并发调用，但会阻塞直到所有进行中的 Push/Pop 操作完成。
    Resize(newCap int) bool
}
```

### Interface 接口

```go
type Interface interface {
    // Push 将值添加到队列的头部（写入位置）。
    // 成功添加返回 true，队列已满返回 false。
    // 此操作是无锁的，可安全并发使用。
    Push(value any) bool

    // Pop 从队列的尾部（读取位置）移除并返回元素。
    // 成功返回值和 true，队列为空返回 nil 和 false。
    // 此操作是无锁的，可安全并发使用。
    Pop() (value any, ok bool)
}
```

## 实现细节

### 无锁环形缓冲区

队列使用环形缓冲区实现，具有以下特点：
- **大小**：始终为2的幂次，以实现高效的取模运算
- **头指针**：指向下一个写入位置（Push）
- **尾指针**：指向下一个读取位置（Pop）
- **空槽位检测**：使用 nil 表示空槽位
- **内存排序**：使用原子操作确保正确的内存屏障

### 并发安全

- Push 和 Pop 操作完全并发且无锁
- Resize 操作使用互斥锁确保容量变更安全
- 实现使用运行时 procPin/procUnpin 防止协程在关键 CAS+Store 序列中被抢占
- 对于 TinyGo 和其他替代运行时，提供了基于互斥锁的回退实现

### 性能注意事项

- 队列非满非空时性能最佳
- 遇到竞争时使用自旋等待
- 首次 Push 时自动初始化（如果未显式初始化）
- Resize 操作会阻塞直到所有进行中的操作完成

## 兼容性

- **Go 版本**：已在 Go 1.x 上测试
- **运行时**：
  - 标准 Go（gc）：完整无锁支持
  - TinyGo：基于互斥锁的回退实现
- **平台**：Go 支持的所有平台

## 参考资料

[1]: https://www.cs.rochester.edu/u/scott/papers/1996_PODC_queues.pdf



