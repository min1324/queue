package queue

import (
	"sync"
	"sync/atomic"
	"unsafe"
)

const (
	// initSize init when not provite cap,use initSize
	initSize = 1 << 8
)

// dequeueNil is used in queue to represent interface{}(nil).
// Since we use nil to represent empty slots, we need a sentinel value
// to represent nil.
type queueNil *struct{}

// New return an empty queue.
func New() *Queue {
	return &Queue{}
}

// Queue is a lock-free ring array queue.
type Queue struct {
	once sync.Once

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

func (q *Queue) onceInit(cap int) {
	q.once.Do(func() {
		if cap < 1 {
			cap = initSize
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
func (q *Queue) OnceInit(cap int) {
	q.onceInit(cap)
}

// Init initialize queue use default size: 256
// it only execute once time.
func (q *Queue) Init() {
	q.onceInit(initSize)
}

// Cap return queue's cap
func (q *Queue) Cap() int {
	return int(atomic.LoadUint32(&q.cap))
}

// Empty return queue if empty
func (q *Queue) Empty() bool {
	return atomic.LoadUint32(&q.count) == 0
}

// Full return queue if full
func (q *Queue) Full() bool {
	return atomic.LoadUint32(&q.count) == atomic.LoadUint32(&q.cap)
}

// Size return current number in queue
func (q *Queue) Size() int {
	return int(atomic.LoadUint32(&q.count))
}

// 根据enID,deID获取进队，出队对应的slot
func (q *Queue) getSlot(id uint32) *entry {
	return &q.data[id&atomic.LoadUint32(&q.mod)]
}

// EnQueue put value into queue,
// it return true if success,or false if queue full.
func (q *Queue) EnQueue(value interface{}) bool {
	q.Init()
	if value == nil {
		value = queueNil(nil)
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
		if atomic.CompareAndSwapUint32(&q.enID, enID, enID+1) {
			slot.store(value)
			atomic.AddUint32(&q.count, 1)
			break
		}
	}
	return true
}

// DeQueue get the frist element in queue,
// it return true if success,or false if queue empty.
func (q *Queue) DeQueue() (value interface{}, ok bool) {
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
		if atomic.CompareAndSwapUint32(&q.deID, deID, deID+1) {
			slot.free()
			atomic.AddUint32(&q.count, ^uint32(0))
			break
		}
	}
	if value == queueNil(nil) {
		value = nil
	}
	return value, true
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
