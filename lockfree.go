package queue

import (
	"runtime"
	"sync"
	"sync/atomic"
)

// LockFree 是一个无锁环形缓冲区队列的实现。
// 它为 Push 和 Pop 操作提供高性能的并发访问，无需互斥锁。
type LockFree struct {
	once sync.Once

	cap  uint64        // 队列容量
	mask uint64        // 2^n-1 > cap
	tail atomic.Uint64 // 指向下次写入数据的位置 (tail & mod)
	head atomic.Uint64 // 指向下次读取数据的位置 (head & mod)

	// 环形缓冲区存储
	//
	// tail == head+cap 表示队列已满，head == tail 表示队列为空。
	//
	// 槽位状态语义：
	//   - val == nil: 槽位为空（可 Push，对 Pop 表示队列为空）
	//   - val != nil: 槽位已占用（可 Pop，对 Push 表示队列已满）
	// 状态转换规则：
	//   - 只有 Push 能将槽位从 nil 变为非 nil
	//   - 只有 Pop 能将槽位从非 nil 变为 nil
	data []eface
}

// // ============= use for bond test =============
// func (q *LockFree) Head() *atomic.Uint64 {
// 	return &q.head
// }

// func (q *LockFree) Tail() *atomic.Uint64 {
// 	return &q.tail
// }

// // ==================== end ====================

// New 创建并返回一个新的空的无锁队列。
// 如果未显式初始化，队列将在首次 Push 操作时自动使用默认容量（256）进行初始化。
func New() Queue {
	return &LockFree{}
}

func (q *LockFree) Init(cap int) {
	q.once.Do(func() {
		if cap < 1 {
			cap = initSize
		}
		if uint64(cap) > maxUint64 {
			panic("queue: capacity overflow")
		}
		q.mask = upPow2(uint64(cap))
		q.data = make([]eface, q.mask+1)
		q.cap = uint64(cap)
		q.head.Store(0)
		q.tail.Store(0)
	})
}

func (q *LockFree) Cap() int {
	if q.data == nil {
		return 0
	}
	return int(q.cap)
}

func (q *LockFree) Len() int {
	tail := q.tail.Load()
	head := q.head.Load()
	return int(tail - head)
}

func (q *LockFree) IsEmpty() bool {
	return q.Len() == 0
}

func (q *LockFree) IsFull() bool {
	return q.Len() == int(q.cap)
}

func (q *LockFree) Push(val any) bool {
	if q.data == nil {
		q.Init(initSize)
	}
	spin := 0
	for {
		head := q.head.Load()
		tail := q.tail.Load()
		if tail-head >= q.cap {
			return false
		}
		slot := &q.data[tail&q.mask]
		if !slot.isNil() {
			runtime.Gosched() // 让出时间片，避免忙等待
			continue
		}
		if q.tail.CompareAndSwap(tail, tail+1) {
			slot.store(val)
			return true
		}
		spin++
		if spin&spinMask == 0 {
			runtime.Gosched() // 让出时间片，避免忙等待
			spin = 0
		}
	}
}

func (q *LockFree) Pop() (any, bool) {
	if q.data == nil {
		return nil, false
	}
	spin := 0
	for {
		tail := q.tail.Load()
		head := q.head.Load()
		if tail == head {
			// 队列为空。
			return nil, false
		}
		slot := &q.data[head&q.mask]
		if slot.isNil() {
			runtime.Gosched() // 让出时间片，避免忙等待
			continue
		}
		if q.head.CompareAndSwap(head, head+1) {
			val, ok := slot.load()
			if !ok {
				return nil, false
			}
			slot.free()
			return val, ok
		}
		spin++
		if spin&spinMask == 0 {
			runtime.Gosched() // 让出时间片，避免忙等待
			spin = 0
		}
	}
}

func (q *LockFree) PushBetch(vals ...any) int {
	if len(vals) <= 0 {
		return 0
	}
	actual := 0
	for _, val := range vals {
		if !q.Push(val) {
			break
		}
		actual++
	}
	return actual
}

func (q *LockFree) PopBetch(count int) ([]any, int) {
	if count <= 0 {
		return nil, 0
	}
	result := make([]any, count)
	actual := 0
	for i := 0; i < count; i++ {
		val, ok := q.Pop()
		if !ok {
			break
		}
		result[actual] = val
		actual++
	}
	result = result[:actual]
	return result, actual
}
