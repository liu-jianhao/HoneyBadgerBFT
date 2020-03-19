package hbbft

import (
	"bytes"
	"encoding/gob"
	"math"
	"math/rand"
	"sync"
	"time"
)

type HoneyBadger struct {
	Config
	// epoch -> ACS
	acsInstances map[uint64]*ACS
	// 存事务的buffer
	transactionBuffer *transactionBuffer
	// 当前epoch
	epoch uint64

	lock sync.Mutex
	// epoch -> []Transaction，在epoch中提交的交易
	outputs map[uint64][]Transaction
	// 需要在处理之后广播的消息
	messageList *messageList
}

func NewHoneyBadger(cfg Config) *HoneyBadger {
	return &HoneyBadger{
		Config:            cfg,
		acsInstances:      make(map[uint64]*ACS),
		transactionBuffer: newTransactionBuffer(),
		outputs:           make(map[uint64][]Transaction),
		messageList:       newMessageList(),
	}
}

func (hb *HoneyBadger) Messages() []Message {
	return hb.messageList.getMessages()
}

// 加到buffer中
func (hb *HoneyBadger) AddTransaction(tx Transaction) {
	hb.transactionBuffer.add(tx)
}

// 处理给定`epoch`下的`ACSMessage`
func (hb *HoneyBadger) HandleMessage(sid, epoch uint64, msg *ACSMessage) error {
	acs, ok := hb.acsInstances[epoch]
	if !ok {
		if epoch < hb.epoch {
			return nil
		}
		acs = NewACS(hb.Config)
		hb.acsInstances[epoch] = acs
	}
	if err := acs.HandleMessage(sid, msg); err != nil {
		return err
	}
	hb.addMessages(acs.messageList.getMessages())

	if hb.epoch == epoch {
		acs, ok := hb.acsInstances[hb.epoch]
		if !ok {
			return nil
		}

		outputs := acs.Output()
		if outputs == nil || len(outputs) == 0 {
			return nil
		}

		transactionMap := make(map[string]Transaction)
		for _, output := range outputs {
			transactions := make([]Transaction, 0)
			if err := gob.NewDecoder(bytes.NewReader(output)).Decode(&transactions); err != nil {
				return err
			}
			for _, tx := range transactions {
				transactionMap[string(tx.Hash())] = tx
			}
		}

		transactions := make([]Transaction, 0)
		for _, tx := range transactionMap {
			transactions = append(transactions, tx)
		}

		hb.transactionBuffer.delete(transactions)
		hb.outputs[hb.epoch] = transactions
		hb.epoch++

		return hb.propose()
	}

	// 移除没用的acs
	for i, acs := range hb.acsInstances {
		if i >= hb.epoch-1 {
			continue
		}
		for _, t := range acs.bbaInstances {
			close(t.closeCh)
		}
		for _, t := range acs.rbcInstances {
			close(t.closeCh)
		}
		close(acs.closeCh)
		delete(hb.acsInstances, i)
	}

	return nil
}

// 调用propose
func (hb *HoneyBadger) Start() error {
	return hb.propose()
}

// 返回每次`epoch`已经提交的交易
func (hb *HoneyBadger) Outputs() map[uint64][]Transaction {
	hb.lock.Lock()
	defer hb.lock.Unlock()

	out := hb.outputs
	hb.outputs = make(map[uint64][]Transaction)
	return out
}

// 在当前`epoch`提出一批
func (hb *HoneyBadger) propose() error {
	if hb.transactionBuffer.len() == 0 {
		time.Sleep(2 * time.Second)
	}
	batchSize := hb.BatchSize
	batchSize = int(math.Min(float64(batchSize), float64(hb.transactionBuffer.len())))
	n := int(math.Max(float64(1), float64(batchSize/len(hb.Nodes))))

	rand.Seed(time.Now().UnixNano())
	batch := make([]Transaction, 0)
	for i := 0; i < n; i++ {
		batch = append(batch, hb.transactionBuffer.data[:batchSize][rand.Intn(batchSize)])
	}

	buf := new(bytes.Buffer)
	if err := gob.NewEncoder(buf).Encode(batch); err != nil {
		return err
	}

	acs, ok := hb.acsInstances[hb.epoch]
	if !ok {
		acs = NewACS(hb.Config)
	}
	hb.acsInstances[hb.epoch] = acs

	if err := acs.InputValue(buf.Bytes()); err != nil {
		return err
	}
	hb.addMessages(acs.messageList.getMessages())
	return nil
}

func (hb *HoneyBadger) addMessages(msgs []Message) {
	for _, msg := range msgs {
		hb.messageList.addMessage(HBMessage{hb.epoch, msg.Payload}, msg.To)
	}
}
