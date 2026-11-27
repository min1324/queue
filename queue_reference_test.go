package queue_test

import (
	"errors"
	"sync"
	"sync/atomic"
	"unsafe"
)

// use for slice
const (
	/*
		1<< 20~28
		1048576		20
		2097152		21
		4194304		22
		8388608		23
		16777216	24
		33554432	25
		67108864	26
		134217728	27
		268435456	28
	*/
	prevCap = 1 << 22 // queue previous Push

	// stressTestCap 压力测试队列容量
	stressTestCap = 1 << 20
	// shortTestCap 短测试队列容量
	shortTestCap = 1 << 8

	// defaultTestCap 测试默认队列容量
	defaultTestCap = 1 << 8

	// DefaultCap 是未提供容量时的默认队列容量。
	// 选择2的幂次是为了高效的取模运算。
	DefaultCap = 1 << 8 // 256

	// MaxCap 是队列的最大容量。
	MaxCap = 1<<32 - 1 // ^uint64(0) >> 2
)

// Interface use in stack,queue testing
type Interface interface {
	Queue

	BatchPush(val ...any) (int, error)
	BatchPop(count int) []any
	Head() (value any, ok bool)
	Tail() (value any, ok bool)
	Full() bool
	Empty() bool
	Clean()
	All() []any
}

// Queue 定义了带有容量管理的无锁队列接口。
// 它扩展了基本的 Interface 接口，增加了查询和管理队列容量与长度的方法。
type Queue interface {
	// Push 将值添加到队列的头部（写入位置）。
	// 成功添加返回 true，队列已满返回 false。
	// 此操作是无锁的，可安全并发使用。
	Push(value any) bool

	// Pop 从队列的尾部（读取位置）移除并返回元素。
	// 成功返回值和 true，队列为空返回 nil 和 false。
	// 此操作是无锁的，可安全并发使用。
	Pop() (value any, ok bool)

	// Cap 返回队列的容量（最多可容纳的元素数量）。
	// 如果队列未初始化，返回 0。
	Cap() int

	// Len 返回队列中当前的元素数量。
	// 注意：在并发访问下，此值可能是近似值。
	// 负数结果会被钳制为 0。
	Size() int

	// Init 使用指定容量初始化队列。
	// 此方法只能调用一次，后续调用无效。
	// 如果 cap < 1，将使用默认容量（256）。
	// 如果未显式调用，将在首次 Push 操作时自动调用。
	Init(cap int)
}

// upPow2 返回 2^n-1，其中 2^n >= u，用于环形缓冲区的取模计算。
// 此位操作技术将值向上取整到下一个2的幂次减1，
// 使得可以通过按位与运算代替除法来实现高效的取模操作。
// upPow2(128) =127, upPow2(129) = 255。
// 如果 u 为 0 则返回 0，以避免下溢。
// 另见：Linux 内核 kfifo 实现。
func upPow2(u uint64) uint64 {
	if u == 0 {
		return 0
	}
	u -= 1 // 调整0值：确保最小结果
	u |= u >> 1
	u |= u >> 2
	u |= u >> 4
	u |= u >> 8
	u |= u >> 16
	u |= u >> 32 // 处理64位类型的高32位
	return u
}

type entryNil struct{}

var entryNilValue = (*entryNil)(nil)

// entry queue element
type entry struct {
	p unsafe.Pointer
}

func (n *entry) load() (any, bool) {
	p := atomic.LoadPointer(&n.p)
	if p == nil {
		return nil, false
	}
	val := *(*any)(p)
	if val == entryNilValue {
		val = nil
	}
	return val, true
}

func (n *entry) store(i any) {
	// Copy i to a heap-allocated variable to ensure the pointer
	// remains valid after store() returns. Without this, &i points
	// to a stack frame that may be reused.
	heapI := i
	if heapI == nil {
		heapI = entryNilValue
	}
	atomic.StorePointer(&n.p, unsafe.Pointer(&heapI))
}

