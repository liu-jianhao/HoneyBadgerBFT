package main

import (
	"encoding/binary"
	"encoding/gob"
	"math/rand"
	"time"
)

func init() {
	rand.Seed(time.Now().UnixNano())
	gob.Register(&Transaction{})
}

type Transaction struct {
	Nonce uint64
}

func (t *Transaction) Hash() []byte {
	buf := make([]byte, 8)
	binary.LittleEndian.PutUint64(buf, t.Nonce)
	return buf
}

func MakeTransactions(n int) []*Transaction {
	txx := make([]*Transaction, n)
	for i := 0; i < n; i++ {
		txx[i] = &Transaction{rand.Uint64()}
	}
	return txx
}
