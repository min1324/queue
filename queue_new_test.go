package queue_test

import (
	"runtime"
	"sync"
	"sync/atomic"
	"unsafe"
)

const (
	initSize = 1 << 8 // 256
)

// --- 职责1: 底层内存布局与无锁原子存储 ---
// queueNilVal 是一个哨兵值，用于在队列中表示 interface{}(nil)。
type nilQueueNil struct{}

type any interface{}

var queueNilVal any = (*nilQueueNil)(nil)

// eface 映射了空接口的运行时内部表示。
type eface struct {
	typ, data unsafe.Pointer
}

// atomicSlot 封装了对单个槽位的无锁原子读写和状态管理。
// 它负责处理 unsafe.Pointer、内存屏障和 GC 可见性。
type atomicSlot struct {
	typ, data unsafe.Pointer
}

func (s *atomicSlot) Store(val any) {
	if val == nil {
		val = queueNilVal
	}
	ep := (*eface)(unsafe.Pointer(&val))
	atomic.StorePointer(&s.data, ep.data)
	atomic.StorePointer(&s.typ, ep.typ) // 发布屏障
}

func (s *atomicSlot) Load() any {
	typ := atomic.LoadPointer(&s.typ)
	if typ == nil {
		return nil // 槽位为空或数据尚未就绪
	}
	data := atomic.LoadPointer(&s.data)
	tmp := eface{typ: typ, data: data}
	return *(*any)(unsafe.Pointer(&tmp))
}

func (s *atomicSlot) Release() {
	atomic.StorePointer(&s.data, nil)
	atomic.StorePointer(&s.typ, nil) // 发布屏障
}

func (s *atomicSlot) IsOccupied() bool {
	return atomic.LoadPointer(&s.typ) != nil
}

// newAtomicSlice 创建指定大小的槽位切片，并验证运行时布局。
func newAtomicSlice(cap int) []atomicSlot {
	verifyEfaceLayout()
	return make([]atomicSlot, modUint64(uint64(cap))+1)
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

// --- 职责2: 队列业务逻辑与环形缓冲区管理 ---
// LockFree 现在只关注队列的索引移动、空满判断和自旋逻辑。

type LockFree struct {
	once sync.Once
	cap  uint64       // 队列容量
	mod  uint64       // 用于取模的掩码 (len(data) - 1)
	data []atomicSlot // 环形缓冲区存储

	// 使用 atomic.Uint64 管理索引，利用其内部自带的锁禁用机制
	// 替代手动的 runtime_procPin，使代码更安全且符合 Go 内存模型。
	head atomic.Uint64
	tail atomic.Uint64
}

func New() *LockFree {
	return &LockFree{}
}

func (q *LockFree) Init(cap int) {
	q.once.Do(func() {
		if cap < 1 {
			cap = initSize
		}
		q.mod = modUint64(uint64(cap))
		q.cap = uint64(cap)
		q.data = newAtomicSlice(cap)
	})
}

func (q *LockFree) Cap() int {
	return int(q.cap)
}

func (q *LockFree) Len() int {
	return int(q.tail.Load() - q.head.Load())
}

func (q *LockFree) Push(val any) bool {
	if q.data == nil {
		q.Init(initSize)
	}
	for {
		tail := q.tail.Load()
		head := q.head.Load()
		if tail == head+q.cap {
			return false // 队列已满
		}

		slot := &q.data[tail&q.mod]

		// 如果槽位被占用，说明 Pop 还没清理完，自旋等待
		if slot.IsOccupied() {
			runtime.Gosched()
			continue
		}

		// CAS 抢占 tail 索引
		if q.tail.CompareAndSwap(tail, tail+1) {
			slot.Store(val) // 独占槽位后，安全写入
			return true
		}
		runtime.Gosched()
	}
}

func (q *LockFree) Pop() (any, bool) {
	if q.data == nil {
		return nil, false
	}
	for {
		head := q.head.Load()
		tail := q.tail.Load()
		if head == tail {
			return nil, false // 队列为空
		}

		slot := &q.data[head&q.mod]

		// 如果槽位为空，说明 Push 还没写完数据，自旋等待
		if !slot.IsOccupied() {
			runtime.Gosched()
			continue
		}

		// CAS 抢占 head 索引
		if q.head.CompareAndSwap(head, head+1) {
			val := slot.Load() // 独占槽位后，安全读取
			slot.Release()     // 清空槽位，交还给 Push
			if val == queueNilVal {
				val = nil
			}
			return val, true
		}
		runtime.Gosched()
	}
}

// modUint64 返回 2^n-1，其中 2^n >= u
func modUint64(u uint64) uint64 {
	if u == 0 {
		return 0
	}
	u -= 1
	u |= u >> 1
	u |= u >> 2
	u |= u >> 4
	u |= u >> 8
	u |= u >> 16
	u |= u >> 32
	return u
}

// Queue 和 Interface 接口定义保持不变...
