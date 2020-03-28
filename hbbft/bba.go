package hbbft

import (
	"fmt"
	"sync"
)

type BBA struct {
	Config
	// 当前epoch
	epoch uint64
	// 我们在这次epoch接受的BinaryValue
	binaryValues []bool
	// 这个实例发送的二进制值
	sentBinaryValue []bool
	// sender -> 接收到的BinaryValue
	recvBinaryValue map[uint64]bool
	// sender -> 接收到的aux value
	recvAux map[uint64]bool
	// 是否终止
	done bool
	// bool or nil
	output, estimated, decision interface{}
	// 一些在之后的epoch的消息
	delayedMessages []delayedMessage

	// 锁
	lock sync.RWMutex
	// 需要被广播的消息
	messages []*AgreementMessage

	// 内部使用的通道
	closeCh   chan struct{}
	inputCh   chan bbaInput
	messageCh chan bbaMessage
}

func NewBBA(cfg Config) *BBA {
	bba := &BBA{
		Config:          cfg,
		recvBinaryValue: make(map[uint64]bool),
		recvAux:         make(map[uint64]bool),
		sentBinaryValue: make([]bool, 0),
		binaryValues:    make([]bool, 0),
		closeCh:         make(chan struct{}),
		inputCh:         make(chan bbaInput),
		messageCh:       make(chan bbaMessage),
		messages:        make([]*AgreementMessage, 0),
		delayedMessages: make([]delayedMessage, 0),
	}

	go bba.run()

	return bba
}

// input
type BinaryValueRequest struct {
	Value bool
}

// output
type AuxRequest struct {
	Value bool
}

type bbaMessage struct {
	senderID uint64
	msg      *AgreementMessage
	err      chan error
}

type bbaInput struct {
	value bool
	err   chan error
}

type delayedMessage struct {
	sid uint64
	msg *AgreementMessage
}

// 输入，输出结果
func (b *BBA) InputValue(val bool) error {
	t := bbaInput{
		value: val,
		err:   make(chan error),
	}
	b.inputCh <- t
	return <-t.err
}

// 输入，输出错误
func (b *BBA) HandleMessage(senderID uint64, msg *AgreementMessage) error {
	t := bbaMessage{
		senderID: senderID,
		msg:      msg,
		err:      make(chan error),
	}
	b.messageCh <- t
	return <-t.err
}

// 返回是否已经接受值
func (b *BBA) NotProvidedInput() bool {
	return b.estimated == nil
}

// 返回output，并清空
func (b *BBA) Output() interface{} {
	out := b.output
	b.output = nil
	return out
}

// 返回消息队列中的消息
func (b *BBA) Messages() []*AgreementMessage {
	b.lock.Lock()
	defer b.lock.Unlock()

	msg := b.messages
	b.messages = make([]*AgreementMessage, 0)
	return msg
}

func (b *BBA) addMessage(msg *AgreementMessage) {
	b.lock.Lock()
	defer b.lock.Unlock()

	b.messages = append(b.messages, msg)
}

// 接受通道中的请求并处理
func (b *BBA) run() {
	for {
		select {
		case t := <-b.inputCh:
			t.err <- b.inputValue(t.value)
		case t := <-b.messageCh:
			t.err <- b.handleMessage(t.senderID, t.msg)
		case <-b.closeCh:
			return
		}
	}
}

// 添加进message、调用handleBinaryValueRequest
func (b *BBA) inputValue(val bool) error {
	if b.epoch != 0 || b.estimated != nil {
		return nil
	}
	// 接收到b(input)后，设置estimated
	b.estimated = val
	b.sentBinaryValue = append(b.sentBinaryValue, val)
	// 广播bval
	b.addMessage(&AgreementMessage{b.epoch, &BinaryValueRequest{val}})
	return b.handleBinaryValueRequest(b.ID, val)
}

// 拒绝老epoch、新epoch加进delayedMessage、根据消息类型分别处理
func (b *BBA) handleMessage(senderID uint64, msg *AgreementMessage) error {
	if b.done || msg.Epoch < b.epoch {
		return nil
	}

	if msg.Epoch > b.epoch {
		b.delayedMessages = append(b.delayedMessages, delayedMessage{senderID, msg})
		return nil
	}

	switch t := msg.Message.(type) {
	case *BinaryValueRequest:
		return b.handleBinaryValueRequest(senderID, t.Value)
	case *AuxRequest:
		return b.handleAuxRequest(senderID, t.Value)
	default:
		return fmt.Errorf("unknown BBA message received: %v", t)
	}
}

