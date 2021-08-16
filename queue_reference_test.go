package queue_test

import (
	"sync"
	"unsafe"
)

// use for slice
const (
	bit  = 3
	mod  = 1<<bit - 1
	null = ^uintptr(0) // -1
)

// 包装nil值。
var empty = unsafe.Pointer(new(interface{}))

// QInterface use in stack,queue testing
type QInterface interface {
	Init()
	Size() int
	EnQueue(interface{}) bool
	DeQueue() (interface{}, bool)
}

// SQInterface use in stack,queue testing
type SInterface interface {
	Init()
	Size() int
	Push(interface{}) bool
	Pop() (interface{}, bool)
}

// node stack,queue node
type node struct {
	// p    unsafe.Pointer
	p    interface{}
	next *node
}

func newNode(i interface{}) *node {
	return &node{p: i}
	// return &node{p: unsafe.Pointer(&i)}
}

func (n *node) load() interface{} {
	return n.p
	//return *(*interface{})(n.p)
}

// UnsafeQueue queue without mutex
type UnsafeQueue struct {
	head, tail *node
	len        int
	once       sync.Once
}

func (q *UnsafeQueue) onceInit() {
	q.once.Do(func() {
		q.init()
	})
}

func (q *UnsafeQueue) init() {
	q.head = &node{}
	q.tail = q.head
	q.len = 0
}

func (q *UnsafeQueue) Init() {
	q.onceInit()

	head := q.head // start element
	tail := q.tail // end element
	if head == tail {
		return
	}
	q.head = q.tail
	q.len = 0
	// free queue [s ->...-> e]
	for head != tail && head != nil {
		el := head
		head = el.next
		el.next = nil
	}
}

func (q *UnsafeQueue) Full() bool {
	return false
}

func (q *UnsafeQueue) Empty() bool {
	return q.len == 0
}

func (q *UnsafeQueue) Size() int {
	return q.len
}

func (q *UnsafeQueue) EnQueue(i interface{}) bool {
	q.onceInit()
	slot := newNode(i)
	q.tail.next = slot
	q.tail = slot
	q.len += 1
	return true
}

func (q *UnsafeQueue) DeQueue() (val interface{}, ok bool) {
	q.onceInit()
	if q.head.next == nil {
		return nil, false
	}
	slot := q.head
	q.head = q.head.next
	slot.next = nil
	q.len -= 1
	val = q.head.load()
	return val, true
}

// interface node
type baseNode struct {
	p interface{}
}

func newBaseNode(i interface{}) *baseNode {
	return &baseNode{p: i}
}

func (n *baseNode) load() interface{} {
	return n.p
}

func (n *baseNode) store(i interface{}) {
	n.p = i
}

func (n *baseNode) free() {
	n.p = nil
}

// 链表节点
type listNode struct {
	baseNode
	next *listNode
}

func newListNode(i interface{}) *listNode {
	ln := listNode{}
	ln.store(i)
	return &ln
}

func (n *listNode) free() {
	n.baseNode.free()
	n.next = nil
}

// SLQueue unbounded list queue with one mutex
type SLQueue struct {
	once sync.Once
	mu   sync.Mutex

	len  int
	head *listNode
	tail *listNode
}

func (q *SLQueue) onceInit() {
	q.once.Do(func() {
		q.init()
	})
}

func (q *SLQueue) init() {
	q.head = newListNode(nil)
	q.tail = q.head
	q.len = 0
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
	q.len = 0
	for head != tail && head != nil {
		freeNode := head
		head = freeNode.next
		freeNode.free()
	}
}

func (q *SLQueue) Empty() bool {
	return q.head == q.tail
}

func (q *SLQueue) Size() int {
	return q.len
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

	q.len++
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
	q.len--
	slot.free()
	return val, true
}
