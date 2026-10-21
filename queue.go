package queue

import (
	"runtime"
	"sync"
	"sync/atomic"
	"unsafe"
)

const (
	// initSize 是未提供容量时的默认队列容量。
	// 选择2的幂次是为了高效的取模运算。
	initSize  = 1 << 8 // 256
	maxUint64 = ^uint64(0) >> 2
)

type any = interface{}

// Queue 定义了带有容量管理的无锁队列接口。
// 它扩展了基本的 Interface 接口，增加了查询和管理队列容量与长度的方法。
type Queue interface {
	Interface

	// Cap 返回队列的容量（最多可容纳的元素数量）。
	// 如果队列未初始化，返回 0。
	Cap() int

	// Len 返回队列中当前的元素数量。
	// 注意：在并发访问下，此值可能是近似值。
	// 负数结果会被钳制为 0。
	Len() int

	// Init 使用指定容量初始化队列。
	// 此方法只能调用一次，后续调用无效。
	// 如果 cap < 1，将使用默认容量（256）。
	// 如果未显式调用，将在首次 Push 操作时自动调用。
	Init(cap int)
}

// Interface 定义了队列的基本操作接口。
// 此接口提供了所有队列实现必须支持的基本入队和出队操作。
type Interface interface {
	// Push 将值添加到队列的头部（写入位置）。
	// 成功添加返回 true，队列已满返回 false。
	// 此操作是无锁的，可安全并发使用。
	Push(value any) bool

	// Pop 从队列的尾部（读取位置）移除并返回元素。
	// 成功返回值和 true，队列为空返回 nil 和 false。
	// 此操作是无锁的，可安全并发使用。
	Pop() (value any, ok bool)
}

// queueNilVal 是一个哨兵值，用于在队列中表示 interface{}(nil)。
// 由于 nil 被用来表示空槽位，我们需要一个用户无法意外构造的哨兵值。
// 未导出的 nilQueueNil 类型确保外部代码无法创建与 queueNilVal 相等的值。
type nilQueueNil struct{}

var queueNilVal any = (*nilQueueNil)(nil)

var queueNilTyp = (*eface)(unsafe.Pointer(&queueNilVal)).typ

// eface 映射了空接口的运行时内部表示。
type eface struct {
	typ, data unsafe.Pointer
}

// entry queue element
type entry struct {
	typ, data unsafe.Pointer
}

func (n *entry) load() (val any, ok bool) {
	typ := atomic.LoadPointer(&n.typ)
	if typ == nil {
		return nil, false
	}
	data := atomic.LoadPointer(&n.data)
	if typ == queueNilTyp {
		return nil, true
	}
	ef := eface{typ: typ, data: data}
	tmp := *(*any)(unsafe.Pointer(&ef))
	return tmp, true
}

func (n *entry) store(val any) {
	if val == nil {
		val = queueNilVal
	}
	ef := (*eface)(unsafe.Pointer(&val))
	// 先写入 data，后写入 typ
	atomic.StorePointer(&n.data, ef.data)
	atomic.StorePointer(&n.typ, ef.typ)
}

func (n *entry) free() {
	//  先清 typ (撤销守卫，阻止 Pop 继续读取)

	atomic.StorePointer(&n.data, nil)
	atomic.StorePointer(&n.typ, nil)
}

func (n *entry) isNil() bool {
	return atomic.LoadPointer(&n.typ) == nil
}

// verifyEfaceLayout 检查 eface 结构体是否与运行时匹配。
func verifyEfaceLayout() {
	var val interface{} = int(42)
	ep := (*eface)(unsafe.Pointer(&val))
	if ep.typ == nil || ep.data == nil {
		panic("queue: eface layout does not match runtime interface layout")
	}
	var nilVal interface{}
	nilEp := (*eface)(unsafe.Pointer(&nilVal))
	if nilEp.typ != nil || nilEp.data != nil {
		panic("queue: eface layout does not match runtime nil interface layout")
	}
}

// LockFree 是一个无锁环形缓冲区队列的实现。
// 它为 Push 和 Pop 操作提供高性能的并发访问，无需互斥锁。
type LockFree struct {
	once sync.Once

	cap  uint64        // 队列容量
	mask uint64        // 队列容量 - 1
	tail atomic.Uint64 // 指向下次写入数据的位置 (tail & mod)
	head atomic.Uint64 // 指向下次读取数据的位置 (head & mod)

	// 环形缓冲区存储，大小始终为2的幂次。
	//
	// tail == head+cap 表示队列已满，head == tail 表示队列为空。
	//
	// 槽位状态语义：
	//   - val == nil: 槽位为空（可 Push，对 Pop 表示队列为空）
	//   - val != nil: 槽位已占用（可 Pop，对 Push 表示队列已满）
	// 状态转换规则：
	//   - 只有 Push 能将槽位从 nil 变为非 nil
	//   - 只有 Pop 能将槽位从非 nil 变为 nil
	data []entry
}

// func (q *LockFree) Head() *atomic.Uint64 {
// 	return &q.head
// }

// func (q *LockFree) Tail() *atomic.Uint64 {
// 	return &q.tail
// }

// New 创建并返回一个新的空的无锁队列。
// 如果未显式初始化，队列将在首次 Push 操作时自动使用默认容量（255）进行初始化。
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
		verifyEfaceLayout()
		q.mask = modUint64(uint64(cap))
		q.data = make([]entry, q.mask+1)
		q.cap = uint64(cap)
		q.head.Store(0)
		q.tail.Store(0)
	})
}

func (q *LockFree) Cap() int {
	return int(q.cap)
}

func (q *LockFree) Len() int {
	l := int(q.tail.Load() - q.head.Load())
	if l < 0 {
		return 0
	}
	return l
}

func (q *LockFree) Push(val any) bool {
	return q.pushTail(val)
}

func (q *LockFree) pushTail(val any) bool {
	if q.data == nil {
		q.Init(initSize)
	}
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
		runtime.Gosched()
	}
}

func (q *LockFree) Pop() (any, bool) {
	return q.popHead()
}

func (q *LockFree) popHead() (any, bool) {
	if q.data == nil {
		return nil, false
	}
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
			slot.free()
			return val, ok
		}
		runtime.Gosched()
	}
}

// modUint64 返回 2^n-1，其中 2^n >= u，用于环形缓冲区的取模计算。
// 此位操作技术将值向上取整到下一个2的幂次减1，
// 使得可以通过按位与运算代替除法来实现高效的取模操作。
// 例如：modUint32(128) = 127，modUint32(129) = 255。
// 如果 u 为 0 则返回 0，以避免下溢。
// 另见：Linux 内核 kfifo 实现。
func modUint64(u uint64) uint64 {
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
