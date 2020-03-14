package hbbft

import (
	"sync"
)

type Transport struct {
	lock sync.RWMutex
	// addr -> local_transport
	peers map[uint64]*Transport
	addr  uint64
}

func NewTransport(addr uint64) *Transport {
	return &Transport{
		peers: make(map[uint64]*Transport),
		addr:  addr,
	}
}

// 加入到peers里
func (t *Transport) Connect(addr uint64, trans *Transport) {
	t.lock.Lock()
	defer t.lock.Unlock()
	t.peers[addr] = trans
}

func (t *Transport) Addr() uint64 {
	return t.addr
}
