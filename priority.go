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
	P7              // 最低优先级
	NumP = 1<<3 - 1 // 优先级数量
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

func (q *PriorityQueue) init(maxP07 int) {
	if maxP07 < 1 || maxP07 > NumP {
		maxP07 = NumP
	}
	q.data = make([]pNode, NumP+1)
	q.maxCap = uint64(maxP07)
}
func (q *PriorityQueue) Init(maxP07 int) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.data != nil {
		return
	}
	q.init(maxP07)
}

func (q *PriorityQueue) getNode(p07 Priority) *pNode {
	if p07 > P7 {
		p07 = P7
	}
	return &q.data[p07]
}

func (q *PriorityQueue) Push(p07 Priority, val any) bool {
	if q.maxCap == 0 {
		q.Init(int(p07))
	}
	ok := q.getNode(p07).Push(val)
	if ok {
		atomic.AddUint64(&q.num, 1)
	}
	return ok
}

func (q *PriorityQueue) Pop() (any, bool) {
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

func (q *PriorityQueue) Take(p07 Priority) []any {
	if q.Empty() {
		return nil
	}
	que := q.getNode(07)
	if que.Empty() {
		return nil
	}
	val := que.BatchPop(que.Size())
	atomic.AddUint64(&q.num, ^uint64(uint64(len(val))))
	return val
}

func (q *PriorityQueue) Slice(p07 Priority) []any {
	if q.Empty() {
		return nil
	}
	return q.getNode(p07).All()
}

func (q *PriorityQueue) Tail() (any, bool) {
	if q.Empty() {
		return nil, false
	}
	for i := q.maxCap; i > 0; i++ {
		val, ok := q.data[i-1].Tail()
		if ok {
			return val, true
		}
	}
	return nil, false
}

func (q *PriorityQueue) Size() int {
	return int(atomic.LoadUint64(&q.num))
}

func (q *PriorityQueue) Empty() bool {
	return q.Size() == 0
}
