package queue

import (
	"sync"
	"sync/atomic"
)

// 优先级队列
// 优先级从0开始，0为最高优先级，7为最低优先级，超过7的优先级被置为7
type Priority uint32

const (
	P0 Priority = iota // 最高优先级
	P1
	P2
	P3
	P4
	P5
	P6
	P7            // 最低优先级
	NumP = 1 << 3 // 优先级数量
)

type pNode struct {
	MuxAuto
}

type PriorityQueue struct {
	mu     sync.Mutex
	maxCap uint64 // 为0表示未初始化
	num    uint64
	data   []pNode
}

func (q *PriorityQueue) init(maxP int) {
	if maxP < 1 || maxP > NumP {
		maxP = NumP
	}
	q.data = make([]pNode, maxP)
	q.maxCap = uint64(maxP)
}
func (q *PriorityQueue) Init(maxP int) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.data != nil {
		return
	}
	q.init(maxP)
}

func (q *PriorityQueue) getNode(p Priority) *pNode {
	if p > P7 {
		p = P7
	}
	return &q.data[p]
}

func (q *PriorityQueue) Push(p Priority, val any) bool {
	if q.maxCap == 0 {
		q.Init(int(p))
	}
	ok := q.getNode(p).Push(val)
	if ok {
		atomic.AddUint64(&q.num, 1)
	}
	return ok
}

func (q *PriorityQueue) BatchPush(p Priority, val ...any) (int, error) {
	if q.maxCap == 0 {
		q.Init(int(p))
	}
	n, err := q.getNode(p).BatchPush(val...)
	if n > 0 {
		atomic.AddUint64(&q.num, uint64(n))
	}
	return n, err
}

func (q *PriorityQueue) Pop(p Priority) (any, bool) {
	if q.Empty() {
		return nil, false
	}
	val, ok := q.getNode(p).Pop()
	if ok {
		atomic.AddUint64(&q.num, ^uint64(0))
		return val, true
	}
	return nil, false
}

func (q *PriorityQueue) BatchPop(p Priority, count int) []any {
	if q.Empty() {
		return nil
	}
	vals := q.getNode(p).BatchPop(count)
	if len(vals) > 0 {
		atomic.AddUint64(&q.num, ^uint64(len(vals)))
	}
	return vals
}

func (q *PriorityQueue) PopHead() (any, bool) {
	if q.Empty() {
		return nil, false
	}
	for i := 0; i < int(q.maxCap); i++ {
		val, ok := q.data[i].Pop()
		if ok {
			atomic.AddUint64(&q.num, ^uint64(0))
			return val, true
		}
	}
	return nil, false
}

func (q *PriorityQueue) Head() (any, bool) {
	if q.Empty() {
		return nil, false
	}
	for i := 0; i < int(q.maxCap); i++ {
		val, ok := q.data[i].Head()
		if ok {
			return val, true
		}
	}
	return nil, false
}

func (q *PriorityQueue) Tail() (any, bool) {
	if q.Empty() {
		return nil, false
	}
	for i := int(q.maxCap) - 1; i > 0; i-- {
		val, ok := q.data[i].Tail()
		if ok {
			return val, true
		}
	}
	return nil, false
}

func (q *PriorityQueue) Take(p Priority) []any {
	if q.Empty() {
		return nil
	}
	que := q.getNode(p)
	if que.Empty() {
		return nil
	}
	val := que.BatchPop(que.Size())
	atomic.AddUint64(&q.num, ^uint64(len(val)))
	return val
}

func (q *PriorityQueue) TakeAll() []any {
	if q.Empty() {
		return nil
	}
	var result []any
	for i := 0; i < int(q.maxCap); i++ {
		que := &q.data[i]
		result = append(result, que.BatchPop(que.Cap())...)
	}
	atomic.AddUint64(&q.num, ^uint64(len(result)))
	return result
}

func (q *PriorityQueue) Slice(p Priority) []any {
	if q.Empty() {
		return nil
	}
	return q.getNode(p).All()
}

func (q *PriorityQueue) All() []any {
	if q.Empty() {
		return nil
	}
	var result []any
	for i := 0; i < int(q.maxCap); i++ {
		result = append(result, q.data[i].All()...)
	}
	return result
}

func (q *PriorityQueue) Size() int {
	return int(atomic.LoadUint64(&q.num))
}

func (q *PriorityQueue) Empty() bool {
	return q.Size() == 0
}