// 当接收到2f+1个节点的输入时，且binaryValues为0则广播AUX{val}，调用handleAuxRequest
func (b *BBA) handleBinaryValueRequest(senderID uint64, val bool) error {
	b.lock.Lock()
	b.recvBinaryValue[senderID] = val
	b.lock.Unlock()
	length := b.countBinaryValue(val)

	// 接收到从2f+1个节点发出的bval(b)，bval(r) = bval(r) U {b}
	if length == 2*b.F+1 {
		isEmpty := len(b.binaryValues) == 0
		b.binaryValues = append(b.binaryValues, val)
		if isEmpty {
			// 广播aux(r)
			b.addMessage(&AgreementMessage{b.epoch, &AuxRequest{val}})
			_ = b.handleAuxRequest(b.ID, val)
		}
		return nil
	}

	// 接收到从f+1个节点发出的bval(b)，如果此节点没有广播过bval(b)，那么广播bval(b)
	if length == b.F+1 && !b.hasSentBinaryValue(val) {
		b.sentBinaryValue = append(b.sentBinaryValue, val)
		b.addMessage(&AgreementMessage{b.epoch, &BinaryValueRequest{val}})
		return b.handleBinaryValueRequest(b.ID, val)
	}
	return nil
}

// 记录recvAux，调用outputAgreement
func (b *BBA) handleAuxRequest(senderID uint64, val bool) error {
	b.lock.Lock()
	b.recvAux[senderID] = val
	b.lock.Unlock()

	b.outputAgreement()
	return nil
}

// 等接收到N-F个消息，epoch+1，广播agreement值，处理delayedMessages
func (b *BBA) outputAgreement() {
	if len(b.binaryValues) == 0 {
		return
	}

	// 直到接收到N-f个不同节点发来的aux(r)
	lenOutputs, values := b.countOutputs()
	if lenOutputs < b.N-b.F {
		return
	}

	coin := b.epoch%2 == 0

	if b.done || b.decision != nil && b.decision.(bool) == coin {
		b.done = true
		return
	}

	b.nextEpoch()

	if len(values) != 1 {
		b.estimated = coin
	} else {
		val := values[0]
		b.estimated = val
		if b.decision == nil && val == coin {
			b.output = val
			b.decision = val
		}
	}

	est := b.estimated.(bool)
	b.sentBinaryValue = append(b.sentBinaryValue, est)
	b.addMessage(&AgreementMessage{b.epoch, &BinaryValueRequest{est}})

	for _, que := range b.delayedMessages {
		_ = b.handleMessage(que.sid, que.msg)
	}
	b.delayedMessages = make([]delayedMessage, 0)
}

// 重置所有值，然后epoch+1
func (b *BBA) nextEpoch() {
	b.binaryValues = make([]bool, 0)
	b.sentBinaryValue = make([]bool, 0)
	b.recvAux = make(map[uint64]bool)
	b.recvBinaryValue = make(map[uint64]bool)
	b.epoch++
}

// 返回recvAux的长度
func (b *BBA) countOutputs() (int, []bool) {
	m := make(map[bool]int)
	for i, val := range b.recvAux {
		m[val] = int(i)
	}

	// 接收到的aux(r)包含的b形成集合values，并且values C bval(r)
	values := make([]bool, 0)
	for _, val := range b.binaryValues {
		if _, ok := m[val]; ok {
			values = append(values, val)
		}
	}
	return len(b.recvAux), values
}

func (b *BBA) countBinaryValue(val bool) int {
	b.lock.RLock()
	defer b.lock.RUnlock()

	n := 0
	for _, v := range b.recvBinaryValue {
		if v == val {
			n++
		}
	}
	return n
}

func (b *BBA) hasSentBinaryValue(val bool) bool {
	for _, v := range b.sentBinaryValue {
		if v == val {
			return true
		}
	}
	return false
}
