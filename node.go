package queue

// interface node
type baseNode struct {
	p interface{}
}

func newBaseNode(i interface{}) *baseNode {
	return &baseNode{p: i}
}

func (n *baseNode) load() interface{} {
	return n.p
}

func (n *baseNode) store(i interface{}) {
	n.p = i
}

func (n *baseNode) free() {
	n.p = nil
}
