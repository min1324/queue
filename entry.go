package queue

import (
	"sync/atomic"
	"unsafe"
)

type entryNil struct{}

var entryNilValue = (*entryNil)(nil)

// ===========================================================================

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

// =========================================================================

// type entry struct {
// 	flag atomic.Bool // 0: nil, 1: valid
// 	atomic.Value
// }

// func (n *entry) load() (val any, ok bool) {
// 	if !n.flag.Load() {
// 		return nil, false
// 	}
// 	val = n.Value.Load()
// 	if val == entryNilValue {
// 		val = nil
// 	}
// 	return val, true
// }
// func (n *entry) store(val any) {
// 	if val == nil {
// 		val = entryNilValue
// 	}
// 	n.Value.Store(val)
// 	n.flag.Store(true)
// }
// func (n *entry) free() {
// 	n.flag.Store(false)
// }

// ===========================================================

// // entry 映射了空接口的运行时内部表示。
// type entry struct {
// 	typ, data unsafe.Pointer
// }

// func (n *entry) load() (val any, ok bool) {
// 	typ := atomic.LoadPointer(&n.typ)
// 	data := atomic.LoadPointer(&n.data)
// 	if typ == nil {
// 		return nil, false
// 	}
// 	ef := entry{typ: typ, data: data}
// 	return *(*any)(unsafe.Pointer(&ef)), true
// }

// func (n *entry) store(val any) {
// 	ef := (*entry)(unsafe.Pointer(&val))
// 	atomic.StorePointer(&n.typ, ef.typ)
// 	atomic.StorePointer(&n.data, ef.data)
// }

// func (n *entry) free() {
// 	atomic.StorePointer(&n.typ, nil)
// 	atomic.StorePointer(&n.data, nil)
// }

// func (n *entry) isNil() bool {
// 	return atomic.LoadPointer(&n.typ) == nil
// }
