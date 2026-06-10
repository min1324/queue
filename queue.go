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
	initSize = 1 << 8 // 256
)

// queueNilVal 是一个哨兵值，用于在队列中表示 interface{}(nil)。
// 由于 nil 被用来表示空槽位，我们需要一个用户无法意外构造的哨兵值。
// 未导出的 nilQueueNil 类型确保外部代码无法创建与 queueNilVal 相等的值。
type nilQueueNil struct{}

var queueNilVal any = (*nilQueueNil)(nil)

type any = interface{}

// New 创建并返回一个新的空的无锁队列。
// 如果未显式初始化，队列将在首次 Push 操作时自动使用默认容量（256）进行初始化。
func New() Queue {
	return &LockFree{}
}

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

	// Resize 将队列容量更改为 newCap。
	// 如果无法调整容量则返回 false，例如：
	//   - newCap 小于当前元素数量
	//   - 队列尚未初始化
	// 此方法可安全并发调用，但会阻塞直到所有进行中的 Push/Pop 操作完成后再迁移元素。
	Resize(newCap int) bool
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

// LockFree 是一个无锁环形缓冲区队列的实现。
// 它为 Push 和 Pop 操作提供高性能的并发访问，无需互斥锁。
type LockFree struct {
	once sync.Once
	mu   sync.Mutex // 保护 Resize 操作

	tail uint64 // 指向下次读取数据的位置 (tail & mod)
	head uint64 // 指向下次写入数据的位置 (head & mod)

	// 环形缓冲区存储，大小始终为2的幂次。
	// 槽位状态语义：
	//   - val == nil: 槽位为空（可 Push，对 Pop 表示队列为空）
	//   - val != nil: 槽位已占用（可 Pop，对 Push 表示队列已满）
	// 状态转换规则：
	//   - 只有 Push 能将槽位从 nil 变为非 nil
	//   - 只有 Pop 能将槽位从非 nil 变为 nil
	data []eface
}

// Init 使用指定容量初始化队列。
// 此方法只能调用一次，后续调用无效。
// 如果 cap < 1，将使用默认容量（256）。
// 实际容量会向上取整到2的幂次减1。
// 如果未显式调用，将在首次 Push 操作时自动调用。
func (q *LockFree) Init(cap int) {
	q.once.Do(func() {
		if cap < 1 {
			cap = initSize
		}
		verifyEfaceLayout()
		q.data = make([]eface, modUint64(uint64(cap))+1)
	})
}

// verifyEfaceLayout 检查我们的 eface 结构体是否与运行时的
// interface{} 布局匹配。这是一个构建时和初始化时的安全检查，
// 用于捕获跨 Go 版本的不兼容性。
func verifyEfaceLayout() {
	// 创建一个非 nil 的 interface 值，验证我们的 eface 结构体
	// 能否正确解码它。
	var val interface{} = int(42)
	ep := (*eface)(unsafe.Pointer(&val))
	if ep.typ == nil || ep.data == nil {
		panic("queue: eface layout does not match runtime interface layout; " +
			"this Go version may have changed the internal interface representation. " +
			"Please file a bug report.")
	}
	// 验证 nil interface 的两个字段都为 nil。
	var nilVal interface{}
	nilEp := (*eface)(unsafe.Pointer(&nilVal))
	if nilEp.typ != nil || nilEp.data != nil {
		panic("queue: eface layout does not match runtime nil interface layout; " +
			"this Go version may have changed the internal interface representation. " +
			"Please file a bug report.")
	}
}

// Cap 返回队列的容量（最多可容纳的元素数量）。
// 如果队列未初始化，返回 0。
func (q *LockFree) Cap() int {
	return len(q.data)
}

// Resize 将队列容量更改为 newCap。
// 如果 newCap 小于队列中当前的元素数量，或队列尚未初始化，则返回 false。
// Resize 会阻塞直到所有进行中的 Push/Pop 操作完成，
// 然后将现有元素迁移到新的底层数组。
func (q *LockFree) Resize(newCap int) bool {
	if newCap < 1 {
		newCap = initSize
	}
	q.mu.Lock()
	defer q.mu.Unlock()

	if q.data == nil {
		return false
	}

	currentLen := int(int64(atomic.LoadUint64(&q.head) - atomic.LoadUint64(&q.tail)))
	if currentLen < 0 {
		currentLen = 0
	}
	if newCap < currentLen {
		return false
	}

	newMod := modUint64(uint64(newCap))
	newData := make([]eface, newMod+1)

	oldMod := uint64(len(q.data) - 1)

	// 将旧数据中的现有元素迁移到新数据中。
	oldTail := atomic.LoadUint64(&q.tail)
	oldHead := atomic.LoadUint64(&q.head)
	for i := oldTail; i < oldHead; i++ {
		oldSlot := &q.data[i&oldMod]
		newSlot := &newData[i&newMod]
		// 原子读取旧槽位
		newSlot.typ = atomic.LoadPointer(&oldSlot.typ)
		newSlot.data = atomic.LoadPointer(&oldSlot.data)
	}

	// 更新数据指针。由于 mu 互斥锁确保没有并发的 Push/Pop 操作正在进行，所以这是安全的。
	q.data = newData

	return true
}

// Len 返回队列中当前的元素数量。
// 注意：head 和 tail 使用原子操作读取，但由于它们是分开读取的，
// 在高并发访问下结果可能不准确。负数结果会被钳制为 0。
func (q *LockFree) Len() int {
	l := int(int64(atomic.LoadUint64(&q.head) - atomic.LoadUint64(&q.tail)))
	if l < 0 {
		return 0
	}
	return l
}

