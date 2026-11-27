package queue

import (
	"errors"
	"sync"
	"sync/atomic"
)

var errMuxRingFull error = errors.New("MuxRing[T any]: queue full")

type entrymux[T any] struct {
	// 是否被使用
	// 读操作必须先判断是否被使用，然后再读取
	// 写操作必须先写数据，然后才设置被使用
	use  atomic.Bool
	data T
}

func (l *entrymux[T]) isUsed() bool {
	return l.use.Load()
}

func (l *entrymux[T]) load() T {
	val := l.data
	return val
}

func (l *entrymux[T]) store(val T) {
	l.data = val
	l.use.Store(true)
}

func (l *entrymux[T]) delete() {
	var zero T
	l.data = zero
	l.use.Store(false)
}

func (l *entrymux[T]) LoadAndDelete() (val T, loaded bool) {
	if !l.use.Load() {
		return val, false
	}
	var zero T
	val = l.data
	l.data = zero
	l.use.Store(false)
	return val, true
}

// 双锁环形队列，可以并发Push和Pop
type MuxRing[T any] struct {
	// 如果同时获取push和pop锁，必须先push后pop
	pushMux sync.Mutex   // 控制Push操作
	popMux  sync.RWMutex // 控制Pop操作

	cap  uint64 // 队列容量,为0时表示未初始化
	mask uint64 // 2^n-1 > cap

	tail atomic.Uint64 // 指向下次取出数据的位置:tail&mask
	head atomic.Uint64 // 指向下次写入数据的位置:head&mask

	// 环形缓冲区存储区。
	//
	// 容量为2^n，其中 n 是大于 cap 的最小正整数。
	//
	// tail == head+cap 表示队列已满，head == tail 表示队列为空。
	data []entrymux[T]
}

func (q *MuxRing[T]) isInit() bool {
	return q.cap > 0
}

// 必须在锁里面调用
func (q *MuxRing[T]) initInLock(cap int) {
	if cap < 1 {
		cap = DefaultCap
	}
	if cap > MaxCap {
		cap = MaxCap
	}
	q.mask = kfifo(uint64(cap))
	q.head.Store(0)
	q.tail.Store(0)
	q.data = make([]entrymux[T], q.mask+1)

	// cap必须最后写入
	q.cap = uint64(cap)
}

func (q *MuxRing[T]) Init(cap int) {
	if q.isInit() {
		return
	}
	q.pushMux.Lock()
	defer q.pushMux.Unlock()
	if q.data == nil {
		q.initInLock(cap)
	}
}

func (q *MuxRing[T]) Cap() int {
	return int(q.cap)
}

// Size return current number in queue
func (q *MuxRing[T]) Size() int {
	tail := q.tail.Load()
	head := q.head.Load()
	return int(tail - head)
}

func (q *MuxRing[T]) Empty() bool {
	return q.tail.Load() == q.head.Load()
}

func (q *MuxRing[T]) Full() bool {
	return q.tail.Load() == q.head.Load()+q.cap
}

func (q *MuxRing[T]) Push(val T) bool {
	if q.Full() && q.cap > 0 {
		return false
	}
	q.pushMux.Lock()
	defer q.pushMux.Unlock()
	if q.data == nil {
		q.initInLock(DefaultCap)
	}
	tail := q.tail.Load()
	head := q.head.Load()
	if tail-head >= q.cap {
		return false
	}
	// 先增tail，再写入数据,提高pop的并发性
	q.tail.Add(1)
	slot := &q.data[tail&q.mask]
	slot.store(val)
	return true
}

func (q *MuxRing[T]) Pop() (val T, ok bool) {
	if q.Empty() {
		return val, false
	}
	q.popMux.Lock()
	defer q.popMux.Unlock()
	tail := q.tail.Load()
	head := q.head.Load()
	if tail == head {
		return val, false
	}
	slot := &q.data[head&q.mask]
	if !slot.isUsed() {
		// 还没写入完成
		return val, false
	}
	val, _ = slot.LoadAndDelete()
	q.head.Add(1)
	return val, true
}

