package queue

import (
	"sync"
	"sync/atomic"
)

// 双锁环形队列，可以并发Push和Pop
// 自动扩容缩容
type MuxAuto struct {
	once sync.Once

	inMux  sync.Mutex // 控制Push操作
	outMux sync.Mutex // 控制Pop操作

	auto bool // 是否自动扩容,默认是 true

	cap  uint64 // 队列容量
	mask uint64 // 2^n-1 > cap

	tail atomic.Uint64 // 指向下次取出数据的位置:tail&mask
	head atomic.Uint64 // 指向下次写入数据的位置:head&mask

	// 环形缓冲区存储区。
	//
	// 容量为2^n，其中 n 是大于 cap 的最小正整数。
	//
	// tail == head+cap 表示队列已满，head == tail 表示队列为空。
	data []entry
}

// 必须在锁里面调用
func (q *MuxAuto) initInLock(cap int) {
	if cap < 1 {
		cap = DefaultCap
	}
	if uint64(cap) > MaxCap {
		panic("MuxAuto: queue capacity overflow MaxCap")
	}
	q.mask = upPow2(uint64(cap))
	q.cap = uint64(cap)
	q.head.Store(0)
	q.tail.Store(0)
	q.auto = true // 默认扩容
	q.data = make([]entry, q.mask+1)
}

func (q *MuxAuto) Init(cap int) {
	q.inMux.Lock()
	defer q.inMux.Unlock()
	if q.data == nil {
		q.initInLock(cap)
	}
}

func (q *MuxAuto) Cap() int {
	return int(q.cap)
}

// Size return current number in queue
func (q *MuxAuto) Size() int {
	tail := q.tail.Load()
	head := q.head.Load()
	return int(tail - head)
}

func (q *MuxAuto) Empty() bool {
	return q.tail.Load() == q.head.Load()
}

func (q *MuxAuto) Full() bool {
	return q.tail.Load() == q.head.Load()+q.cap
}

func (q *MuxAuto) SetOpt(auto bool) {
	q.auto = auto
}

func (q *MuxAuto) Push(val any) bool {
	if q.Full() && !q.auto && q.cap > 0 {
		return false
	}
	q.inMux.Lock()
	defer q.inMux.Unlock()
	if q.data == nil {
		q.initInLock(DefaultCap)
	}
	tail := q.tail.Load()
	head := q.head.Load()
	if tail-head >= q.cap {
		// grow
		if q.auto {
			q.growWorkInLock(1)
		}
		if tail-head >= q.cap {
			// grow failed
			return false
		}
	}
	slot := &q.data[tail&q.mask]
	slot.store(val)
	q.tail.Add(1)
	return true
}

// 扩容，必须在锁内进行，扩容后更新mask,cap值
func (s *MuxAuto) growWorkInLock(delta uint64) {
	const incGrow = 1 << 15
	curCap := s.cap
	if curCap >= MaxCap || delta < 1 || curCap+delta > MaxCap {
		return
	}
	// curCap+delta < MaxCap
	newCap := curCap
	// TODO adjust grow size
	for newCap < curCap+delta {
		if newCap > incGrow {
			newCap += newCap>>2 + delta
		} else {
			newCap <<= 1
		}
	}
	if newCap > MaxCap {
		newCap = MaxCap
	}
	newMask := upPow2(newCap)
	if newMask == s.mask {
		s.cap = newCap
		return
	}
	newData := make([]entry, newMask+1)
	head, tail := s.head.Load(), s.tail.Load()
	mask := s.mask
	// 从head开始，保持一致性
	for i := head; i < tail; i++ {
		newData[i&newMask] = s.data[i&mask]
	}
	s.data = newData
	s.mask = newMask
	s.cap = newCap
}

func (q *MuxAuto) Pop() (val any, ok bool) {
	if q.Empty() {
		return nil, false
	}
	q.outMux.Lock()
	defer q.outMux.Unlock()
	tail := q.tail.Load()
	head := q.head.Load()
	if tail == head {
		return nil, false
	}
	slot := &q.data[head&q.mask]
	val, ok = slot.load()
	slot.free()
	q.head.Add(1)
	if q.auto {
		q.shrinkWorkInLock()
	}
	return val, ok
}