func (n *entry) free() {
	atomic.StorePointer(&n.p, nil)
}

var errMuxRingFull error = errors.New("MuxRing: queue full")

// 双锁环形队列，可以并发Push和Pop
type MuxRing struct {
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

// 必须在锁里面调用
func (q *MuxRing) initInLock(cap int) {
	if cap < 1 {
		cap = DefaultCap
	}
	if uint64(cap) > MaxCap {
		panic("MuxRing: queue capacity overflow")
	}
	q.mask = upPow2(uint64(cap))
	q.cap = uint64(cap)
	q.head.Store(0)
	q.tail.Store(0)
	q.data = make([]entry, q.mask+1)
}

func (q *MuxRing) Init(cap int) {
	q.inMux.Lock()
	defer q.inMux.Unlock()
	if q.data == nil {
		q.initInLock(cap)
	}
}

func (q *MuxRing) Cap() int {
	if q.data == nil {
		return 0
	}
	return int(q.cap)
}

// Size return current number in queue
func (q *MuxRing) Size() int {
	tail := q.tail.Load()
	head := q.head.Load()
	return int(tail - head)
}

func (q *MuxRing) Empty() bool {
	return q.tail.Load() == q.head.Load()
}

func (q *MuxRing) Full() bool {
	return q.tail.Load() == q.head.Load()+q.cap
}

func (q *MuxRing) Push(val any) bool {
	if q.Full() && q.cap > 0 {
		return false
	}
	q.inMux.Lock()
	defer q.inMux.Unlock()
	if q.data == nil {
		q.initInLock(DefaultCap)
	}
	tail := q.tail.Load()
	head := q.head.Load()
	if tail-head >= q.cap {
		return false
	}
	slot := &q.data[tail&q.mask]
	slot.store(val)
	q.tail.Add(1)
	return true
}

func (q *MuxRing) Pop() (val any, ok bool) {
	if q.Empty() {
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
	slot.free()
	q.head.Add(1)
	return val, ok
}

func (q *MuxRing) Tail() (val any, ok bool) {
	if q.Empty() {
		return nil, false
	}
	q.outMux.Lock()
	defer q.outMux.Unlock()
	tail := q.tail.Load()
	head := q.head.Load()
	if tail == head {
		return nil, false
	}
	slot := &q.data[(tail-1)&q.mask]
	val, ok = slot.load()
	return val, true
}

func (q *MuxRing) Head() (val any, ok bool) {
	if q.Empty() {
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
	return val, true

}

func (q *MuxRing) BatchPush(val ...any) (int, error) {
	if len(val) < 1 {
		return 0, nil
	}
	q.inMux.Lock()
	defer q.inMux.Unlock()
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
	mask := q.mask
	for i := 0; i < availbale; i++ {
		slot := &q.data[(tail+uint64(i))&mask]
		slot.store(val[i])
	}
	q.tail.Add(uint64(availbale))
	return availbale, nil
}

func (q *MuxRing) BatchPop(count int) []any {
	if count < 1 || q.Empty() {
		return nil
	}
	q.outMux.Lock()
	defer q.outMux.Unlock()
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
	result := make([]any, availbale)
	mask := q.mask
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
	return result
}

func (q *MuxRing) All() []any {
	if q.Empty() {
		return nil
	}
	q.outMux.Lock()
	defer q.outMux.Unlock()
	tail := q.tail.Load()
	head := q.head.Load()
	mask := q.mask
	if tail == head {
		return nil
	}

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
	}
	result = result[:actual]
	return result
}

func (q *MuxRing) Clean() {
	if q.Empty() {
		return
	}
	q.inMux.Lock()
	defer q.inMux.Unlock()
	q.outMux.Lock()
	defer q.outMux.Unlock()
	tail := q.tail.Load()
	// tail == head+cap 表示队列已满，head == tail 表示队列为空。
	q.head.Store(tail)
	q.data = make([]entry, q.mask+1)
}
