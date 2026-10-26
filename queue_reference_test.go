package queue_test

import (
	"github.com/min1324/queue"
)

// use for slice
const (
	/*
		1<< 20~28
		1048576		20
		2097152		21
		4194304		22
		8388608		23
		16777216	24
		33554432	25
		67108864	26
		134217728	27
		268435456	28
	*/
	prevCap = 1 << 22 // queue previous Push

	// stressTestCap 压力测试队列容量
	stressTestCap = 1 << 20
	// shortTestCap 短测试队列容量
	shortTestCap = 1 << 8

	// defaultTestCap 测试默认队列容量
	defaultTestCap = 1 << 8
)

// Interface use in stack,queue testing
type Interface interface {
	queue.Queue
	BatchPop(count int) ([]any, int)
	BatchPush(val ...any) int
	Clean() ([]any, int)
}
