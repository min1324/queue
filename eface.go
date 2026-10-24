package queue

import (
	"sync/atomic"
	"unsafe"
)

func init() {
	verifyEfaceLayout()
}

const (
	cacheLineSize = 64
)

type nilQueueNil struct{}

var queueNilVal any = (*nilQueueNil)(nil)

var queueNilTyp = (*eface)(unsafe.Pointer(&queueNilVal)).typ

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

type paddedEface struct {
	eface
	_ [cacheLineSize - unsafe.Sizeof(eface{})]byte
}

// eface 映射了空接口的运行时内部表示。
type eface struct {
	typ, data unsafe.Pointer
}

func (n *eface) load() (val any, ok bool) {
	typ := atomic.LoadPointer(&n.typ)
	if typ == nil {
		return nil, false
	}
	data := atomic.LoadPointer(&n.data)
	if typ == queueNilTyp {
		return nil, true
	}
	ef := eface{typ: typ, data: data}
	return *(*any)(unsafe.Pointer(&ef)), true
}

func (n *eface) store(val any) {
	if val == nil {
		val = queueNilVal
	}
	ef := (*eface)(unsafe.Pointer(&val))
	// 先写入 data，后写入 typ
	atomic.StorePointer(&n.data, ef.data)
	atomic.StorePointer(&n.typ, ef.typ)
}

func (n *eface) free() {
	// 先清理data，后清理typ
	atomic.StorePointer(&n.data, nil)
	atomic.StorePointer(&n.typ, nil)
}

func (n *eface) isNil() bool {
	return atomic.LoadPointer(&n.typ) == nil
}

// type paddedUint64 struct {
// 	atomic.Uint64
// 	_ [cacheLineSize - unsafe.Sizeof(atomic.Uint64{})]byte
// }