// Push 将值添加到队列的头部（写入位置）。
// 如果队列已满，返回 false。
// 此操作是无锁的，可安全并发使用。
// 如果队列尚未初始化，将使用默认容量（256）自动初始化。
func (q *LockFree) Push(val any) bool {
	if q.data == nil {
		q.Init(initSize)
	}
	var slot *eface
	mod := uint64(len(q.data) - 1)
	for {
		head := atomic.LoadUint64(&q.head)
		tail := atomic.LoadUint64(&q.tail)
		if head == mod+1+tail {
			// 队列已满。
			return false
		}
		slot = &q.data[head&mod]

		// 检查头部槽位是否已被 popTail 释放。
		// 如果 typ != nil，表示 Pop 仍在清理此槽位。
		// 自旋等待而不是立即返回 false，
		// 因为该槽位很快就会可用。
		typ := atomic.LoadPointer(&slot.typ)
		if typ != nil {
			runtime.Gosched()
			continue
		}
		// 递增 head。这将槽位的所有权传递给 popTail，
		// 并作为写入槽位的存储屏障。
		runtime_procPin()
		if atomic.CompareAndSwapUint64(&q.head, head, head+1) {
			// 头部槽位空闲，我们拥有它。
			if val == nil {
				val = queueNilVal
			}
			// 对 typ 和 data 都使用原子存储，以确保
			// 正确的内存排序和 GC 可见性。
			// 先写入 data，然后发布 typ 作为存储屏障，
			// 使数据对 Pop 可见。
			atomic.StorePointer(&slot.data, (*eface)(unsafe.Pointer(&val)).data)
			atomic.StorePointer(&slot.typ, (*eface)(unsafe.Pointer(&val)).typ)
			runtime_procUnpin()
			return true
		}
		runtime_procUnpin()
		runtime.Gosched()
	}
}

// Pop 从队列的尾部（读取位置）移除并返回元素。
// 成功返回值和 true，队列为空返回 nil 和 false。
// 此操作是无锁的，可安全并发使用。
// 如果队列尚未初始化，返回 nil, false。
func (q *LockFree) Pop() (any, bool) {
	var slot *eface
	if q.data == nil {
		return nil, false
	}
	mod := uint64(len(q.data) - 1)
	for {
		head := atomic.LoadUint64(&q.head)
		tail := atomic.LoadUint64(&q.tail)
		if head == tail {
			// 队列为空。
			return nil, false
		}

		// 确认 head 和 tail（用于上面的推测性检查）并递增 tail。
		// 如果成功，我们就拥有 tail 位置的槽位。
		slot = &q.data[tail&mod]
		typ := atomic.LoadPointer(&slot.typ)
		// 如果 typ == nil，表示 Push 已声明该槽位（head 已移动），
		// 但尚未完成数据写入（例如，协程在 CAS 和 StorePointer
		// 之间被抢占）。自旋等待而不是立即返回 false，
		// 因为数据很快就会可用。
		if typ == nil {
			runtime.Gosched()
			continue
		}
		runtime_procPin()
		if atomic.CompareAndSwapUint64(&q.tail, tail, tail+1) {

			// 我们现在拥有该槽位。使用原子加载读取两个字段，
			// 以确保正确的内存排序并避免弱排序架构（如 ARM）上的数据竞争。
			slotTyp := atomic.LoadPointer(&slot.typ)
			slotData := atomic.LoadPointer(&slot.data)
			tmp := eface{typ: slotTyp, data: slotData}
			val := *(*any)(unsafe.Pointer(&tmp))
			if val == queueNilVal {
				val = nil
			}

			// 通知 pushHead 我们已完成此槽位的使用。清零槽位也很重要，
			// 这样我们就不会留下可能导致对象存活时间超过必要的引用。
			//
			// 我们先清除 data，然后通过原子写入 typ 来发布我们
			// 已完成此槽位使用的信号。
			// 两者都必须使用原子存储，以确保正确的内存排序，
			// 并防止 GC 看到不一致的 eface 状态。
			atomic.StorePointer(&slot.data, nil)
			atomic.StorePointer(&slot.typ, nil)
			// 此时 pushHead 拥有该槽位。
			runtime_procUnpin()
			return val, true
		}
		runtime_procUnpin()
		runtime.Gosched()
	}
}

// eface 映射了空接口（interface{}）的运行时内部表示。
// 它包含两个 unsafe.Pointer：typ（类型描述符）和 data（值指针）。
// 此结构体用于对 interface 值进行原子操作，而不会触发
// 竞态检测器或违反 Go 的内存模型保证。
type eface struct {
	typ, data unsafe.Pointer
}

// modUint32 返回 2^n-1，其中 2^n >= u，用于环形缓冲区的取模计算。
// 此位操作技术将值向上取整到下一个2的幂次减1，
// 使得可以通过按位与运算代替除法来实现高效的取模操作。
// 例如：modUint32(128) = 127，modUint32(129) = 255。
// 另见：Linux 内核 kfifo 实现。
func modUint32(u uint32) uint32 {
	if u == 0 {
		return 0
	}
	u -= 1 // 调整0值：确保最小结果，例如 128 -> 127，而非 255
	u |= u >> 1
	u |= u >> 2
	u |= u >> 4
	u |= u >> 8  // 32位类型已足够
	u |= u >> 16 // 处理高16位
	return u
}

// modUint64 返回 2^n-1，其中 2^n >= u，用于环形缓冲区的取模计算。
// 这是 modUint32 的64位版本，增加了对高32位的移位处理。
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


