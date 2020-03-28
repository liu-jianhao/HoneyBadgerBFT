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
		go node.addTransactionLoop()
		go func(node *Server) {
			if err := node.hb.Start(); err != nil {
				panic("hb.Start")
			}
			for _, msg := range node.hb.GetMessage() {
				messages <- message{node.id, msg}
			}
		}(node)
		go node.commitLoop()
	}

	for {
		msg := <-messages
		node := nodes[msg.payload.To]
		hbMessage, _ := msg.payload.Payload.(hbbft.HBMessage)
		if err := node.hb.HandleMessage(msg.from, hbMessage.Epoch, hbMessage.Payload.(*hbbft.ACSMessage)); err != nil {
			panic("hb.HandleMessage")
		}
		for _, msg := range node.hb.GetMessage() {
			messages <- message{node.id, msg}
		}
	}
}
