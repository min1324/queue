package queue

import (
	"errors"
	"runtime"
	"sync"
	"sync/atomic"
	"unsafe"
)

const (
	cacheLineSize = 64
	defaultCap    = DefaultCap // LockFree 默认容量
	maxCap        = MaxCap     // LockFree 最大容量
)

var ErrLockFreeFull error = errors.New("LockFree: queue full")
var ErrBatchCASFail error = errors.New("LockFree: concurrent batch CAS failed")

// ------------------------ eface ------------------------

// eface 映射了空接口的运行时内部表示。
type eface struct {
	typ, data unsafe.Pointer
}

func newEface(val any) *eface {
	if val == nil {
		val = efaceNilVal
	}
	return (*eface)(unsafe.Pointer(&val))
}

// keep typ!=nil
func newAny(typ, data unsafe.Pointer) any {
	tmp := eface{typ: typ, data: data}
	val := *(*any)(unsafe.Pointer(&tmp))
	if typ == efaceNilTyp {
		val = nil
	}
	return val
}

type efaceNil struct{}

var (
	efaceNilVal any = (*efaceNil)(nil)
	efaceNilTyp     = (*eface)(unsafe.Pointer(&efaceNilVal)).typ
)

func init() {
	verifyEfaceLayout()
}

// verifyEfaceLayout 检查 eface 结构体是否与运行时匹配。
func verifyEfaceLayout() {
	var val any = int(42)
	ep := (*eface)(unsafe.Pointer(&val))
	if ep.typ == nil || ep.data == nil {
		panic("queue: eface layout does not match runtime interface layout")
	}
	var nilVal any
	nilEp := (*eface)(unsafe.Pointer(&nilVal))
	if nilEp.typ != nil || nilEp.data != nil {
		panic("queue: eface layout does not match runtime nil interface layout")
	}
}

// kfifo 返回 2^n-1，其中 2^n >= u，用于环形缓冲区的取模计算。
// 此位操作技术将值向上取整到下一个2的幂次减1，
// 使得可以通过按位与运算代替除法来实现高效的取模操作。
// kfifo(128) =127, kfifo(129) = 255。
// 如果 u 为 0 则返回 0，以避免下溢。
// 另见：Linux 内核 kfifo 实现。
func kfifo(u uint64) uint64 {
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

// ------------------------ LockFree ------------------------

// LockFree 是一个无锁环形缓冲区队列的实现。
type LockFree struct {
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
	//
	// 槽位状态语义：
	//   - typ == nil: 槽位为空（可 Push，对 Pop 表示队列为空）
	//   - typ != nil: 槽位已占用（可 Pop，对 Push 表示队列已满）
	// 状态转换规则：
	//   - 只有 Push 能将槽位从 nil 变为非 nil
	//   - 只有 Pop 能将槽位从非 nil 变为 nil
	data []eface
}

func New() QueueOpt {
	return &LockFree{}
}

// Init 初始化队列，只执行一次
func (q *LockFree) Init(cap int) {
	q.lazyInit(cap)
}

func (q *LockFree) notInit() bool {
	return atomic.LoadUint64(&q.cap) == 0
}

func (q *LockFree) lazyInit(cap int) {
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

		q.data = make([]eface, mask+1)
		q.mask = mask
		atomic.StoreUint64(&q.cap, uint64(cap))
	})
}

// Cap 返回队列最大容量
func (q *LockFree) Cap() int {
	if q.notInit() {
		return 0
	}
	return int(q.cap)
}

// Size 返回队列元素个数
func (q *LockFree) Size() int {
	tail := q.tail.Load()
	head := q.head.Load()
	return int(tail - head)
}

// Empty 返回队列是否为空
func (q *LockFree) Empty() bool {
	return q.tail.Load() == q.head.Load()
}

