package queue

import (
	"sync"
	"sync/atomic"
	"unsafe"
)

type DRQueue struct {
	once sync.Once
	deMu sync.Mutex
	enMu sync.Mutex

	len  uint64 // queue's len
	cap  uint64 // queue's cap
	tail uint64 // 指向下次取出数据的位置:tail&mod
	head uint64 // 指向下次写入数据的位置:head&mod

	// 环形队列，大小必须是2的倍数。
	// val为空，表示可以Push,如果是Pop操作，表示队列空。
	// val不为空，表所可以Pop,如果是Push操作，表示队列满了。
	// 只能由Push将val从nil变成非nil,
	// 只能由Pop将val从非nil变成nil.
	data []ringEntry
}

func (q *DRQueue) Init(cap int) {
	q.once.Do(func() {
		if cap < 1 {
			cap = 1 << 8
		}
		q.data = make([]ringEntry, cap)
		atomic.StoreUint64(&q.cap, uint64(cap))
	})
}

// Cap return queue's cap
func (q *DRQueue) Cap() int {
	return int(atomic.LoadUint64(&q.cap))
}

// Size return current number in queue
func (q *DRQueue) Len() int {
	return int(atomic.LoadUint64(&q.len))
}

func (q *DRQueue) IsEmpty() bool {
	return q.Len() == 0
}

func (q *DRQueue) IsFull() bool {
	return q.Len() == q.Cap()
}

func (q *DRQueue) mod() uint64 {
	return uint64(q.Cap())
}

func (q *DRQueue) Push(val any) bool {
	if q.data == nil {
		q.Init(initSize)
	}
	if q.IsFull() {
		return false
	}
	q.enMu.Lock()
	defer q.enMu.Unlock()
	if q.IsFull() {
		return false
	}
	q.data[q.tail].store(val)
	q.tail = (q.tail + 1) % q.mod()
	atomic.AddUint64(&q.len, 1)

	return true
}

func (q *DRQueue) Pop() (val any, ok bool) {
	if q.IsEmpty() {
		return
	}
	q.deMu.Lock()
	defer q.deMu.Unlock()
	if q.IsEmpty() {
		return
	}
	e := &q.data[q.head]
	val, ok = e.load()
	e.free()
	if !ok {
		panic("DRQueue Pop nil val")
	}
	q.head = (q.head + 1) % q.mod()
	atomic.AddUint64(&q.len, ^uint64(0))
	return
}

type nilentrynil struct{}

var empty any = (*nilentrynil)(nil)

// ringEntry queue element
type ringEntry struct {
	p unsafe.Pointer
}

func (n *ringEntry) load() (interface{}, bool) {
	p := atomic.LoadPointer(&n.p)
	if p == nil {
		return nil, false
	}
	val := *(*interface{})(p)
	if val == empty {
		val = nil
	}
	return val, true
}

func (n *ringEntry) store(i interface{}) {
	// Copy i to a heap-allocated variable to ensure the pointer
	// remains valid after store() returns. Without this, &i points
	// to a stack frame that may be reused.
	heapI := i
	if heapI == nil {
		heapI = empty
	}
	atomic.StorePointer(&n.p, unsafe.Pointer(&heapI))
}

func (n *ringEntry) free() {
	atomic.StorePointer(&n.p, nil)
}

// func (q *DRQueue) Resize(newCap int) bool {

// 	// Acquire both locks to ensure no concurrent Push/Pop
// 	q.enMu.Lock()
// 	defer q.enMu.Unlock()
// 	q.deMu.Lock()
// 	defer q.deMu.Unlock()

// 	if q.data == nil {
// 		q.Init(newCap)
// 		return true
// 	}
// 	if newCap < 1 {
// 		newCap = initSize
// 	}
// 	newMod := modUint64(uint64(newCap))
// 	newCap = int(newMod + 1)

// 	clen := q.Len()
// 	oldHead := atomic.LoadUint64(&q.head)
// 	oldMod := uint64(len(q.data) - 1)
// 	if newCap < clen {
// 		return false
// 	}
// 	newData := make([]entry, newMod+1)

// 	j := 0
// 	for i := oldHead; j < clen; j++ {
// 		newData[j] = q.data[i&oldMod]
// 		i++
// 	}
// 	q.data = newData
// 	atomic.StoreUint64(&q.tail, uint64(j))
// 	atomic.StoreUint64(&q.head, 0)

// 	return true
// }
