package queue_test

import (
	"github.com/min1324/queue"
)

// use for slice
const (
	bit  = 3
	mod  = 1<<bit - 1
	null = ^uintptr(0) // -1

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
	prevSize = 1 << 23 // queue previous Push
)

// Interface use in stack,queue testing
type Interface interface {
	queue.Queue
}