// Full 返回队列是否已满
func (q *LockFree) Full() bool {
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
func (q *LockFree) Push(val any) bool {
	q.lazyInit(defaultCap)

	tmp := newEface(val)
	for {
		tail := q.tail.Load()
		head := q.head.Load()
		if tail-head >= q.cap {
			return false // 队列已满。
		}
		slot := &q.data[tail&q.mask]

		// 如果 typ != nil，表示 Pop 仍在清理此槽位。
		typ := atomic.LoadPointer(&slot.typ)
		if typ != nil {
			runtime.Gosched()
			continue
		}
		if q.tail.CompareAndSwap(tail, tail+1) {
			// 先写入 data，然后发布 typ 作为存储屏障，
			atomic.StorePointer(&slot.data, tmp.data)
			atomic.StorePointer(&slot.typ, tmp.typ)
			return true
		}
		runtime.Gosched()
	}
}

// Pop 是一个无锁队列的出队操作方法
// 它从队列中取出一个元素，如果队列为空则返回 (nil, false)
// 参数: 无
// 返回值: (any, bool) - 取出的元素和是否成功取出的标志
func (q *LockFree) Pop() (any, bool) {
	for {
		tail := q.tail.Load()
		head := q.head.Load()
		if head == tail {
			return nil, false // 队列为空。
		}
		slot := &q.data[head&q.mask]
		typ := atomic.LoadPointer(&slot.typ)
		data := atomic.LoadPointer(&slot.data)
		if typ == nil {
			runtime.Gosched()
			continue
		}
		if q.head.CompareAndSwap(head, head+1) {
			// 我们先清除 data，然后通过原子写入 typ 来发布我们
			// 已完成此槽位使用的信号。
			atomic.StorePointer(&slot.data, nil)
			atomic.StorePointer(&slot.typ, nil)

			val := newAny(typ, data)
			return val, true
		}
		runtime.Gosched()
	}
}

// PushBetch 是一个批量尽可能推入方法
// 返回成功推入队列的元素数量
// 错误分为：队列满和并发cas失败
func (q *LockFree) BatchPush(vals ...any) (int, error) {
	if len(vals) < 1 {
		return 0, nil
	}
	q.lazyInit(defaultCap)

	tail := q.tail.Load()
	head := q.head.Load()
	// 计算队列中可用的槽位数
	available := int(q.cap - (tail - head))
	if available < 1 {
		return 0, ErrLockFreeFull // 队列已满。
	}
	if available > len(vals) {
		available = len(vals)
	}

	// 预检测所有槽位是否可用
	mask := q.mask
	for i := 0; i < available; i++ {
		slot := &q.data[(tail+uint64(i))&mask]
		if atomic.LoadPointer(&slot.typ) != nil {
			available = i
			break
		}
	}

	// 尝试更新队列尾部位置
	newTail := tail + uint64(available)
	if !q.tail.CompareAndSwap(tail, newTail) {
		return 0, ErrBatchCASFail
	}

	// 批量写入数据到队列
	for i := 0; i < available; i++ {
		tmp := newEface(vals[i])
		slot := &q.data[(tail+uint64(i))&mask]
		atomic.StorePointer(&slot.data, tmp.data)
		atomic.StorePointer(&slot.typ, tmp.typ)
	}
	return available, nil
}

// PushAll 全量入队，推入所有vals
func (q *LockFree) PushAll(vals ...any) (int, error) {
	if len(vals) < 1 {
		return 0, nil
	}
	q.lazyInit(defaultCap)

	tail := q.tail.Load()
	head := q.head.Load()
	// 计算队列中可用的槽位数
	available := int(q.cap - (tail - head))
	if available < 1 || available < len(vals) {
		return 0, ErrLockFreeFull // 队列已满。
	}
	available = len(vals)

	// 预检测所有槽位是否可用
	mask := q.mask
	for i := 0; i < available; i++ {
		slot := &q.data[(tail+uint64(i))&mask]
		if atomic.LoadPointer(&slot.typ) != nil {
			return 0, ErrLockFreeFull
		}
	}

	// 尝试更新队列尾部位置
	newTail := tail + uint64(available)
	if !q.tail.CompareAndSwap(tail, newTail) {
		return 0, ErrBatchCASFail
	}

	// 批量写入数据到队列
	for i := 0; i < available; i++ {
		tmp := newEface(vals[i])
		slot := &q.data[(tail+uint64(i))&mask]
		atomic.StorePointer(&slot.data, tmp.data)
		atomic.StorePointer(&slot.typ, tmp.typ)
	}
	return available, nil
}

// // BatchPop 是一个从无锁队列中批量弹出元素的方法
// // 它接收一个整数参数 count，表示希望弹出的元素数量
// // 返回实际弹出的元素数量元素的切片
// func (q *LockFree) BatchPop(count int) []any {
// 	if count < 1 || q.notInit() {
// 		return nil
// 	}
// 	size := q.Size()
// 	if size < 1 {
// 		return nil
// 	}
// 	if count > size {
// 		count = size
// 	}
// 	result := make([]any, count)
// 	actual := 0
// 	for i := 0; i < count; i++ {
// 		val, ok := q.Pop()
// 		if !ok {
// 			break
// 		}
// 		result[actual] = val
// 		actual++
// 	}
// 	// 调整结果切片大小，只包含实际获取的元素
// 	result = result[:actual]
// 	return result
// }

// BatchPop 是一个从无锁队列中批量弹出元素的方法
// 它接收一个整数参数 count，表示希望弹出的元素数量
// 返回实际弹出的元素数量元素的切片
func (q *LockFree) BatchPop(count int) []any {
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
		slotTyp := atomic.LoadPointer(&slot.typ)
		if slotTyp == nil {
			available = i
			break
		}
	}
	newHead := head + uint64(available)
	if !q.head.CompareAndSwap(head, newHead) {
		return nil
	}

	result := make([]any, available)
	actual := 0
	for i := 0; i < available; i++ {
		slot := &q.data[(head+uint64(i))&mask]
		data := atomic.SwapPointer(&slot.data, nil)
		typ := atomic.SwapPointer(&slot.typ, nil)
		result[i] = newAny(typ, data)
		actual++
	}
	result = result[:actual]
	return result
}

