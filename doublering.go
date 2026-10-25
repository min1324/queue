package queue

import (
	"sync"
	"sync/atomic"
)

// 双锁环形队列，可以并发Push和Pop
type DoubleRing struct {
	once sync.Once

	inMux  sync.Mutex // 控制Push操作
	outMux sync.Mutex // 控制Pop操作

	cap  uint64 // 队列容量
	mask uint64 // 2^n-1 > cap

	tail atomic.Uint64 // 指向下次取出数据的位置:tail&mask
	head atomic.Uint64 // 指向下次写入数据的位置:head&mask

	// 环形缓冲区存储区。
	//
	// 容量为2^n，其中 n 是大于 cap 的最小正整数。
	//
	// tail == head+cap 表示队列已满，head == tail 表示队列为空。
	data []entry
}

func (q *DoubleRing) Init(cap int) {
	q.once.Do(func() {
		if cap < 1 {
			cap = initSize
		}
		if uint64(cap) > maxUint64 {
			panic("queue: capacity overflow")
		}
		q.mask = upPow2(uint64(cap))
		q.data = make([]entry, q.mask+1)
		q.cap = uint64(cap)
		q.head.Store(0)
		q.tail.Store(0)
	})
}

func (q *DoubleRing) Cap() int {
	if q.data == nil {
		return 0
	}
	return int(q.cap)
}

// Size return current number in queue
func (q *DoubleRing) Len() int {
	tail := q.tail.Load()
	head := q.head.Load()
	return int(tail - head)
}

func (q *DoubleRing) IsEmpty() bool {
	return q.tail.Load() == q.head.Load()
}

func (q *DoubleRing) IsFull() bool {
	return q.tail.Load() == q.head.Load()+q.cap
}

func (q *DoubleRing) Push(val any) bool {
	if q.data == nil {
		q.Init(initSize)
	}
	if q.IsFull() {
		return false
	}
	q.inMux.Lock()
	defer q.inMux.Unlock()
	tail := q.tail.Load()
	head := q.head.Load()
	if tail-head == q.cap {
		return false
	}
	slot := &q.data[tail&q.mask]
	slot.store(val)
	q.tail.Add(1)
	return true
}

func (q *DoubleRing) Pop() (val any, ok bool) {
	if q.IsEmpty() || q.data == nil {
		return nil, false
	}
	q.outMux.Lock()
	defer q.outMux.Unlock()
	tail := q.tail.Load()
	head := q.head.Load()
	if tail == head {
		return nil, false
	}
	slot := &q.data[head&q.mask]
	val, ok = slot.load()
	if !ok {
		return nil, false
	}
	slot.free()
	q.head.Add(1)
	return val, true
}

func (q *DoubleRing) BatchPush(val ...any) int {
	i, _ := q.batchPush(val...)
	return i
}

// 返回已经push的个数，返回队列是否满
func (q *DoubleRing) batchPush(val ...any) (int, bool) {
	if len(val) == 0 {
		return 0, true
	}
	if q.data == nil {
		q.Init(initSize)
	}
	q.inMux.Lock()
	defer q.inMux.Unlock()

	tail := q.tail.Load()
	head := q.head.Load()
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
	q.tail.Add(uint64(availbale))
	return availbale, true
}

func (q *DoubleRing) BatchPop(count int) ([]any, int) {
	if count < 1 {
		return nil, 0
	}
	if q.IsEmpty() {
		return nil, 0
	}
	q.outMux.Lock()
	defer q.outMux.Unlock()
	tail := q.tail.Load()
	head := q.head.Load()
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
	q.head.Add(uint64(actual))
	result = result[:actual]
	return result, actual
}

func (q *DoubleRing) Clean() ([]any, int) {
	if q.IsEmpty() {
		return nil, 0
	}
	q.inMux.Lock()
	defer q.inMux.Unlock()
	q.outMux.Lock()
	defer q.outMux.Unlock()
	tail := q.tail.Load()
	head := q.head.Load()
	mask := q.mask

	// tail == head+cap 表示队列已满，head == tail 表示队列为空。
	q.head.Store(tail)

	var result = make([]any, q.cap)
	actual := 0
	for i := head; i < tail; i++ {
		slot := &q.data[i&mask]
		v, ok := slot.load()
		if !ok {
			continue
		}
		result[i-head] = v
		actual++
		slot.free()
	}
	if actual > 0 {
		result = result[:actual]
	}
	return result, actual
}
