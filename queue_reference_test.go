package queue_test

import (
	"sync"
	"sync/atomic"
	"unsafe"
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
	prevEnQueueSize = 1 << 20 // queue previous EnQueue
)

// Interface use in stack,queue testing
type Interface interface {
	Size() int
	Init()
	OnceInit(cap int)
	EnQueue(interface{}) bool
	DeQueue() (interface{}, bool)
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
// EnQUeue,DeQueue操作时，先操作slot增改value
// 操作完成后移动deID,enID.
// 队列空条件为deID==enID
// 满条件enID^cap==deID
//
// DRQueue is an unbounded queue which uses a slice as underlying.
type DRQueue struct {
	once sync.Once
	deMu sync.Mutex
	enMu sync.Mutex

	count uint32 // number of element in queue
	cap   uint32 // 队列容量，自动向上调整至2^n
	mod   uint32 // cap-1,即2^n-1,用作取slot: data[ID&mod]
	deID  uint32 // 指向下次取出数据的位置:deID&mod
	enID  uint32 // 指向下次写入数据的位置:enID&mod

	// 环形队列，大小必须是2的倍数。
	// val为空，表示可以EnQUeue,如果是DeQueue操作，表示队列空。
	// val不为空，表所可以DeQueue,如果是EnQUeue操作，表示队列满了。
	// 只能由EnQUeue将val从nil变成非nil,
	// 只能由DeQueue将val从非nil变成nil.
	data []entry
}

func (q *DRQueue) onceInit(cap int) {
	q.once.Do(func() {
		if cap < 1 {
			cap = 1 << 8
		}
		mod := modUint32(uint32(cap))
		q.mod = mod
		q.cap = mod + 1
		q.data = make([]entry, mod+1)
	})
}

// OnceInit initialize queue use cap
// it only execute once time.
// if cap<1, will use 256.
func (q *DRQueue) OnceInit(cap int) {
	q.onceInit(cap)
}

// Init initialize queue use default size: 256
// it only execute once time.
func (q *DRQueue) Init() {
	q.onceInit(1 << 8)
}

// Cap return queue's cap
func (q *DRQueue) Cap() int {
	return int(atomic.LoadUint32(&q.cap))
}

// Empty return queue if empty
func (q *DRQueue) Empty() bool {
	return atomic.LoadUint32(&q.count) == 0
}

// Full return queue if full
func (q *DRQueue) Full() bool {
	return atomic.LoadUint32(&q.count) == atomic.LoadUint32(&q.cap)
}

// Size return current number in queue
func (q *DRQueue) Size() int {
	return int(atomic.LoadUint32(&q.count))
}

// 根据enID,deID获取进队，出队对应的slot
func (q *DRQueue) getSlot(id uint32) *entry {
	return &q.data[id&atomic.LoadUint32(&q.mod)]
}

// EnQueue put value into queue,
// it return true if success,or false if queue full.
func (q *DRQueue) EnQueue(val interface{}) bool {
	q.Init()
	if q.Full() {
		return false
	}
	q.enMu.Lock()
	defer q.enMu.Unlock()
	if q.Full() {
		return false
	}
	slot := q.getSlot(q.enID)
	if slot.load() != nil {
		// 队列满了
		return false
	}
	if val == nil {
		val = empty
	}
	atomic.AddUint32(&q.enID, 1)
	slot.store(val)
	atomic.AddUint32(&q.count, 1)
	return true
}

// DeQueue get the frist element in queue,
// it return true if success,or false if queue empty.
func (q *DRQueue) DeQueue() (val interface{}, ok bool) {
	q.Init()
	if q.Empty() {
		return nil, false
	}
	q.deMu.Lock()
	defer q.deMu.Unlock()
	if q.Empty() {
		return nil, false
	}
	slot := q.getSlot(q.deID)
	if slot.load() == nil {
		// EnQueue正在写入
		return nil, false
	}
	val = slot.load()
	if val == empty {
		val = nil
	}
	atomic.AddUint32(&q.deID, 1)
	slot.free()
	atomic.AddUint32(&q.count, ^uint32(0))
	return val, true
}
