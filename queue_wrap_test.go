package queue

import "testing"

func TestQueueCounterWraparound(t *testing.T) {
	var q LockFree
	q.Init(8)

	start := ^uint64(0) - 2
	q.head = start
	q.tail = start

	for i := 0; i < q.Cap(); i++ {
		if !q.Push(i) {
			t.Fatalf("Push(%d) failed before queue was full", i)
		}
	}

	if q.head <= q.tail {
		t.Fatalf("expected head (%d) > tail (%d) after counter wraparound", q.head, q.tail)
	}
	if got := q.Len(); got != q.Cap() {
		t.Fatalf("Len() = %d after filling wrapped queue, want %d", got, q.Cap())
	}
	if q.Push(q.Cap()) {
		t.Fatal("Push succeeded on full wrapped queue")
	}

	for i := 0; i < q.Cap(); i++ {
		got, ok := q.Pop()
		if !ok {
			t.Fatalf("Pop() %d returned ok=false", i)
		}
		if got != i {
			t.Fatalf("Pop() %d = %v, want %d", i, got, i)
		}
	}
	if got := q.Len(); got != 0 {
		t.Fatalf("Len() after draining wrapped queue = %d, want 0", got)
	}
	if _, ok := q.Pop(); ok {
		t.Fatal("Pop() succeeded on empty wrapped queue")
	}
}
