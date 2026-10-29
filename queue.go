package queue

const (
	// DefaultCap 是未提供容量时的默认队列容量。
	// 选择2的幂次是为了高效的取模运算。
	DefaultCap = 1 << 8 // 256

	// MaxCap 是队列的最大容量。
	MaxCap = 1<<30 - 1 // ^uint64(0) >> 2
)

type QueueOpt interface {
	Queue

	BatchPush(val ...any) (int, error)
	BatchPop(count int) []any
	Head() (value any, ok bool)
	Tail() (value any, ok bool)
	Full() bool
	Empty() bool
	All() []any
	Clean()
}

// Queue 定义了带有容量管理的无锁队列接口。
// 它扩展了基本的 Interface 接口，增加了查询和管理队列容量与长度的方法。
type Queue interface {
	// Push 将值添加到队列的头部（写入位置）。
	// 成功添加返回 true，队列已满返回 false。
	// 此操作是无锁的，可安全并发使用。
	Push(value any) bool

	// Pop 从队列的尾部（读取位置）移除并返回元素。
	// 成功返回值和 true，队列为空返回 nil 和 false。
	// 此操作是无锁的，可安全并发使用。
	Pop() (value any, ok bool)

	// Cap 返回队列的容量（最多可容纳的元素数量）。
	// 如果队列未初始化，返回 0。
	Cap() int

	// Len 返回队列中当前的元素数量。
	// 注意：在并发访问下，此值可能是近似值。
	// 负数结果会被钳制为 0。
	Size() int

	// Init 使用指定容量初始化队列。
	// 此方法只能调用一次，后续调用无效。
	// 如果 cap < 1，将使用默认容量（256）。
	// 如果未显式调用，将在首次 Push 操作时自动调用。
	Init(cap int)
}

// upPow2 返回 2^n-1，其中 2^n >= u，用于环形缓冲区的取模计算。
// 此位操作技术将值向上取整到下一个2的幂次减1，
// 使得可以通过按位与运算代替除法来实现高效的取模操作。
// upPow2(128) =127, upPow2(129) = 255。
// 如果 u 为 0 则返回 0，以避免下溢。
// 另见：Linux 内核 kfifo 实现。
func upPow2(u uint64) uint64 {
	if u == 0 {
		return 0
	}
	u -= 1 // 调整0值：确保最小结果
	u |= u >> 1
	u |= u >> 2
	u |= u >> 4
	u |= u >> 8
	u |= u >> 16
	u |= u >> 32 // 处理64位类型的高32位
	return u
}
