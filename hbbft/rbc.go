package hbbft

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"sort"
	"sync"

	"github.com/NebulousLabs/merkletree"
	"github.com/klauspost/reedsolomon"
)

type RBC struct {
	Config
	// 提议的节点id
	proposerID uint64
	// sendID -> root hash
	recvReadys map[uint64]*ReadyRequest
	// sendID -> EchoRequest
	recvEchos map[uint64]*EchoRequest

	// 里德所罗门编码，可以将消息拆成多个部分，然后用这些部分的叫小部分集合进行重构，节省分发时的带宽
	encoder reedsolomon.Encoder
	// 分片数
	numParityShards int
	numDataShards   int

	// 一些内部状态的布尔值
	echoSent  bool
	readySent bool
	decided   bool

	lock sync.Mutex
	// 需要被广播的消息
	messages []*BroadcastMessage

	// 该实例产生的
	output []byte

	// 内部使用的通道
	closeCh   chan struct{}
	inputCh   chan rbcInput
	messageCh chan rbcMessage
}

func NewRBC(cfg Config, proposerID uint64) *RBC {
	parityShards := 2 * cfg.F
	dataShards := cfg.N - parityShards

	encoder, err := reedsolomon.New(dataShards, parityShards)
	if err != nil {
		panic(err)
	}

	rbc := &RBC{
		Config:          cfg,
		proposerID:      proposerID,
		recvEchos:       make(map[uint64]*EchoRequest),
		recvReadys:      make(map[uint64]*ReadyRequest),
		encoder:         encoder,
		numParityShards: parityShards,
		numDataShards:   dataShards,
		messages:        make([]*BroadcastMessage, 0),
		closeCh:         make(chan struct{}),
		inputCh:         make(chan rbcInput),
		messageCh:       make(chan rbcMessage),
	}

	go rbc.run()

	return rbc
}

type rbcMessage struct {
	senderID uint64
	msg      *BroadcastMessage
	err      chan error
}

type rbcInputResponse struct {
	messages []*BroadcastMessage
	err      error
}

type rbcInput struct {
	value    []byte
	response chan rbcInputResponse
}

type ProofRequest struct {
	RootHash []byte
	// Proof[0]
	Proof  [][]byte
	Index  int
	Leaves int
}

type EchoRequest struct {
	ProofRequest
}

type ReadyRequest struct {
	ProofRequest
}

type proofList []ProofRequest

func (p proofList) Len() int {
	return len(p)
}

func (p proofList) Swap(i, j int) {
	p[i], p[j] = p[j], p[i]
}

func (p proofList) Less(i, j int) bool {
	return p[i].Index < p[j].Index
}

// 输入，然后返回结果
func (r *RBC) InputValue(data []byte) ([]*BroadcastMessage, error) {
	input := rbcInput{
		value:    data,
		response: make(chan rbcInputResponse),
	}
	r.inputCh <- input
	// 等待结果
	resp := <-input.response
	return resp.messages, resp.err
}

// 输入，然后返回错误
func (r *RBC) HandleMessage(senderID uint64, msg *BroadcastMessage) error {
	m := rbcMessage{
		senderID: senderID,
		msg:      msg,
		err:      make(chan error),
	}
	r.messageCh <- m
	return <-m.err
}

// 返回messages，并清空
func (r *RBC) Messages() []*BroadcastMessage {
	r.lock.Lock()
	defer r.lock.Unlock()

	msgs := r.messages
	r.messages = []*BroadcastMessage{}
	return msgs
}

// 返回output，并清空
func (r *RBC) Output() []byte {
	out := r.output
	r.output = nil
	return out
}

// 实际的处理，inputValue和handleMessage
func (r *RBC) run() {
	for {
		select {
		case t := <-r.inputCh:
			msgs, err := r.inputValue(t.value)
			t.response <- rbcInputResponse{
				messages: msgs,
				err:      err,
			}
		case t := <-r.messageCh:
			t.err <- r.handleMessage(t.senderID, t.msg)
		case <-r.closeCh:
			return
		}
	}
}

