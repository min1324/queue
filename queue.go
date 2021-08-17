package queue

import (
	"sync"
	"sync/atomic"
	"unsafe"
)

// 包装nil值。
var empty = new(interface{})

// LRQueue is a lock-free ring array queue.
type LRQueue struct {
	once sync.Once

	count uint32 // number of element in queue
	cap   uint32 // 队列容量，自动向上调整至2^n
	mod   uint32 // cap-1,即2^n-1,用作取slot: data[ID&mod]
	deID  uint32 // 指向下次取出数据的位置:deID&mod
	enID  uint32 // 指向下次写入数据的位置:enID&mod

	// 环形队列，大小必须是2的倍数。
	// val为空，表示可以EnQUeue,如果是DeQueue操作，表示队列空。
	// val不为空，表所可以DeQueue,如果是EnQUeue操作，表示队列满了。
	// 并且只能由EnQUeue将val从nil变成非nil,
	// 只能由DeQueue将val从非niu变成nil.
	data []entry
}

func (q *LRQueue) onceInit(cap int) {
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
// if cap<1, will use 1<<8.
func (q *LRQueue) OnceInit(cap int) {
	q.onceInit(cap)
}

// Init initialize queue use default size: 1<<8
// it only execute once time.
func (q *LRQueue) Init() {
	q.onceInit(1 << 8)
}

// Size return current element in queue
func (q *LRQueue) Size() int {
	return int(atomic.LoadUint32(&q.count))
}

// 根据enID,deID获取进队，出队对应的slot
func (q *LRQueue) getSlot(id uint32) *entry {
	return &q.data[id&atomic.LoadUint32(&q.mod)]
}

// EnQueue put value into queue,
// it return true if success,or false if queue full.
func (q *LRQueue) EnQueue(value interface{}) bool {
	q.Init()
	if value == nil {
		value = empty
	}
	for {
		enID := atomic.LoadUint32(&q.enID)
		if q.Full() {
			return false
		}
		slot := q.getSlot(enID)
		if slot.load() != nil {
			// queue full,
			return false
		}
		if casUint32(&q.enID, enID, enID+1) {
			slot.store(value)
			atomic.AddUint32(&q.count, 1)
			break
		}
	}
	return true
}

// DeQueue get the frist element in queue,
// it return true if success,or false if queue empty.
func (q *LRQueue) DeQueue() (value interface{}, ok bool) {
	q.Init()
	for {
		deID := atomic.LoadUint32(&q.deID)
		if q.Empty() {
			return nil, false
		}
		slot := q.getSlot(deID)
		value = slot.load()
		if value == nil {
			// enqueue not yet success,queue empty
			return nil, false
		}
		if casUint32(&q.deID, deID, deID+1) {
			slot.free()
			atomic.AddUint32(&q.count, ^uint32(0))
			break
		}
	}
	if value == empty {
		value = nil
	}
	return value, true
}

// Cap return queue's cap
func (q *LRQueue) Cap() int {
	return int(atomic.LoadUint32(&q.cap))
}

// Empty return queue if empty
func (q *LRQueue) Empty() bool {
	return atomic.LoadUint32(&q.count) == 0
}

// Full return queue if full
func (q *LRQueue) Full() bool {
	return atomic.LoadUint32(&q.count) == atomic.LoadUint32(&q.cap)
}

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

func casUint32(p *uint32, old, new uint32) bool {
	return atomic.CompareAndSwapUint32(p, old, new)
}
