package hbbft

import (
	"sync"
)

type transactionBuffer struct {
	lock sync.Mutex
	data []Transaction
}

func newTransactionBuffer() *transactionBuffer {
	return &transactionBuffer{
		data: make([]Transaction, 0, 1024*1024),
	}
}

func (b *transactionBuffer) add(transaction Transaction) {
	b.lock.Lock()
	defer b.lock.Unlock()
	b.data = append(b.data, transaction)
}

func (b *transactionBuffer) delete(transactions []Transaction) {
	b.lock.Lock()
	defer b.lock.Unlock()

	temp := make(map[string]Transaction)
	for i := 0; i < len(b.data); i++ {
		temp[string(b.data[i].Hash())] = b.data[i]
	}
	for i := 0; i < len(transactions); i++ {
		delete(temp, string(transactions[i].Hash()))
	}
	data := make([]Transaction, len(temp))
	i := 0
	for _, tx := range temp {
		data[i] = tx
		i++
	}
	b.data = data
}

func (b *transactionBuffer) len() int {
	b.lock.Lock()
	defer b.lock.Unlock()
	return len(b.data)
}
