package main

import (
	"flag"
	"fmt"

	"../hbbft"
)

type message struct {
	from    uint64
	payload hbbft.Message
}

var (
	lenNodes  = 4
	batchSize = 100
	messages  = make(chan message, 1024*1024)
)

func Init() {
	flag.IntVar(&lenNodes, "lenNodes", lenNodes, "number of nodes")
	flag.IntVar(&batchSize, "batchSize", batchSize, "number of transactions at a time")
}

func main() {
	Init()
	flag.Parse()

	nodes := makeNetwork(lenNodes)

	fmt.Printf("lenNodes = %v, faultyNodes = %v, batchSize = %v\n", lenNodes, lenNodes/4, batchSize)

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
