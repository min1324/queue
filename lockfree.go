package queue

import (
	"runtime"
	"sync"
	"sync/atomic"
	"unsafe"
)

const (
	cacheLineSize = 64
)

// ------------------------ eface ------------------------

// eface 映射了空接口的运行时内部表示。
type eface struct {
	typ, data unsafe.Pointer
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

	cap  uint64 // 队列容量
	mask uint64 // 2^n-1 > cap

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
		q.mask = kfifo(uint64(cap))
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
	return q.tail.Load() == q.head.Load()
}

func (q *LockFree) IsFull() bool {
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
	if q.data == nil {
		q.Init(initSize)
	}
	// 处理nil值，使用特殊值代替
	if val == nil {
		val = efaceNilVal
	}
	tmp := (*eface)(unsafe.Pointer(&val))
	for {
		// 获取当前队列的尾部和头部位置
		tail := q.tail.Load()
		head := q.head.Load()
		// 检查队列是否已满
		if tail-head >= q.cap {
			return false // 队列已满。
		}
		slot := &q.data[tail&q.mask]

		// 检查头部槽位是否已被 pop 释放。
		// 如果 typ != nil，表示 Pop 仍在清理此槽位。
		// 自旋等待而不是立即返回 false，
		// 因为该槽位很快就会可用。
		typ := atomic.LoadPointer(&slot.typ)
		if typ != nil {
			runtime.Gosched()
			continue
		}
		// 递增 head。这将槽位的所有权传递给 pop
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
	if q.data == nil {
		return nil, false
	}
	for {
		tail := q.tail.Load()
		head := q.head.Load()
		if head == tail {
			return nil, false // 队列为空。
		}
		slot := &q.data[head&q.mask]
		typ := atomic.LoadPointer(&slot.typ)
		if typ == nil {
			runtime.Gosched()
			continue
		}
		if q.head.CompareAndSwap(head, head+1) {
			// 以确保正确的内存排序并避免弱排序架构（如 ARM）上的数据竞争。
			// 再次读取槽位的类型和数据，确保获取的是最新的值
			slotTyp := atomic.LoadPointer(&slot.typ)
			slotData := atomic.LoadPointer(&slot.data)

			// 我们先清除 data，然后通过原子写入 typ 来发布我们
			// 已完成此槽位使用的信号。
			atomic.StorePointer(&slot.data, nil)
			atomic.StorePointer(&slot.typ, nil)

			if slotTyp == nil || slotTyp == efaceNilTyp {
				return nil, slotTyp == efaceNilTyp
			}
			tmp := eface{typ: slotTyp, data: slotData}
			val := *(*any)(unsafe.Pointer(&tmp))
			return val, true
		}
		runtime.Gosched()
	}
}

// PushBetch 是一个无锁队列的批量推入方法
// 它接受可变数量的参数 vals，并成功推入队列的元素数量
func (q *LockFree) BatchPush(vals ...any) int {
	if len(vals) < 1 {
		return 0
	}
	if q.data == nil {
		q.Init(initSize)
	}
	tail := q.tail.Load()
	head := q.head.Load()
	// 计算队列中可用的槽位数
	available := int(q.cap - (tail - head))
	if available < 1 {
		return 0
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
		return 0
	}

	// 批量写入数据到队列
	for i := 0; i < available; i++ {
		slot := &q.data[(tail+uint64(i))&mask]
		val := vals[i]
		if val == nil {
			val = efaceNilVal
		}
		tmp := (*eface)(unsafe.Pointer(&val))
		atomic.StorePointer(&slot.data, tmp.data)
		atomic.StorePointer(&slot.typ, tmp.typ)
	}
	return available
}

// BatchPop 是一个从无锁队列中批量弹出元素的方法
// 它接收一个整数参数 count，表示希望弹出的元素数量
// 返回一个包含最多 count 个元素的切片和实际弹出的元素数量
func (q *LockFree) BatchPop(count int) ([]any, int) {
	if count < 1 {
		return nil, 0
	}
	result := make([]any, count)
	// actual 用于记录实际弹出的元素数量
	actual := 0
	for i := 0; i < count; i++ {
		val, ok := q.Pop()
		if !ok {
			break
		}
		result[actual] = val
		actual++
	}
	// 调整结果切片大小，只包含实际获取的元素
	result = result[:actual]
	return result, actual
}

// func (q *LockFree) PopBetch1(count int) ([]any, int) {
// 	if count < 1 {
// 		return nil, 0
// 	}
// 	if q.data == nil {
// 		return nil, 0
// 	}
// 	head := q.head.Load()
// 	tail := q.tail.Load()

// 	available := int(tail - head)
// 	if available < 1 {
// 		return nil, 0
// 	}
// 	if available > count {
// 		available = count
// 	}

// 	// 尝试更新
// 	result := make([]any, available)
// 	actual := 0
// 	mask := q.mask
// 	success := true

// 	for i := 0; i < available; i++ {
// 		slot := &q.data[(head+uint64(i))&mask]
// 		slotTyp := atomic.LoadPointer(&slot.typ)
// 		slotData := atomic.LoadPointer(&slot.data)
// 		if slotTyp == nil {
// 			success = false
// 			break
// 		}

// 		tmp := eface{typ: slotTyp, data: slotData}
// 		val := *(*any)(unsafe.Pointer(&tmp))
// 		if slotTyp == queueNilTyp {
// 			val = nil
// 		}
// 		result[i] = val
// 		actual++
// 	}
// 	if !success {
// 		result = result[:actual]
// 	}
// 	newHead := head + uint64(actual)
// 	if !q.head.CompareAndSwap(head, newHead) {
// 		return nil, 0
// 	}
// 	// 批量清理已读取的槽位
// 	for i := 0; i < actual; i++ {
// 		slot := &q.data[(head+uint64(i))&mask]
// 		atomic.StorePointer(&slot.data, nil)
// 		atomic.StorePointer(&slot.typ, nil)
// 	}
// 	return result, actual
// }
