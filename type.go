package queue

import (
	"errors"
	"runtime"
	"sync"
	"sync/atomic"
	"unsafe"
)

var errTypeLFFull error = errors.New("TypeLF: queue full")
var errTypeLFCASFail error = errors.New("TypeLF: concurrent batch CAS failed")

// ------------------------ Type[T any] ------------------------

// TypeLF[T any] 是一个 LockFree 的泛型实现。
type TypeLF[T any] struct {
	once sync.Once

	cap  uint64 // 队列实际容量
	mask uint64 // 2^n-1 > cap, 用于取模计算

	tail atomic.Uint64 // 指向下次写入数据的位置 (tail & mod)
	_    [cacheLineSize - unsafe.Sizeof(atomic.Uint64{})]byte
	head atomic.Uint64 // 指向下次读取数据的位置 (head & mod)
	_    [cacheLineSize - unsafe.Sizeof(atomic.Uint64{})]byte

	// 环形缓冲区存储区。
	//
	// 容量为2^n，其中 n 是大于 cap 的最小正整数。
	//
	// tail == head+cap 表示队列已满，head == tail 表示队列为空。
	data []lfentry[T]
}

type lfentry[T any] struct {
	use  atomic.Bool
	data T
}

func (l *lfentry[T]) isUsed() bool {
	return l.use.Load()
}

func (l *lfentry[T]) Load() T {
	val := l.data
	return val
}

func (l *lfentry[T]) Store(val T) {
	l.data = val
	l.use.Store(true)
}

func (l *lfentry[T]) Delete() {
	var zero T
	l.data = zero
	l.use.Store(false)
}

func (l *lfentry[T]) LoadAndDelete() T {
	var zero T
	val := l.data
	l.data = zero
	l.use.Store(false)
	return val
}

func NewTypeLF[T any]() *TypeLF[T] {
	return &TypeLF[T]{}
}

// Init 初始化队列，只执行一次
func (q *TypeLF[T]) Init(cap int) {
	q.lazyInit(cap)
}

func (q *TypeLF[T]) notInit() bool {
	return atomic.LoadUint64(&q.cap) == 0
}

func (q *TypeLF[T]) lazyInit(cap int) {
	q.once.Do(func() {
		if cap < 1 {
			cap = defaultCap
		}
		if uint64(cap) > maxCap {
			panic("queue: capacity overflow")
		}
		mask := kfifo(uint64(cap))

		q.head.Store(0)
		q.tail.Store(0)

		q.data = make([]lfentry[T], mask+1)
		q.mask = mask
		atomic.StoreUint64(&q.cap, uint64(cap))
	})
}

// Cap 返回队列最大容量
func (q *TypeLF[T]) Cap() int {
	if q.notInit() {
		return 0
	}
	return int(q.cap)
}

// Size 返回队列元素个数
func (q *TypeLF[T]) Size() int {
	tail := q.tail.Load()
	head := q.head.Load()
	return int(tail - head)
}

// Empty 返回队列是否为空
func (q *TypeLF[T]) Empty() bool {
	return q.tail.Load() == q.head.Load()
}

// Full 返回队列是否已满
func (q *TypeLF[T]) Full() bool {
	return q.tail.Load() == q.head.Load()+q.cap
}

// Push 是一个无锁队列的入队方法
// 参数:
//
//	val - 要入队的任意类型值
//
// 返回值:
//
//	bool - 成功入队返回true，队列已满返回false
func (q *TypeLF[T]) Push(val T) bool {
	q.lazyInit(defaultCap)
	for {
		tail := q.tail.Load()
		head := q.head.Load()
		if tail-head >= q.cap {
			return false // 队列已满。
		}
		slot := &q.data[tail&q.mask]

		// 如果 isUsed,表示 Pop 仍在清理此槽位。
		if slot.isUsed() {
			runtime.Gosched()
			continue
		}
		if q.tail.CompareAndSwap(tail, tail+1) {
			slot.Store(val)
			return true
		}
		runtime.Gosched()
	}
}

// Pop 是一个无锁队列的出队操作方法
// 它从队列中取出一个元素，如果队列为空则返回 (nil, false)
// 参数: 无
// 返回值: (any, bool) - 取出的元素和是否成功取出的标志
func (q *TypeLF[T]) Pop() (T, bool) {
	var zero T
	for {
		tail := q.tail.Load()
		head := q.head.Load()
		if head == tail {
			return zero, false // 队列为空。
		}
		slot := &q.data[head&q.mask]
		if !slot.isUsed() {
			runtime.Gosched()
			continue
		}
		if q.head.CompareAndSwap(head, head+1) {
			val := slot.LoadAndDelete()
			return val, true
		}
		runtime.Gosched()
	}
}

