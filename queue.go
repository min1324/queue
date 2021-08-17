package queue

import (
	"sync"
	"sync/atomic"
)

// 包装nil值。
var empty = new(interface{})

// lock-free queue implement with array
//
// LRQueue is a lock-free ring array queue.
type LRQueue struct {
	once sync.Once

	cap  uint32 // 队列容量，自动向上调整至2^n
	mod  uint32 // cap-1,即2^n-1,用作取slot: data[ID&mod]
	deID uint32 // 指向下次取出数据的位置:deID&mod
	enID uint32 // 指向下次写入数据的位置:enID&mod

	// 环形队列，大小必须是2的倍数。
	// val为空，表示可以EnQUeue,如果是DeQueue操作，表示队列空。
	// val不为空，表所可以DeQueue,如果是EnQUeue操作，表示队列满了。
	// 并且只能由EnQUeue将val从nil变成非nil,
	// 只能由DeQueue将val从非niu变成nil.
	data []baseNode
}

// 一次性初始化
func (q *LRQueue) onceInit() {
	q.once.Do(func() {
		q.init()
	})
}

// 无并发初始化
func (q *LRQueue) init() {
	cap := atomic.LoadUint32(&q.cap)
	if cap < 1 {
		cap = 1 << 8
	}
	mod := modUint32(cap)
	q.deID = q.enID
	q.mod = mod
	q.cap = mod + 1
	// atomic.StoreUint32(&q.cap, mod+1)
	q.data = make([]baseNode, q.cap)
}

// Init初始化长度为: DefauleSize.
func (q *LRQueue) Init() {
	q.InitWith()
}

// InitWith初始化长度为cap的queue,
// 如果未提供，则使用默认值: DefauleSize.
func (q *LRQueue) InitWith(caps ...int) {
	q.onceInit()
	var oldCap = atomic.LoadUint32(&q.cap)
	var newCap = oldCap
	if len(caps) > 0 && caps[0] > 0 {
		newCap = uint32(caps[0])
	}
	newMod := modUint32(newCap)
	newCap = newMod + 1
	for {
		cap := atomic.LoadUint32(&q.cap)
		if cap == 0 {
			// 其他InitWith正在执行，等待完成
			return
		}
		if casUint32(&q.cap, cap, 0) {
			// 获得初始化权限
			oldCap = cap
			break
		}
	}
	// 让运行中的push,pop停止。
	for {
		enID := atomic.LoadUint32(&q.enID)
		deID := atomic.LoadUint32(&q.deID)
		if !casUint32(&q.deID, deID, enID) {
			continue
		}
		if casUint32(&q.enID, enID, enID+1) {
			atomic.StoreUint32(&q.deID, enID)
			break
		}
	}
	// 初始化,保证getSlot不panic
	if oldCap > newCap {
		atomic.StoreUint32(&q.mod, newMod)
		q.data = make([]baseNode, newCap)
	} else {
		q.data = make([]baseNode, newCap)
		atomic.StoreUint32(&q.mod, newMod)
	}
	atomic.StoreUint32(&q.cap, newCap)
}

// 数量
func (q *LRQueue) Size() int {
	deID := atomic.LoadUint32(&q.deID)
	enID := atomic.LoadUint32(&q.enID)
	return int(enID - deID)
}

// 根据enID,deID获取进队，出队对应的slot
func (q *LRQueue) getSlot(id uint32) *baseNode {
	return &q.data[id&atomic.LoadUint32(&q.mod)]
}

func (q *LRQueue) EnQueue(val interface{}) bool {
	q.onceInit()
	if q.Full() {
		return false
	}
	if val == nil {
		val = empty
	}
	for {
		enID := atomic.LoadUint32(&q.enID)
		if q.Full() {
			return false
		}
		slot := q.getSlot(enID)
		if slot.load() != nil {
			// TODO 是否需要写入缓冲区,或者扩容
			// queue full,
			return false
		}
		if casUint32(&q.enID, enID, enID+1) {
			// 成功获得slot
			slot.store(val)
			break
		}
	}
	return true
}

func (q *LRQueue) DeQueue() (val interface{}, ok bool) {
	q.onceInit()
	if q.Empty() {
		return
	}
	for {
		// 获取最新 DeQueuePID,
		deID := atomic.LoadUint32(&q.deID)
		if q.Empty() {
			return
		}
		slot := q.getSlot(deID)
		val = slot.load()
		if val == nil {
			// queue empty,
			return nil, false
		}
		if casUint32(&q.deID, deID, deID+1) {
			// 成功取出slot
			if val == empty {
				val = nil
			}
			slot.free()
			break
		}
	}
	return val, true
}

// queue's cap
func (q *LRQueue) Cap() int {
	return int(q.cap)
}

// 队列是否满
func (q *LRQueue) Full() bool {
	// InitWith时，将cap置为0.
	cap := atomic.LoadUint32(&q.cap)
	deID := atomic.LoadUint32(&q.deID)
	enID := atomic.LoadUint32(&q.enID)
	return enID >= cap+deID
}

// 队列是否空
func (q *LRQueue) Empty() bool {
	return atomic.LoadUint32(&q.deID) == atomic.LoadUint32(&q.enID)
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
