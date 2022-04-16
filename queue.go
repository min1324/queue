package queue

import (
	"sync"
	"sync/atomic"
	"unsafe"
)

const (
	// initSize init when not provite cap,use initSize
	initSize = 1 << 8
)

// queueNil is used in queue to represent interface{}(nil).
// Since we use nil to represent empty slots, we need a sentinel value
// to represent nil.
type queueNil *struct{}

type any = interface{}

// New return an empty queue.
func New() Queue {
	return &LFQueue{}
}

type Queue interface {
	Interface
	Cap() int
	Len() int
	Init(cap int)
}

type Interface interface {
	Push(value any) bool
	Pop() (value any, ok bool)
}

var _ Queue = &LFQueue{}

// LFQueue is a lock-free ring array queue.
type LFQueue struct {
	count uint32
	lfQueue
}

func (q *LFQueue) Push(value any) bool {
	if q.lfQueue.Push(value) {
		atomic.AddUint32(&q.count, 1)
		return true
	}
	return false
}

func (q *LFQueue) Pop() (value any, ok bool) {
	value, ok = q.lfQueue.Pop()
	if ok {
		atomic.AddUint32(&q.count, ^uint32(0))
	}
	return
}

func (q *LFQueue) Cap() int {
	return len(q.data)
}

func (q *LFQueue) Len() int {
	return int(atomic.LoadUint32(&q.count))
}

func (q *LFQueue) Init(cap int) {
	q.lfQueue.Init(cap)
}

// lfQueue is a lock-free ring array queue.
type lfQueue struct {
	once sync.Once

	tail uint32 // 指向下次取出数据的位置:tail&mod
	head uint32 // 指向下次写入数据的位置:head&mod

	// 环形队列，大小必须是2的倍数。
	// val为空，表示可以Push,如果是Pop操作，表示队列空。
	// val不为空，表所可以Pop,如果是Push操作，表示队列满了。
	// 只能由Push将val从nil变成非nil,
	// 只能由Pop将val从非nil变成nil.
	data []eface
}

// Init initialize queue use cap
// it only execute once time.
// if cap<1, will use 256.
func (q *lfQueue) Init(cap int) {
	q.once.Do(func() {
		if cap < 1 {
			cap = initSize
		}
		q.data = make([]eface, modUint32(uint32(cap))+1)
	})
}

// pushHead adds val at the head of the queue.
// It returns false if the queue is full.
func (q *lfQueue) Push(val any) bool {
	if q.data == nil {
		q.Init(initSize)
	}
	var slot *eface
	mod := uint32(len(q.data) - 1)
	for {
		head, tail := atomic.LoadUint32(&q.head), atomic.LoadUint32(&q.tail)
		if head == mod+1+tail {
			// Queue is full.
			return false
		}
		slot = &q.data[head&mod]

		// Check if the head slot has been released by popTail.
		typ := atomic.LoadPointer(&slot.typ)
		if typ != nil {
			// Another goroutine is still cleaning up the tail, so
			// the queue is actually still full.
			return false
		}
		// Increment head. This passes ownership of slot to popTail
		// and acts as a store barrier for writing the slot.
		runtime_procPin()
		if atomic.CompareAndSwapUint32(&q.head, head, head+1) {
			// The head slot is free, so we own it.
			if val == nil {
				val = queueNil(nil)
			}
			slot.data = (*eface)(unsafe.Pointer(&val)).data
			atomic.StorePointer(&slot.typ, (*eface)(unsafe.Pointer(&val)).typ)
			runtime_procUnpin()
			return true
		}
		runtime_procUnpin()
	}
}

// popTail removes and returns the element at the tail of the queue.
// It returns false if the queue is empty.
func (q *lfQueue) Pop() (any, bool) {
	var slot *eface
	if q.data == nil {
		return nil, false
	}
	mod := uint32(len(q.data) - 1)
	for {
		head, tail := atomic.LoadUint32(&q.head), atomic.LoadUint32(&q.tail)
		if head == tail {
			// Queue is empty.
			return nil, false
		}

		// Confirm head and tail (for our speculative check
		// above) and increment tail. If this succeeds, then
		// we own the slot at tail.
		slot = &q.data[tail&mod]
		// p := atomic.LoadPointer(&slot.p)
		typ := atomic.LoadPointer(&slot.typ)
		if typ == nil {
			// Another goroutine is store head, so
			// the queue is actually still empty.
			return nil, false
		}
		runtime_procPin()
		if atomic.CompareAndSwapUint32(&q.tail, tail, tail+1) {

			// We now own slot.
			val := *(*any)(unsafe.Pointer(slot))
			if val == queueNil(nil) {
				val = nil
			}

			// Tell pushHead that we're done with this slot. Zeroing the
			// slot is also important so we don't leave behind references
			// that could keep this object live longer than necessary.
			//
			// We write to val first and then publish that we're done with
			// this slot by atomically writing to typ.
			slot.data = nil
			atomic.StorePointer(&slot.typ, nil)
			// atomic.StorePointer(&slot.p, nil)
			// At this point pushHead owns the slot.
			runtime_procUnpin()
			return val, true
		}
		runtime_procUnpin()
	}
}

// eface queue element
type eface struct {
	typ, data unsafe.Pointer
}

// 溢出环形计算需要，得出2^n-1。(2^n>=u,具体可见kfifo）
func modUint32(u uint32) uint32 {
	u -= 1 //兼容0, as min as ,128->127 !255
	u |= u >> 1
	u |= u >> 2
	u |= u >> 4
	u |= u >> 8  // 32位类型已经足够
	u |= u >> 16 // 64位
	return u
}

//go:linkname runtime_procPin runtime.procPin
func runtime_procPin()

//go:linkname runtime_procUnpin runtime.procUnpin
func runtime_procUnpin()