// 分片、建默克尔树、第一块分片留给自己，验证proof，返回剩余分片
func (r *RBC) inputValue(data []byte) ([]*BroadcastMessage, error) {
	shards, err := r.encoder.Split(data)
	if err != nil {
		return nil, err
	}
	if err := r.encoder.Encode(shards); err != nil {
		return nil, err
	}

	msgs := make([]*BroadcastMessage, len(shards))
	for i := 0; i < len(msgs); i++ {
		tree := merkletree.New(sha256.New())
		if err := tree.SetIndex(uint64(i)); err != nil {
			return nil, err
		}

		for i := 0; i < len(shards); i++ {
			tree.Push(shards[i])
		}

		root, proof, proofIndex, n := tree.Prove()
		msgs[i] = &BroadcastMessage{
			Payload: &ProofRequest{
				RootHash: root,
				Proof:    proof,
				Index:    int(proofIndex),
				Leaves:   int(n),
			},
		}
	}

	proof, ok := msgs[0].Payload.(*ProofRequest)
	if !ok {
		return nil, fmt.Errorf("payload decode err")
	}

	if err := r.handleProofRequest(r.ID, proof); err != nil {
		return nil, err
	}

	return msgs[1:], nil
}

// 根据msg类型分别处理
func (r *RBC) handleMessage(senderID uint64, msg *BroadcastMessage) error {
	switch t := msg.Payload.(type) {
	case *ProofRequest:
		return r.handleProofRequest(senderID, t)
	case *EchoRequest:
		return r.handleEchoRequest(senderID, t)
	case *ReadyRequest:
		return r.handleReadyRequest(senderID, t)
	}
	return nil
}

// 当一个节点收到一个来自proposer的proof，验证合法之后广播这个proof
func (r *RBC) handleProofRequest(senderID uint64, req *ProofRequest) error {
	if senderID != r.proposerID {
		return fmt.Errorf("senderId != proposerId")
	}
	if r.echoSent {
		return fmt.Errorf("proof has recvieved")
	}
	if !merkletree.VerifyProof(sha256.New(), req.RootHash, req.Proof, uint64(req.Index), uint64(req.Leaves)) {
		return fmt.Errorf("invalid proof")
	}

	r.echoSent = true
	echo := &EchoRequest{*req}
	// 广播收到的proof
	r.messages = append(r.messages, &BroadcastMessage{echo})
	return r.handleEchoRequest(r.ID, echo)
}

// 处理echo，判断是否ready
func (r *RBC) handleEchoRequest(senderID uint64, req *EchoRequest) error {
	if !merkletree.VerifyProof(sha256.New(), req.RootHash, req.Proof, uint64(req.Index), uint64(req.Leaves)) {
		return fmt.Errorf("invalid proof")
	}

	r.recvEchos[senderID] = req
	if r.readySent || r.countEchos(req.RootHash) < r.N-r.F {
		return r.decodeValue(req.RootHash)
	}

	r.readySent = true
	ready := &ReadyRequest{ProofRequest{RootHash: req.RootHash}}
	r.messages = append(r.messages, &BroadcastMessage{ready})
	return r.handleReadyRequest(r.ID, ready)
}

// 处理ready，尝试decode
func (r *RBC) handleReadyRequest(senderID uint64, req *ReadyRequest) error {
	if _, ok := r.recvReadys[senderID]; ok {
		return fmt.Errorf("already received ready from server %d", senderID)
	}
	r.recvReadys[senderID] = req

	if r.countReadys(req.RootHash) == r.F+1 && !r.readySent {
		r.readySent = true
		ready := &ReadyRequest{ProofRequest{RootHash: req.RootHash}}
		r.messages = append(r.messages, &BroadcastMessage{ready})
	}
	return r.decodeValue(req.RootHash)
}

func (r *RBC) decodeValue(hash []byte) error {
	if r.decided || r.countReadys(hash) <= 2*r.F || r.countEchos(hash) <= r.F {
		return nil
	}

	r.decided = true
	var proof proofList
	for _, echo := range r.recvEchos {
		proof = append(proof, echo.ProofRequest)
	}
	sort.Sort(proof)

	// 重建shards
	shards := make([][]byte, r.numParityShards+r.numDataShards)
	for _, p := range proof {
		shards[p.Index] = p.Proof[0]
	}
	if err := r.encoder.Reconstruct(shards); err != nil {
		return nil
	}

	var result []byte
	for _, data := range shards[:r.numDataShards] {
		result = append(result, data...)
	}
	r.output = result

	return nil
}

func (r *RBC) countEchos(hash []byte) int {
	n := 0
	for _, h := range r.recvEchos {
		if bytes.Compare(hash, h.RootHash) == 0 {
			n++
		}
	}
	return n
}

func (r *RBC) countReadys(hash []byte) int {
	n := 0
	for _, r := range r.recvReadys {
		if bytes.Compare(hash, r.RootHash) == 0 {
			n++
		}
	}
	return n
}