// PushBetch 是一个批量尽可能推入方法
// 返回成功推入队列的元素数量
// 错误分为：队列满和并发cas失败
func (q *TypeLF[T]) BatchPush(vals ...T) (int, error) {
	if len(vals) < 1 {
		return 0, nil
	}
	q.lazyInit(defaultCap)

	tail := q.tail.Load()
	head := q.head.Load()
	// 计算队列中可用的槽位数
	available := int(q.cap - (tail - head))
	if available < 1 {
		return 0, errTypeLFFull // 队列已满。
	}
	if available > len(vals) {
		available = len(vals)
	}

	// 预检测所有槽位是否可用
	mask := q.mask
	for i := 0; i < available; i++ {
		slot := &q.data[(tail+uint64(i))&mask]
		if slot.isUsed() {
			available = i
			break
		}
	}

	// 尝试更新队列尾部位置
	newTail := tail + uint64(available)
	if !q.tail.CompareAndSwap(tail, newTail) {
		return 0, errTypeLFCASFail
	}

	// 批量写入数据到队列
	for i := 0; i < available; i++ {
		slot := &q.data[(tail+uint64(i))&mask]
		slot.Store(vals[i])
	}
	return available, nil
}

// PushAll 全量入队，推入所有vals
func (q *TypeLF[T]) PushAll(vals ...T) (int, error) {
	if len(vals) < 1 {
		return 0, nil
	}
	q.lazyInit(defaultCap)

	tail := q.tail.Load()
	head := q.head.Load()
	// 计算队列中可用的槽位数
	available := int(q.cap - (tail - head))
	if available < 1 || available < len(vals) {
		return 0, errTypeLFFull // 队列已满。
	}
	available = len(vals)

	// 预检测所有槽位是否可用
	mask := q.mask
	for i := 0; i < available; i++ {
		slot := &q.data[(tail+uint64(i))&mask]
		if slot.isUsed() {
			return 0, errTypeLFFull
		}
	}

	// 尝试更新队列尾部位置
	newTail := tail + uint64(available)
	if !q.tail.CompareAndSwap(tail, newTail) {
		return 0, errTypeLFCASFail
	}

	// 批量写入数据到队列
	for i := 0; i < available; i++ {
		slot := &q.data[(tail+uint64(i))&mask]
		slot.Store(vals[i])
	}
	return available, nil
}

// BatchPop 是一个从无锁队列中批量弹出元素的方法
// 它接收一个整数参数 count，表示希望弹出的元素数量
// 返回实际弹出的元素数量元素的切片
func (q *TypeLF[T]) BatchPop(count int) []T {
	if count < 1 || q.notInit() {
		return nil
	}
	head := q.head.Load()
	tail := q.tail.Load()
	available := int(tail - head)
	if available < 1 {
		return nil
	}
	if available > count {
		available = count
	}
	mask := q.mask
	for i := 0; i < available; i++ {
		slot := &q.data[(head+uint64(i))&mask]
		if !slot.isUsed() {
			available = i
			break
		}
	}
	newHead := head + uint64(available)
	if !q.head.CompareAndSwap(head, newHead) {
		return nil
	}

	result := make([]T, available)
	actual := 0
	for i := 0; i < available; i++ {
		slot := &q.data[(head+uint64(i))&mask]
		result[i] = slot.LoadAndDelete()
		actual++
	}
	result = result[:actual]
	return result
}

// Head 返回队列头部元素
func (q *TypeLF[T]) Head() (T, bool) {
	for {
		tail := q.tail.Load()
		head := q.head.Load()
		mask := q.mask
		if head == tail {
			var zero T
			return zero, false // 队列为空。
		}
		slot := &q.data[head&mask]
		if !slot.isUsed() {
			runtime.Gosched()
			continue
		}
		val := slot.Load()
		return val, true
	}
}

// Head 返回队列尾部元素
func (q *TypeLF[T]) Tail() (T, bool) {
	// if q.notInit() {
	// 	return nil, false
	// }
	for {
		tail := q.tail.Load()
		head := q.head.Load()
		mask := q.mask
		if head == tail {
			var zero T
			return zero, false // 队列为空。
		}
		// tail-1，前面push之后tail=tail+1增加了，这里的实际地址是tail-1
		slot := &q.data[(tail-1)&mask]
		if !slot.isUsed() {
			runtime.Gosched()
			continue
		}
		val := slot.Load()
		return val, true
	}
}

// Clean 清空队列
func (q *TypeLF[T]) Clean() {
	for {
		tail := q.tail.Load()
		head := q.head.Load()
		mask := q.mask
		if head == tail {
			return // 队列为空。
		}
		size := int(tail - head)
		if !q.head.CompareAndSwap(head, tail) {
			continue
		}
		for i := 0; i < size; i++ {
			slot := &q.data[(head+uint64(i))&mask]
			slot.Delete()
		}
	}
}

// All 返回队列所有元素，
// 如果并发写入未完成则不返回
func (q *TypeLF[T]) All() []T {
	tail := q.tail.Load()
	head := q.head.Load()
	mask := q.mask
	if head == tail {
		return nil // 队列为空。
	}
	size := int(tail - head)
	result := make([]T, size)
	actual := 0

	for i := 0; i < size; i++ {
		slot := &q.data[(head+uint64(i))&mask]
		if !slot.isUsed() {
			// 并发写入未完成，终止
			break
		}
		result[actual] = slot.Load()
		actual++
	}
	result = result[:actual]
	return result
}
