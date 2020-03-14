package hbbft

import (
	"sync"
)

type Message struct {
	To      uint64
	Payload interface{}
}

type messageList struct {
	messages []Message
	lock     sync.Mutex
}

func newMessageList() *messageList {
	return &messageList{
		messages: []Message{},
	}
}

func (q *messageList) addMessage(msg interface{}, to uint64) {
	q.lock.Lock()
	defer q.lock.Unlock()
	q.messages = append(q.messages, Message{to, msg})
}

func (q *messageList) getMessages() []Message {
	q.lock.Lock()
	defer q.lock.Unlock()

	msgs := q.messages
	q.messages = []Message{}
	return msgs
}
