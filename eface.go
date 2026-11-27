package queue

import "unsafe"

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

var (
	efaceNilVal any = (*efaceNil)(nil)
	efaceNilTyp     = (*eface)(unsafe.Pointer(&efaceNilVal)).typ
)

type efaceNil struct{}

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
