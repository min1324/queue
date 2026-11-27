package queue

const (
	// DefaultCap 是未提供容量时的默认队列容量。
	// 选择2的幂次是为了高效的取模运算。
	DefaultCap = 1 << 8 // 256

	// MaxCap 是队列的最大容量。
	MaxCap = 1<<32 - 1 // ^uint64(0) >> 2

	cacheLineSize = 64
)

// kfifo 返回 2^n-1，其中 2^n >= u，用于环形缓冲区的取模计算。
// 此位操作技术将值向上取整到下一个2的幂次减1，
// 使得可以通过按位与运算代替除法来实现高效的取模操作。
// kfifo(128) =127, kfifo(129) = 255。
// 如果 u 为 0 则返回 0，以避免下溢。
// 另见：Linux 内核 kfifo 实现。
func kfifo(u uint64) uint64 {
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