func (q *MuxRing[T]) BatchPush(val ...T) (int, error) {
	if len(val) < 1 {
		return 0, nil
	}
	if q.Full() && q.cap > 0 {
		return 0, errMuxRingFull
	}
	q.pushMux.Lock()
	defer q.pushMux.Unlock()
	if q.data == nil {
		q.initInLock(DefaultCap)
	}

	tail := q.tail.Load()
	head := q.head.Load()

	availbale := int(q.cap - (tail - head))
	if availbale < 0 {
		return 0, errMuxRingFull
	}
	if availbale > len(val) {
		availbale = len(val)
	}
	q.tail.Add(uint64(availbale))
	mask := q.mask
	for i := 0; i < availbale; i++ {
		slot := &q.data[(tail+uint64(i))&mask]
		slot.store(val[i])
	}
	return availbale, nil
}

func (q *MuxRing[T]) BatchPop(count int) []T {
	if count < 1 || q.Empty() {
		return nil
	}
	q.popMux.Lock()
	defer q.popMux.Unlock()
	tail := q.tail.Load()
	head := q.head.Load()
	availbale := int(tail - head)
	if availbale < 1 {
		return nil
	}
	if availbale > count {
		availbale = count
	}
	actual := 0
	result := make([]T, availbale)
	mask := q.mask
	for i := 0; i < availbale; i++ {
		slot := &q.data[(head+uint64(i))&mask]
		if !slot.isUsed() {
			break // 遇到未就绪数据，终止
		}
		v, _ := slot.LoadAndDelete()
		result[i] = v
		actual++
	}
	q.head.Add(uint64(actual))
	result = result[:actual]
	return result
}

func (q *MuxRing[T]) Head() (val T, ok bool) {
	if q.Empty() {
		return val, false
	}
	q.popMux.RLock()
	defer q.popMux.RUnlock()
	tail := q.tail.Load()
	head := q.head.Load()
	if tail == head {
		return val, false
	}
	slot := &q.data[head&q.mask]
	if !slot.isUsed() {
		return val, false
	}
	val = slot.load()
	return val, true

}

func (q *MuxRing[T]) Tail() (val T, ok bool) {
	if q.Empty() {
		return val, false
	}
	q.popMux.RLock()
	defer q.popMux.RUnlock()
	tail := q.tail.Load()
	head := q.head.Load()
	if tail == head {
		return val, false
	}
	slot := &q.data[(tail-1)&q.mask]
	if !slot.isUsed() {
		return val, false
	}
	val = slot.load()
	return val, true
}

func (q *MuxRing[T]) All() []T {
	if q.Empty() {
		return nil
	}
	q.popMux.Lock()
	defer q.popMux.Unlock()
	tail := q.tail.Load()
	head := q.head.Load()
	mask := q.mask
	if tail == head {
		return nil
	}

	var result = make([]T, 0, int(tail-head))
	for i := head; i < tail; i++ {
		slot := &q.data[i&mask]
		if !slot.isUsed() {
			break // 遇到未就绪数据，终止
		}
		v := slot.load()
		result = append(result, v)
	}
	return result
}

func (q *MuxRing[T]) Clean() {
	if q.Empty() {
		return
	}
	q.pushMux.Lock()
	defer q.pushMux.Unlock()
	q.popMux.Lock()
	defer q.popMux.Unlock()
	head := q.head.Load()
	tail := q.tail.Load()
	// tail == head+cap 表示队列已满，head == tail 表示队列为空。
	q.head.Store(tail)
	for i := head; i < tail; i++ {
		slot := &q.data[i&q.mask]
		if slot.isUsed() {
			slot.delete()
		}
	}
}

// func (q *MuxRing[T]) Peek(index int) (val T, ok bool) {
// 	if q.Empty() {
// 		return val, false
// 	}
// 	q.popMux.RLock()
// 	defer q.popMux.RUnlock()
// 	tail := q.tail.Load()
// 	head := q.head.Load()
// 	if tail == head {
// 		return val, false
// 	}
// 	if index < 0 || index >= int(tail-head) {
// 		return val, false
// 	}
// 	slot := &q.data[(head+uint64(index))&q.mask]
// 	if !slot.isUsed() {
// 		return val, false
// 	}
// 	val = slot.load()
// 	return val, true
// }
