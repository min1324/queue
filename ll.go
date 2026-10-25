package queue

// import (
// 	"sync"
// 	"sync/atomic"
// 	"unsafe"
// )

// // 包装nil值。
// var empty = unsafe.Pointer(new(any))

// // LLQueue is a lock-free unbounded linked list queue.
// type LLQueue struct {
// 	// 声明队列后，如果没有调用Init(),队列不能使用
// 	// 为解决这个问题，加入一次性操作。
// 	// 能在队列声明后，调用Push或者Pop操作前，初始化队列。
// 	// 见 onceInit()
// 	once sync.Once

// 	// len is num of value store in queue
// 	len uint32

// 	// cap is max num of value store in queue
// 	cap uint32

// 	// head指向第一个取数据的node。tail可能指向队尾元素。
// 	//
// 	// 出队操作，先检测head==tail判断队列是否空。
// 	// slot指向head先标记需要出队的数据。
// 	// 如果slot的val是空，表明队列空。
// 	// 然后通过cas将head指针指向slot.next以移除slot。
// 	// 保存slot的val,释放slot,并返回val。
// 	//
// 	// 入队操作，由于是链表队列，大小无限制，
// 	// 队列无满条件或者直到用完内存。不用判断是否满。
// 	// 如果tail不是指向最后一个node，提升tail,直到tail指向最后一个node。
// 	// slot指向最后一个node,即slot=tail。
// 	// 通过cas加入一个nilNode在tail后面。
// 	// 尝试提升tail指向nilNode，让其他Push更快执行。
// 	// 将val存在slot里面,Pop可以取出了。
// 	//
// 	// head只能在Pop里面修改，tail只能在Push修改。
// 	head unsafe.Pointer
// 	tail unsafe.Pointer
// }

// func (q *LLQueue) Init(cap int) {
// 	q.once.Do(func() {
// 		if cap < 1 {
// 			cap = initSize
// 		}
// 		q.len = 0
// 		q.cap = uint32(cap)
// 		node := &ptrNode{} // nilNode
// 		atomic.StorePointer(&q.head, unsafe.Pointer(node))
// 		atomic.StorePointer(&q.tail, unsafe.Pointer(node))
// 	})
// }

// func (q *LLQueue) Push(val any) bool {
// 	if q.head == nil || q.tail == nil {
// 		q.Init(0)
// 	}
// 	if q.Full() {
// 		return false
// 	}
// 	// 获取储存的slot
// 	for {
// 		tail := atomic.LoadPointer(&q.tail)
// 		slot := (*ptrNode)(tail)
// 		next := slot.next
// 		if tail != atomic.LoadPointer(&q.tail) {
// 			continue
// 		}
// 		// 提升tail直到指向最后一个node
// 		if next != nil {
// 			cas(&q.tail, tail, next)
// 			continue
// 		}
// 		if !slot.isNil() {
// 			continue
// 		}

// 		// next==nil,确定slot是最后一个node
// 		if cas(&slot.next, nil, unsafe.Pointer(&ptrNode{})) {
// 			slot.store(val)
// 			cas(&q.tail, tail, slot.next)
// 			atomic.AddUint32(&q.len, 1)
// 			return true
// 		}
// 	}
// }

// func (q *LLQueue) Pop() (val any, ok bool) {
// 	if q.head == nil || q.tail == nil {
// 		return
// 	}
// 	for {
// 		head := atomic.LoadPointer(&q.head)
// 		tail := atomic.LoadPointer(&q.tail)
// 		slot := (*ptrNode)(head)
// 		if head != atomic.LoadPointer(&q.head) {
// 			continue
// 		}
// 		if head == tail {
// 			// 即便tail落后了，也不提升。
// 			// tail只能在Push里改变
// 			// return nil, false

// 			// // 尝试提升tail,
// 			if slot.next == nil {
// 				return nil, false
// 			}
// 			cas(&q.tail, tail, slot.next)
// 			continue
// 		}
// 		// 先记录slot，然后尝试取出
// 		if slot.isNil() {
// 			// Push还没添加完成，直接退出.如需等待，用continue
// 			return nil, false
// 		}
// 		if cas(&q.head, head, slot.next) {
// 			val, ok = slot.load()
// 			slot.free()
// 			if ok {
// 				atomic.AddUint32(&q.len, ^uint32(0))
// 			}
// 			// 释放slot
// 			// BUG memory reclamation
// 			//
// 			// 队列假设有node1->node2->node3
// 			// 进程1：队头head=node1.准备cas(&q.head, head, slot.next)
// 			// 进程2：刚好pop完node1,2,3，并且释放，队列空了。
// 			// 然后进程3：刚好new(node1，node3,node2),push(node1,node3,node2)进队，
// 			// 此时队列有node1->node3,node2
// 			// 回到进程1进行cas，head还是指向node1,成功，但是next并不是预期的node2。
// 			//
// 			return val, ok
// 		}
// 	}
// }

// func (q *LLQueue) BatchPush(vals ...any) (n int) {
// 	for _, val := range vals {
// 		if q.Push(val) {
// 			n++
// 		}
// 	}
// 	return n
// }

// func (q *LLQueue) BatchPop(n int) (vals []any, num int) {
// 	for i := 0; i < n; i++ {
// 		val, ok := q.Pop()
// 		if !ok {
// 			break
// 		}
// 		vals = append(vals, val)
// 		num++
// 	}
// 	return vals, num
// }
// func (q *LLQueue) Empty() bool {
// 	return q.head == q.tail
// }

// func (q *LLQueue) Full() bool {
// 	return q.len == q.cap
// }

// func (q *LLQueue) Len() int {
// 	return int(atomic.LoadUint32(&q.len))
// }

// func (q *LLQueue) Cap() int {
// 	return int(q.cap)
// }

// // node next->unsafe.Pointer
// type ptrNode struct {
// 	p    unsafe.Pointer
// 	next unsafe.Pointer
// }

// func (n *ptrNode) load() (any, bool) {
// 	tmp := atomic.LoadPointer(&n.p)
// 	if tmp == nil || tmp == empty {
// 		return nil, tmp == empty
// 	}
// 	return *(*any)(tmp), true
// }

// func (n *ptrNode) store(i any) {
// 	h := i
// 	if h == nil {
// 		atomic.StorePointer(&n.p, empty)
// 	} else {
// 		atomic.StorePointer(&n.p, unsafe.Pointer(&h))
// 	}
// }

// func (n *ptrNode) free() {
// 	atomic.StorePointer(&n.p, nil)
// 	atomic.StorePointer(&n.next, nil)
// }
// func (n *ptrNode) isNil() bool {
// 	return atomic.LoadPointer(&n.p) == nil
// }

// // // node next->unsafe.Pointer
// // type ptrNode struct {
// // 	p    any
// // 	next unsafe.Pointer
// // }

// // func newPrtNode(i any) *ptrNode {
// // 	return &ptrNode{p: i}
// // }

// // func (n *ptrNode) load() any {
// // 	return n.p
// // }

// // func (n *ptrNode) store(i any) {
// // 	n.p = i
// // }

// // func (n *ptrNode) free() {
// // 	// n.p = nil
// // 	n.next = nil
// // }

// func cas(p *unsafe.Pointer, old, new unsafe.Pointer) bool {
// 	return atomic.CompareAndSwapPointer(p, old, new)
// }
