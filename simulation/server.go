package main

import (
	"fmt"
	"sync"
	"time"

	"../hbbft"
)

type Server struct {
	id             uint64
	hb             *hbbft.HoneyBadger
	transport      *hbbft.Transport
	lock           sync.RWMutex
	transactionMap map[string]*Transaction
	totalCommit    int
	start          time.Time
}

func newServer(id uint64, tr *hbbft.Transport, nodes []uint64) *Server {
	hb := hbbft.NewHoneyBadger(hbbft.Config{
		N:         len(nodes),
		ID:        id,
		Nodes:     nodes,
		BatchSize: batchSize,
	})
	return &Server{
		id:             id,
		transport:      tr,
		hb:             hb,
		transactionMap: make(map[string]*Transaction),
		start:          time.Now(),
	}
}

func (s *Server) addTransactions(txx ...*Transaction) {
	for _, tx := range txx {
		s.lock.Lock()
		s.transactionMap[string(tx.Hash())] = tx
		s.lock.Unlock()

		s.hb.AddTransaction(tx)
		go func() {
			for i := 0; i < len(s.hb.Nodes); i++ {
				if uint64(i) != s.hb.ID {
					relayCh <- tx
				}
			}
		}()
	}
}

func (s *Server) addTransactionLoop() {
	timer := time.NewTicker(1 * time.Second)
	for {
		<-timer.C
		s.addTransactions(MakeTransactions(1000)...)
	}
}

func (s *Server) commitLoop() {
	timer := time.NewTicker(time.Second * 2)
	n := 0
	for {
		select {
		case <-timer.C:
			out := s.hb.Outputs()
			// var epoch uint64
			for _, txx := range out {
				for _, tx := range txx {
					hash := tx.Hash()
					s.lock.Lock()
					n++
					delete(s.transactionMap, string(hash))
					// epoch = e
					s.lock.Unlock()
				}
			}
			s.totalCommit += n
			delta := time.Since(s.start)
			if s.id == 1 {
				fmt.Println("")
				fmt.Println("*************************************************")
				fmt.Printf("server %d\n", s.id)
				fmt.Printf("commited %d transactions over %v\n", s.totalCommit, delta)
				fmt.Printf("throughput %d TX/s\n", s.totalCommit/int(delta.Seconds()))
				// fmt.Printf("epoch %d\n", epoch)
				fmt.Println("*************************************************")
				fmt.Println("")
			}
			n = 0
		}
	}
}

// 1. 不断产生事务 2. 不断提交事务
func (s *Server) run() {
	go s.addTransactionLoop()
	go s.commitLoop()
}

func makeNetwork(n int) []*Server {
	transports := make([]*hbbft.Transport, n)
	nodes := make([]*Server, n)
	for i := 0; i < n; i++ {
		transports[i] = hbbft.NewTransport(uint64(i))
		ids := make([]uint64, n)
		for i := 0; i < n; i++ {
			ids[i] = uint64(i)
		}
		nodes[i] = newServer(uint64(i), transports[i], ids)
	}
	for i := 0; i < len(transports); i++ {
		for ii := 0; ii < len(transports); ii++ {
			if ii == i {
				continue
			}
			transports[i].Connect(transports[ii].Addr(), transports[ii])
		}
	}
	return nodes
}