func (s *MuxAuto) shrinkWorkInLock() {
	curCap := s.cap
	curLen := s.tail.Load() - s.head.Load()
	if curCap <= DefaultCap || curLen >= curCap>>3 {
		return
	}

	// 当前长度是cap的1/8时，开始缩容
	newCap := curCap >> 1
	for newCap > DefaultCap && curLen < newCap>>3 {
		newCap >>= 1
	}
	if newCap < DefaultCap {
		newCap = DefaultCap
	}
	newMask := upPow2(newCap)
	if newMask == s.mask {
		s.cap = newCap
		return
	}
	newData := make([]entry, newMask+1)
	head, tail := s.head.Load(), s.tail.Load()
	mask := s.mask
	// 从head开始，保持一致性
	for i := head; i < tail; i++ {
		newData[i&newMask] = s.data[i&mask]
	}
	s.data = newData
	s.mask = newMask
	s.cap = newCap
}

func (q *MuxAuto) Tail() (val any, ok bool) {
	if q.Empty() {
		return nil, false
	}
	q.outMux.Lock()
	defer q.outMux.Unlock()
	tail := q.tail.Load()
	head := q.head.Load()
	if tail == head {
		return nil, false
	}
	slot := &q.data[(tail-1)&q.mask]
	val, ok = slot.load()
	return val, true
}

func (q *MuxAuto) Head() (val any, ok bool) {
	if q.Empty() {
		return nil, false
	}
	q.outMux.Lock()
	defer q.outMux.Unlock()
	tail := q.tail.Load()
	head := q.head.Load()
	if tail == head {
		return nil, false
	}
	slot := &q.data[head&q.mask]
	val, ok = slot.load()
	return val, true
}

func (q *MuxAuto) BatchPush(val ...any) (int, error) {
	if len(val) < 1 || (q.Full() && !q.auto && q.cap > 0) {
		return 0, nil
	}
	q.inMux.Lock()
	defer q.inMux.Unlock()
	if q.data == nil {
		q.initInLock(DefaultCap)
	}

	tail := q.tail.Load()
	head := q.head.Load()

	availbale := int(q.cap - (tail - head))
	if availbale < len(val) {
		// grow
		if q.auto {
			q.growWorkInLock(uint64(len(val)))
			availbale = int(q.cap - (tail - head))
		}
		if availbale < len(val) {
			return 0, ErrMuxRingFull
		}
	}
	availbale = len(val)
	mask := q.mask
	for i := 0; i < availbale; i++ {
		slot := &q.data[(tail+uint64(i))&mask]
		slot.store(val[i])
	}
	q.tail.Add(uint64(availbale))
	return availbale, nil
}

func (q *MuxAuto) BatchPop(count int) []any {
	if count < 1 || q.Empty() {
		return nil
	}
	q.outMux.Lock()
	defer q.outMux.Unlock()
	tail := q.tail.Load()
	head := q.head.Load()
	availbale := int(tail - head)
	if availbale < 1 {
		return nil
	}
	if availbale > count {
		availbale = count
	}
	actual := 0
	result := make([]any, availbale)
	mask := q.mask
	for i := 0; i < availbale; i++ {
		slot := &q.data[(head+uint64(i))&mask]
		v, ok := slot.load()
		if !ok {
			// slot is nil, maybe other goroutine is pop it.
			// or pusher is push it.
			break
		}
		slot.free()
		result[i] = v
		actual++
	}
	q.head.Add(uint64(actual))
	result = result[:actual]

	if q.auto {
		q.shrinkWorkInLock()
	}
	return result
}

func (q *MuxAuto) All() []any {
	if q.Empty() {
		return nil
	}
	q.outMux.Lock()
	defer q.outMux.Unlock()
	tail := q.tail.Load()
	head := q.head.Load()
	mask := q.mask
	if tail == head {
		return nil
	}

	var result = make([]any, q.cap)
	actual := 0
	for i := head; i < tail; i++ {
		slot := &q.data[i&mask]
		v, ok := slot.load()
		if !ok {
			continue
		}
		result[i-head] = v
		actual++
	}
	result = result[:actual]
	return result
}

func (q *MuxAuto) Clean() {
	if q.Empty() {
		return
	}
	q.inMux.Lock()
	defer q.inMux.Unlock()
	q.outMux.Lock()
	defer q.outMux.Unlock()
	tail := q.tail.Load()
	// tail == head+cap 表示队列已满，head == tail 表示队列为空。
	q.head.Store(tail)
	q.data = make([]entry, q.mask+1)
}
