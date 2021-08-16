package queue

import (
	"sync"
	"sync/atomic"
	"unsafe"
)

// 包装nil值。
var empty = unsafe.Pointer(new(interface{}))

// LLQueue is a lock-free unbounded linked list queue.
type LLQueue struct {
	// 声明队列后，如果没有调用Init(),队列不能使用
	// 为解决这个问题，加入一次性操作。
	// 能在队列声明后，调用EnQueue或者DeQueue操作前，初始化队列。
	// 见 onceInit()
	once sync.Once

	// len is num of value store in queue
	len uint32

	// head指向第一个取数据的node。tail可能指向队尾元素。
	//
	// 出队操作，先检测head==tail判断队列是否空。
	// slot指向head先标记需要出队的数据。
	// 如果slot的val是空，表明队列空。
	// 然后通过cas将head指针指向slot.next以移除slot。
	// 保存slot的val,释放slot,并返回val。
	//
	// 入队操作，由于是链表队列，大小无限制，
	// 队列无满条件或者直到用完内存。不用判断是否满。
	// 如果tail不是指向最后一个node，提升tail,直到tail指向最后一个node。
	// slot指向最后一个node,即slot=tail。
	// 通过cas加入一个nilNode在tail后面。
	// 尝试提升tail指向nilNode，让其他EnQueue更快执行。
	// 将val存在slot里面,DeQueue可以取出了。
	//
	// head只能在DeQueue里面修改，tail只能在Enqueue修改。
	head unsafe.Pointer
	tail unsafe.Pointer
}

// 一次性初始化,线程安全。
func (q *LLQueue) onceInit() {
	q.once.Do(func() {
		q.init()
	})
}

func (q *LLQueue) init() {
	q.head = unsafe.Pointer(newPrtNode(nil))
	q.tail = q.head
	q.len = 0
}

func (q *LLQueue) Init() {
	q.onceInit()
	for {
		head := q.head
		tail := q.tail
		if head == tail {
			return //  空队列不需要初始化
		}
		oldLen := atomic.LoadUint32(&q.len)
		if cas(&q.head, head, tail) {
			// cas成功，表明期间并无其他操作，队列已经空了，但是len还没置0
			// 有可能并发EnQueue,增加len；即使有并发DeQueue,也能保持newLen>=oldLen
			// 需要用新len-旧len。负数为：反码+1。
			atomic.AddUint32(&q.len, (^oldLen + 1))
			for head != tail && head != nil {
				freeNode := (*ptrNode)(head)
				head = freeNode.next
				freeNode.free()
			}
			return
		}
	}
}

func (q *LLQueue) EnQueue(val interface{}) bool {
	q.onceInit()
	if val == nil {
		val = empty
	}
	var slot *ptrNode
	nilNode := unsafe.Pointer(newPrtNode(nil))
	// 获取储存的slot
	for {
		tail := atomic.LoadPointer(&q.tail)
		slot = (*ptrNode)(tail)
		next := slot.next
		if tail != atomic.LoadPointer(&q.tail) {
			continue
		}
		// 提升tail直到指向最后一个node
		if next != nil {
			cas(&q.tail, tail, next)
			continue
		}
		if slot.load() != nil {
			continue
		}

		// next==nil,确定slot是最后一个node
		if cas(&slot.next, nil, nilNode) {
			// 获得储存的slot，尝试将tail提升到最后一个node。
			cas(&q.tail, tail, slot.next)
			atomic.AddUint32(&q.len, 1)
			break
		}
	}

	// 将val储存到slot，让DeQueue可以取走
	slot.store(val)
	return true
}

func (q *LLQueue) DeQueue() (val interface{}, ok bool) {
	q.onceInit()
	if q.Empty() {
		return
	}
	var slot *ptrNode
	// 获取slot
	for {
		head := atomic.LoadPointer(&q.head)
		tail := atomic.LoadPointer(&q.tail)
		slot = (*ptrNode)(head)
		if head != atomic.LoadPointer(&q.head) {
			continue
		}
		if head == tail {
			// 即便tail落后了，也不提升。
			// tail只能在EnQueue里改变
			// return nil, false

			// 尝试提升tail,
			if slot.next == nil {
				return nil, false
			}
			cas(&q.tail, tail, slot.next)
			continue
		}
		// 先记录slot，然后尝试取出
		val = slot.load()
		if val == nil {
			// Enqueue还没添加完成，直接退出.如需等待，用continue
			return nil, false
		}
		if cas(&q.head, head, slot.next) {
			// 成功取出slot
			break
		}
	}
	if val == empty {
		val = nil
	}
	atomic.AddUint32(&q.len, ^uint32(0))

	// 释放slot
	// BUG memory reclamation
	//
	// 队列假设有node1->node2->node3
	// 进程1：队头head=node1.准备cas(&q.head, head, slot.next)
	// 进程2：刚好pop完node1,2,3，并且释放，队列空了。
	// 然后进程3：刚好new(node1，node3,node2),push(node1,node3,node2)进队，
	// 此时队列有node1->node3,node2
	// 回到进程1进行cas，head还是指向node1,成功，但是next并不是预期的node2。
	//
	// slot.free()
	return val, true
}

func (q *LLQueue) Empty() bool {
	return q.head == q.tail
}

func (q *LLQueue) Size() int {
	return int(atomic.LoadUint32(&q.len))
}

// node next->unsafe.Pointer
type ptrNode struct {
	p    interface{}
	next unsafe.Pointer
}

func newPrtNode(i interface{}) *ptrNode {
	return &ptrNode{p: i}
}

func (n *ptrNode) load() interface{} {
	return n.p
}

func (n *ptrNode) store(i interface{}) {
	n.p = i
}

func (n *ptrNode) free() {
	// n.p = nil
	n.next = nil
}

func cas(p *unsafe.Pointer, old, new unsafe.Pointer) bool {
	return atomic.CompareAndSwapPointer(p, old, new)
}
