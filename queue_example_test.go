package queue_test

import (
	"fmt"
	"sync"

	"github.com/min1324/queue"
)

func ExampleInit() {
	var q queue.Queue
	q.OnceInit(20)
	fmt.Printf("size:%d,cap:%d\n", q.Size(), q.Cap())
	// Output: size:0,cap:32
}

func ExampleQueue_enqueue() {
	var q queue.Queue
	q.EnQueue(1)

	fmt.Printf("size:%d\n", q.Size())
	// Output: size:1
}

func ExampleQueue_dequeue() {
	var q queue.Queue
	var m sync.Map
	var a = 20
	q.EnQueue(a)
	v, ok := q.DeQueue()
	m.Store(v, v)

	if a != v {
		fmt.Printf("%v!=%v", a, v)
	}
	n, ok := m.LoadAndDelete(a)
	if !ok {
		fmt.Printf("delete %v !ok", a)
	}
	fmt.Printf("%v=%v=%v", n, v, a)
	// Output:20=20=20
}
