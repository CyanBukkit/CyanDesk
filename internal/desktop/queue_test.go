package desktop

import "testing"

func TestVideoQueueDropsOldestUnderPressure(t *testing.T) {
	q := NewVideoQueue(1)
	q.Push([]byte("a"))
	q.Push([]byte("b"))
	q.Push([]byte("c"))
	got, ok := q.Pop()
	if !ok {
		t.Fatal("empty")
	}
	if string(got) == "a" {
		t.Fatal("oldest frame was kept while newer frames were waiting")
	}
	if q.Dropped == 0 {
		t.Fatal("expected drops when the sender outruns the receiver")
	}
}
