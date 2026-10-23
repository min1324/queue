package queue

import (
	"sync"
	"sync/atomic"
)

type DRQueue struct {
	once sync.Once
	deMu sync.Mutex
	enMu sync.Mutex

	mask uint64 // 2^n-1
	cap  uint64 // queue's cap
	tail uint64 // 指向下次取出数据的位置:tail&mod
	head uint64 // 指向下次写入数据的位置:head&mod

	// 环形队列，大小必须是2的倍数。
	// val为空，表示可以Push,如果是Pop操作，表示队列空。
	// val不为空，表所可以Pop,如果是Push操作，表示队列满了。
	// 只能由Push将val从nil变成非nil,
	// 只能由Pop将val从非nil变成nil.
	data []eface
}

func (q *DRQueue) Init(cap int) {
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
		q.head = 0
		q.tail = 0
	})
}

// Cap return queue's cap
func (q *DRQueue) Cap() int {
	return int(q.cap)
}

// Size return current number in queue
func (q *DRQueue) Len() int {
	tail := atomic.LoadUint64(&q.tail)
	head := atomic.LoadUint64(&q.head)
	return int(tail - head)
}

func (q *DRQueue) IsEmpty() bool {
	return atomic.LoadUint64(&q.tail) == atomic.LoadUint64(&q.head)
}

func (q *DRQueue) IsFull() bool {
	tail := atomic.LoadUint64(&q.tail)
	head := atomic.LoadUint64(&q.head)
	return tail-head == q.cap
}

func (q *DRQueue) Push(val any) bool {
	if q.data == nil {
		q.Init(initSize)
	}
	if q.IsFull() {
		return false
	}
	q.enMu.Lock()
	defer q.enMu.Unlock()
	head := atomic.LoadUint64(&q.head)
	tail := atomic.LoadUint64(&q.tail)
	if tail-head == q.cap {
		return false
	}
	slot := &q.data[tail&q.mask]
	slot.store(val)
	atomic.AddUint64(&q.tail, 1)
	return true
}

func (q *DRQueue) Pop() (val any, ok bool) {
	if q.IsEmpty() || q.data == nil {
		return nil, false
	}
	q.deMu.Lock()
	defer q.deMu.Unlock()
	head := atomic.LoadUint64(&q.head)
	tail := atomic.LoadUint64(&q.tail)
	if tail == head {
		return nil, false
	}
	slot := &q.data[head&q.mask]
	val, ok = slot.load()
	if !ok {
		return nil, false
	}
	slot.free()
	atomic.AddUint64(&q.head, 1)
	return val, true
}

// func (q *DRQueue) Push(val any) bool {
// 	i := q.PushBetch(val)
// 	if i > 0 {
// 		return true
// 	}
// 	return false
// }

// func (q *DRQueue) Pop() (val any, ok bool) {
// 	r, i := q.PopBetch(1)
// 	if i == 0 {
// 		return
// 	}
// 	return r[0], true
// }

func (q *DRQueue) PushBetch(val ...any) int {
	i, _ := q.pushBetch(val...)
	return i
}

// 返回已经push的个数，返回队列是否满
func (q *DRQueue) pushBetch(val ...any) (int, bool) {
	if len(val) == 0 {
		return 0, true
	}
	if q.data == nil {
		q.Init(initSize)
	}
	q.enMu.Lock()
	defer q.enMu.Unlock()

	head := atomic.LoadUint64(&q.head)
	tail := atomic.LoadUint64(&q.tail)
	mask := q.mask
	availbale := int(q.cap - (tail - head))
	if availbale < 0 {
		return 0, false
	}
	if availbale > len(val) {
		availbale = len(val)
	}

	for i := 0; i < availbale; i++ {
		slot := &q.data[(tail+uint64(i))&mask]
		slot.store(val[i])
	}
	atomic.AddUint64(&q.tail, uint64(availbale))
	return availbale, true
}

func (q *DRQueue) PopBetch(count int) ([]any, int) {
	if count < 1 {
		return nil, 0
	}
	if q.IsEmpty() {
		return nil, 0
	}
	q.deMu.Lock()
	defer q.deMu.Unlock()
	head := atomic.LoadUint64(&q.head)
	tail := atomic.LoadUint64(&q.tail)
	mask := q.mask
	availbale := int(tail - head)
	if availbale < 1 {
		return nil, 0
	}
	if availbale > count {
		availbale = count
	}
	actual := 0
	result := make([]any, availbale)

	for i := 0; i < availbale; i++ {
		slot := &q.data[(head+uint64(i))&mask]
		v, ok := slot.load()
		if !ok {
			// slot is nil, maybe other goroutine is pop it.
			// or pusher is push it.
			break
		}
		slot.free()
		result[i] = v
		actual++
	}
	atomic.AddUint64(&q.head, uint64(actual))
	result = result[:actual]
	return result, actual
}
