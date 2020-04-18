package hbbft

import (
	"fmt"
)

type ACS struct {
	Config
	// id -> rbc
	rbcInstances map[uint64]*RBC
	// id -> bba
	bbaInstances map[uint64]*BBA
	// 可靠广播的结果
	rbcResults map[uint64][]byte
	// 二进制拜占庭共识的结果
	bbaResults map[uint64]bool
	// ACS最终的输出
	output map[uint64][]byte
	// 需要被广播的消息
	messageList *messageList
	// ACS是否已经得出结果
	decided bool

	// 内部使用的通道
	closeCh   chan struct{}
	inputCh   chan acsInput
	messageCh chan acsMessageSet
}

func NewACS(cfg Config) *ACS {
	if cfg.F == 0 {
		cfg.F = (cfg.N) / 4
	}
	acs := &ACS{
		Config:       cfg,
		rbcInstances: make(map[uint64]*RBC),
		bbaInstances: make(map[uint64]*BBA),
		rbcResults:   make(map[uint64][]byte),
		bbaResults:   make(map[uint64]bool),
		messageList:  newMessageList(),
		closeCh:      make(chan struct{}),
		inputCh:      make(chan acsInput),
		messageCh:    make(chan acsMessageSet),
	}

	for _, id := range cfg.Nodes {
		acs.rbcInstances[id] = NewRBC(cfg, id)
		acs.bbaInstances[id] = NewBBA(cfg)
	}

	go acs.run()

	return acs
}

func (a *ACS) run() {
	for {
		select {
		case t := <-a.inputCh:
			err := a.inputValue(t.value)
			t.response <- acsInputResponse{err: err}
		case t := <-a.messageCh:
			t.err <- a.handleMessage(t.senderID, t.msg)
		case <-a.closeCh:
			return
		}
	}
}

type ACSMessage struct {
	ProposerID uint64
	Payload    interface{}
}

type acsMessageSet struct {
	senderID uint64
	msg      *ACSMessage
	err      chan error
}

type acsInputResponse struct {
	rbcMessages []*BroadcastMessage
	acsMessages []*ACSMessage
	err         error
}

type acsInput struct {
	value    []byte
	response chan acsInputResponse
}

func (a *ACS) InputValue(val []byte) error {
	t := acsInput{
		value:    val,
		response: make(chan acsInputResponse),
	}
	a.inputCh <- t
	resp := <-t.response
	return resp.err
}

func (a *ACS) HandleMessage(senderID uint64, msg *ACSMessage) error {
	t := acsMessageSet{
		senderID: senderID,
		msg:      msg,
		err:      make(chan error),
	}
	a.messageCh <- t
	return <-t.err
}

// 根据payload的类型分别处理
func (a *ACS) handleMessage(senderID uint64, msg *ACSMessage) error {
	switch t := msg.Payload.(type) {
	case *AgreementMessage:
		return a.processAgreement(msg.ProposerID, func(bba *BBA) error {
			return bba.HandleMessage(senderID, t)
		})
	case *BroadcastMessage:
		return a.processBroadcast(msg.ProposerID, func(rbc *RBC) error {
			return rbc.HandleMessage(senderID, t)
		})
	default:
		return fmt.Errorf("unknown message %v", t)
	}
}

// ACS的输出
func (a *ACS) Output() map[uint64][]byte {
	out := a.output
	a.output = nil
	return out
}

// 与rbc、bba交互
func (a *ACS) inputValue(data []byte) error {
	rbc, ok := a.rbcInstances[a.ID]
	if !ok {
		return fmt.Errorf("can not find rbc instance %d", a.ID)
	}

	msgs, err := rbc.InputValue(data)
	if err != nil {
		return err
	}
	if len(msgs) != a.N-1 {
		return fmt.Errorf("getMessages not enough")
	}

	for i := 0; i < a.N-1; i++ {
		if a.Nodes[i] != a.ID {
			a.messageList.addMessage(&ACSMessage{a.ID, msgs[i]}, a.Nodes[i])
		}
	}

	if output := rbc.Output(); output != nil {
		a.rbcResults[a.ID] = output
		err = a.processAgreement(a.ID, func(bba *BBA) error {
			if bba.NotProvidedInput() {
				return bba.InputValue(true)
			}
			return nil
		})
	}
	return err
}

func (a *ACS) processBroadcast(pid uint64, fun func(rbc *RBC) error) error {
	rbc, ok := a.rbcInstances[pid]
	if !ok {
		return fmt.Errorf("can not find rbc instance %d", pid)
	}

	if err := fun(rbc); err != nil {
		return err
	}

	for _, msg := range rbc.Messages() {
		a.addMessage(pid, msg)
	}

	if output := rbc.Output(); output != nil {
		a.rbcResults[pid] = output
		return a.processAgreement(pid, func(bba *BBA) error {
			if bba.NotProvidedInput() {
				return bba.InputValue(true)
			}
			return nil
		})
	}
	return nil
}

func (a *ACS) processAgreement(pid uint64, fun func(bba *BBA) error) error {
	bba, ok := a.bbaInstances[pid]
	if !ok {
		return fmt.Errorf("can not find bba instance %d", pid)
	}
	if bba.done {
		return nil
	}

	if err := fun(bba); err != nil {
		return err
	}
	for _, msg := range bba.Messages() {
		a.addMessage(pid, msg)
	}

	if output := bba.Output(); output != nil {
		if _, ok := a.bbaResults[pid]; ok {
			return fmt.Errorf("already has bba results for %d", pid)
		}

		out, ok := output.(bool)
		if !ok {
			return fmt.Errorf("output.(bool) error")
		}

		a.bbaResults[pid] = out

		if out && a.countOne() == a.N-a.F {
			for id, bba := range a.bbaInstances {
				if bba.NotProvidedInput() {
					if err := bba.InputValue(false); err != nil {
						return err
					}

					for _, msg := range bba.Messages() {
						a.addMessage(id, msg)
					}

					if output := bba.Output(); output != nil {
						out, ok := output.(bool)
						if !ok {
							return fmt.Errorf("output.(bool) error")
						}
						a.bbaResults[id] = out
					}
				}
			}
		}

		if a.decided || a.countOne() < a.N-a.F || len(a.bbaResults) < a.N {
			return nil
		}

		bbaIds := make([]uint64, 0)
		for id, value := range a.bbaResults {
			if value {
				bbaIds = append(bbaIds, id)
			}
		}

		rbcResults := make(map[uint64][]byte)
		for _, id := range bbaIds {
			val, _ := a.rbcResults[id]
			rbcResults[id] = val
		}

		a.output = rbcResults
		a.decided = true
	}
	return nil
}

func (a *ACS) addMessage(from uint64, msg interface{}) {
	for _, id := range a.Nodes {
		if id != a.ID {
			a.messageList.addMessage(&ACSMessage{from, msg}, id)
		}
	}
}

func (a *ACS) countOne() int {
	n := 0
	for _, ok := range a.bbaResults {
		if ok {
			n++
		}
	}
	return n
}
