package queue

import (
	"sync/atomic"
	"unsafe"
)

// interface node
type baseNode struct {
	p unsafe.Pointer
}

func (n *baseNode) load() interface{} {
	p := atomic.LoadPointer(&n.p)
	if p == nil {
		return nil
	}
	return (interface{})(p)
}

func (n *baseNode) store(i interface{}) {
	atomic.StorePointer(&n.p, unsafe.Pointer(&i))
}

func (n *baseNode) free() {
	atomic.StorePointer(&n.p, nil)
}
