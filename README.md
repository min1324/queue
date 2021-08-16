# queue

[![Build Status](https://travis-ci.com/min1324/queue.svg?branch=main)](https://travis-ci.com/min1324/queue) [![codecov](https://codecov.io/gh/min1324/queue/branch/main/graph/badge.svg)](https://codecov.io/gh/min1324/queue) [![GoDoc](https://godoc.org/github.com/min1324/queue?status.png)](https://godoc.org/github.com/min1324/queue) [![Go Report Card](https://goreportcard.com/badge/github.com/min1324/queue)](https://goreportcard.com/report/github.com/min1324/queue)

-----

队列(`queue`)是非常常用的一个数据结构，它只允许在队列的前端（`head`）进行出队(`dequeue`)操作，而在队列的后端（`tail`）进行入队(`enqueue`)操作。[**lock-free**][1]的算法都是通过`CAS`操作实现的。

Queue接口：

```go
type Queue interface {
	EnQueue(interface{}) bool
	DeQueue() (val interface{}, ok bool)
}
```

**EnQueue：**

将val加入队尾，返回是否成功。

**DeQueue：**

取出队头val，返回val和是否成功，如果不成功，val为nil。



-----




[1]: https://www.cs.rochester.edu/u/scott/papers/1996_PODC_queues.pdf

