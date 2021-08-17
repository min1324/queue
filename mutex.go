package queue

import (
	"sync"
	"sync/atomic"
)

const (
	initSize   = 1 << 3
	queueBits  = 32
	queueLimit = (1 << queueBits) >> 2
)

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

	len uint32
	cap uint32
	mod uint32

	enID uint32
	deID uint32

	// val为空，表示可以EnQUeue,如果是DeQueue操作，表示队列空。
	// val不为空，表所可以DeQueue,如果是EnQUeue操作，表示队列满了。
	// 并且只能由EnQUeue将val从nil变成非nil,
	// 只能由DeQueue将val从非niu变成nil.
	data []entry
}

func (q *DRQueue) onceInit(cap int) {
	q.once.Do(func() {
		if q.cap < 1 {
			cap = 1 << 8
		}
		mod := modUint32(uint32(cap))
		q.mod = mod
		q.cap = mod + 1
		q.data = make([]entry, mod+1)
	})
}

func (q *DRQueue) OnceInit(cap int) {
	q.onceInit(cap)
}

func (q *DRQueue) Init() {
	q.onceInit(1 << 8)
}

func (q *DRQueue) Cap() int {
	return int(atomic.LoadUint32(&q.cap))
}

func (q *DRQueue) Full() bool {
	return atomic.LoadUint32(&q.len) == atomic.LoadUint32(&q.cap)
}

func (q *DRQueue) Empty() bool {
	return atomic.LoadUint32(&q.len) == 0
}

func (q *DRQueue) Size() int {
	return int(atomic.LoadUint32(&q.len))
}

// 根据enID,deID获取进队，出队对应的slot
func (q *DRQueue) getSlot(id uint32) *entry {
	return &q.data[int(id&atomic.LoadUint32(&q.mod))]
}

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
	atomic.AddUint32(&q.len, 1)
	return true
}

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
	atomic.AddUint32(&q.len, ^uint32(0))
	return val, true
}

// 链表节点
type listNode struct {
	entry
	next *listNode
}

func newListNode(i interface{}) *listNode {
	ln := listNode{}
	ln.store(i)
	return &ln
}

func (n *listNode) free() {
	n.entry.free()
	n.next = nil
}

// SLQueue unbounded list queue with one mutex
type SLQueue struct {
	once sync.Once
	mu   sync.Mutex

	len  uint32
	head *listNode
	tail *listNode
}

func (q *SLQueue) onceInit() {
	q.once.Do(func() {
		q.head = newListNode(nil)
		q.tail = q.head
		q.len = 0
	})
}

func (q *SLQueue) Init() {
	q.onceInit()
	q.mu.Lock()
	defer q.mu.Unlock()

	head := q.head
	tail := q.tail
	if head == tail {
		return
	}
	q.head = q.tail
	atomic.StoreUint32(&q.len, 0)
	for head != tail && head != nil {
		freeNode := head
		head = freeNode.next
		freeNode.free()
	}
}

func (q *SLQueue) Cap() int {
	return queueLimit
}

func (q *SLQueue) Full() bool {
	return false
}

func (q *SLQueue) Empty() bool {
	return atomic.LoadUint32(&q.len) == 0
}

func (q *SLQueue) Size() int {
	return int(atomic.LoadUint32(&q.len))
}

func (q *SLQueue) EnQueue(val interface{}) bool {
	q.onceInit()
	q.mu.Lock()
	defer q.mu.Unlock()

	if val == nil {
		val = empty
	}
	// 方案1：tail指向最后一个有效node
	// slot := newListNode(i)
	// q.tail.next = slot
	// q.tail = slot

	// 方案2：tail指向下一个存入的空node
	slot := q.tail
	nilNode := newListNode(nil)
	slot.next = nilNode
	q.tail = nilNode
	slot.store(val)

	atomic.AddUint32(&q.len, 1)
	return true
}

func (q *SLQueue) DeQueue() (val interface{}, ok bool) {
	q.onceInit()
	if q.Empty() {
		return
	}
	q.mu.Lock()
	defer q.mu.Unlock()

	if q.Empty() {
		return
	}
	// 方案1：head指向一个无效哨兵node
	// slot := q.head
	// q.head = q.head.next
	// val = q.head.load()

	// 方案2：head指向下一个取出的有效node
	slot := q.head
	val = slot.load()
	if val == nil {
		return
	}
	q.head = slot.next
	if val == empty {
		val = nil
	}
	atomic.AddUint32(&q.len, ^uint32(0))
	slot.free()
	return val, true
}

// 双锁链表队列
//
// DLQueue is a concurrent unbounded queue which uses two-Lock concurrent queue qlgorithm.

// DLQueue unbounded list queue with one mutex
type DLQueue struct {
	once sync.Once
	deMu sync.Mutex // DeQueue操作锁
	enMu sync.Mutex // EnQUeue操作锁

	len  uint32
	head *listNode // 只能由DeQueue操作更改，其他操作只读
	tail *listNode // 只能由EnQUeue操作更改，其他操作只读
}

func (q *DLQueue) onceInit() {
	q.once.Do(func() {
		q.init()
	})
}

func (q *DLQueue) init() {
	q.head = newListNode(nil)
	q.tail = q.head
	q.len = 0
}

func (q *DLQueue) Init() {
	q.onceInit()
	q.enMu.Lock()
	defer q.enMu.Unlock()
	q.deMu.Lock()
	defer q.deMu.Unlock()

	head := q.head
	tail := q.tail
	if head == tail {
		return
	}
	q.head = q.tail
	atomic.StoreUint32(&q.len, 0)
	for head != tail && head != nil {
		freeNode := head
		head = freeNode.next
		freeNode.free()
	}
}

func (q *DLQueue) Cap() int {
	return queueLimit
}

func (q *DLQueue) Full() bool {
	return false
}

func (q *DLQueue) Empty() bool {
	return atomic.LoadUint32(&q.len) == 0
}

func (q *DLQueue) Size() int {
	return int(atomic.LoadUint32(&q.len))
}

func (q *DLQueue) EnQueue(val interface{}) bool {
	q.onceInit()
	q.enMu.Lock()
	defer q.enMu.Unlock()
	if val == nil {
		val = empty
	}
	// tail指向下一个存入的位置
	slot := q.tail
	nilNode := newListNode(nil)
	slot.next = nilNode
	q.tail = nilNode
	slot.store(val)
	atomic.AddUint32(&q.len, 1)
	return true
}

func (q *DLQueue) DeQueue() (val interface{}, ok bool) {
	q.onceInit()
	if q.Empty() {
		return
	}
	q.deMu.Lock()
	defer q.deMu.Unlock()

	if q.Empty() {
		return
	}
	slot := q.head
	val = slot.load()
	if val == nil {
		return
	}
	q.head = slot.next
	if val == empty {
		val = nil
	}
	atomic.AddUint32(&q.len, ^uint32(0))
	slot.free()
	return val, true
}
