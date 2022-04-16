package queue_test

import (
	"sync"
	"sync/atomic"
	"unsafe"

	"github.com/min1324/queue"
)

// use for slice
const (
	bit  = 3
	mod  = 1<<bit - 1
	null = ^uintptr(0) // -1

	/*
		1<< 20~28
		1048576		20
		2097152		21
		4194304		22
		8388608		23
		16777216	24
		33554432	25
		67108864	26
		134217728	27
		268435456	28
	*/
	prevSize = 1 << 23 // queue previous Push
)

// Interface use in stack,queue testing
type Interface interface {
	queue.Queue
}

// 溢出环形计算需要，得出2^n-1。(2^n>=u,具体可见kfifo）
func modUint32(u uint32) uint32 {
	u -= 1 //兼容0, as min as ,128->127 !255
	u |= u >> 1
	u |= u >> 2
	u |= u >> 4
	u |= u >> 8  // 32位类型已经足够
	u |= u >> 16 // 64位
	return u
}

// 包装nil值。
var empty = new(interface{})

// entry queue element
type entry struct {
	p unsafe.Pointer
}

func (n *entry) load() interface{} {
	p := atomic.LoadPointer(&n.p)
	if p == nil {
		return nil
	}
	return *(*interface{})(p)
}

func (n *entry) store(i interface{}) {
	atomic.StorePointer(&n.p, unsafe.Pointer(&i))
}

func (n *entry) free() {
	atomic.StorePointer(&n.p, nil)
}

// 双锁环形队列,有固定数组
// 游标采取先操作，后移动方案。
// Push,Pop操作时，先操作slot增改value
// 操作完成后移动tail,head.
// 队列空条件为tail==head
// 满条件head^cap==tail
//
// DRQueue is an unbounded queue which uses a slice as underlying.
type DRQueue struct {
	once sync.Once
	deMu sync.Mutex
	enMu sync.Mutex

	count uint32 // number of element in queue
	tail  uint32 // 指向下次取出数据的位置:tail&mod
	head  uint32 // 指向下次写入数据的位置:head&mod

	// 环形队列，大小必须是2的倍数。
	// val为空，表示可以Push,如果是Pop操作，表示队列空。
	// val不为空，表所可以Pop,如果是Push操作，表示队列满了。
	// 只能由Push将val从nil变成非nil,
	// 只能由Pop将val从非nil变成nil.
	data []entry
}

func (q *DRQueue) Init(cap int) {
	q.once.Do(func() {
		if cap < 1 {
			cap = 1 << 8
		}
		mod := modUint32(uint32(cap))
		q.data = make([]entry, mod+1)
	})
}

// Cap return queue's cap
func (q *DRQueue) Cap() int {
	return len(q.data)
}

// Size return current number in queue
func (q *DRQueue) Len() int {
	return int(atomic.LoadUint32(&q.count))
}

// Push put value into queue,
// it return true if success,or false if queue full.
func (q *DRQueue) Push(val interface{}) bool {
	if q.data == nil {
		q.Init(1 << 8)
	}
	head := atomic.LoadUint32(&q.head)
	tail := atomic.LoadUint32(&q.tail)
	if head == tail+uint32(len(q.data)) {
		return false
	}
	q.enMu.Lock()
	defer q.enMu.Unlock()
	head = atomic.LoadUint32(&q.head)
	tail = atomic.LoadUint32(&q.tail)
	if head == tail+uint32(len(q.data)) {
		return false
	}
	slot := &q.data[head&uint32(len(q.data)-1)]
	if slot.load() != nil {
		// 队列满了
		return false
	}
	if val == nil {
		val = empty
	}
	atomic.AddUint32(&q.head, 1)
	slot.store(val)
	atomic.AddUint32(&q.count, 1)
	return true
}

// Pop get the frist element in queue,
// it return true if success,or false if queue empty.
func (q *DRQueue) Pop() (val interface{}, ok bool) {
	head := atomic.LoadUint32(&q.head)
	tail := atomic.LoadUint32(&q.tail)
	if head == tail {
		return
	}
	q.deMu.Lock()
	defer q.deMu.Unlock()
	head = atomic.LoadUint32(&q.head)
	tail = atomic.LoadUint32(&q.tail)
	if head == tail {
		return
	}
	slot := &q.data[tail&uint32(len(q.data)-1)]
	if slot.load() == nil {
		// Push正在写入
		return nil, false
	}
	val = slot.load()
	if val == empty {
		val = nil
	}
	atomic.AddUint32(&q.tail, 1)
	slot.free()
	atomic.AddUint32(&q.count, ^uint32(0))
	return val, true
}
