package main

import (
	"flag"
	"fmt"

	"../hbbft"
)

const (
	// 	lenNodes  = 4
	// 	batchSize = 500
	numCores = 4
)

type message struct {
	from    uint64
	payload hbbft.Message
}

var (
	lenNodes  = 4
	batchSize = 500
	messages  = make(chan message, 1024*1024)
	relayCh   = make(chan *Transaction, 1024*1024)
)

func Init() {
	flag.IntVar(&lenNodes, "lenNodes", 4, "number of nodes")
	flag.IntVar(&batchSize, "batchSize", 500, "number of transactions at a time")
}

func main() {
	Init()
	flag.Parse()

	fmt.Printf("lenNodes = %v batchSize = %v\n", lenNodes, batchSize)

	nodes := makeNetwork(lenNodes)

	for _, node := range nodes {
		go node.run()
		go func(node *Server) {
			if err := node.hb.Start(); err != nil {
				panic("hb.Start")
			}
			for _, msg := range node.hb.Messages() {
				messages <- message{node.id, msg}
			}
		}(node)
	}

	go func() {
		for transaction := range relayCh {
			for _, node := range nodes {
				node.addTransactions(transaction)
			}
		}
	}()

	for {
		msg := <-messages
		node := nodes[msg.payload.To]
		switch t := msg.payload.Payload.(type) {
		case hbbft.HBMessage:
			if err := node.hb.HandleMessage(msg.from, t.Epoch, t.Payload.(*hbbft.ACSMessage)); err != nil {
				panic("hb.HandleMessage")
			}
			for _, msg := range node.hb.Messages() {
				messages <- message{node.id, msg}
			}
		}
	}
}