// Head 返回队列头部元素
func (q *LockFree) Head() (any, bool) {
	for {
		tail := q.tail.Load()
		head := q.head.Load()
		mask := q.mask
		if head == tail {
			return nil, false // 队列为空。
		}
		slot := &q.data[head&mask]
		typ := atomic.LoadPointer(&slot.typ)
		data := atomic.LoadPointer(&slot.data)
		if typ == nil {
			runtime.Gosched()
			continue
		}
		val := newAny(typ, data)
		return val, true
	}
}

// Head 返回队列尾部元素
func (q *LockFree) Tail() (any, bool) {
	// if q.notInit() {
	// 	return nil, false
	// }
	for {
		tail := q.tail.Load()
		head := q.head.Load()
		mask := q.mask
		if head == tail {
			return nil, false // 队列为空。
		}
		// tail-1，前面push之后tail=tail+1增加了，这里的实际地址是tail-1
		slot := &q.data[(tail-1)&mask]
		typ := atomic.LoadPointer(&slot.typ)
		data := atomic.LoadPointer(&slot.data)
		if typ == nil {
			runtime.Gosched()
			continue
		}
		val := newAny(typ, data)
		return val, true
	}
}

// Clean 清空队列
func (q *LockFree) Clean() {
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
			atomic.StorePointer(&slot.data, nil)
			atomic.StorePointer(&slot.typ, nil)
		}
	}
}

// All 返回队列所有元素，
// 如果并发写入未完成则不返回
func (q *LockFree) All() []any {
	tail := q.tail.Load()
	head := q.head.Load()
	mask := q.mask
	if head == tail {
		return nil // 队列为空。
	}
	size := int(tail - head)
	result := make([]any, size)
	actual := 0

	for i := 0; i < size; i++ {
		slot := &q.data[(head+uint64(i))&mask]
		typ := atomic.LoadPointer(&slot.typ)
		data := atomic.LoadPointer(&slot.data)
		if typ == nil {
			// 并发写入未完成，终止
			break
		}
		result[actual] = newAny(typ, data)
		actual++
	}
	result = result[:actual]
	return result
}
