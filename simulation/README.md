# Simulation

### How to run

If in root folder of the hbbft project:
```
go run simulation/main.go 
```

> Give the simulation couple seconds to come up to speed. In the first epochs the transaction buffer will be empty or very small. 

### Tuning parameters

The following parameters can be tuned
- lenNodes (default 4)
- batchSize (default 500)
- numCores (default 4)
- transaction verification delay (default 2ms)


## 通用结构体
### 节点 Server
```go
type Server struct {
	id          uint64
	hb          *hbbft.HoneyBadger
	transport   hbbft.Transport // 模拟网络传输的interface
	rpcCh       <-chan hbbft.RPC
	lock        sync.RWMutex
	mempool     map[string]*Transaction
	totalCommit int
	start       time.Time
}
```

+ verifyTransaction：模拟验证交易的延迟
+ addTransactions：加进mempool，调用honey badger的AddTransaction方法，通过relayCh传给其他节点
+ txLoop：定时产生交易，调用addTransactions
+ commitLoop：定时结算提交的交易，从honey badger的output里得到结果，打印数据
+ run：异步调用txLoop和commitLoop
+ makeNetwork：生成一个连接好的集群
+ connectTransports：连接Transport
+ makeIds：0 ～ n-1

### 交易 Transaction
```go
type Transaction struct {
	Nonce uint64
}
```

+ Hash：哈希成[]byte

### 传输 Transport interface
```go
type RPC struct {
	NodeID uint64
	Payload interface{}
}

type Transport interface {
	Consume() <-chan RPC

	SendProofMessages(from uint64, msgs []interface{}) error

	Broadcast(from uint64, msg interface{}) error

	SendMessage(from, to uint64, msg interface{}) error

	Connect(uint64, Transport) // 连接两个Transport

	Addr() uint64
}
```

### 本地传输 LocalTransport
```go
type LocalTransport struct {
	lock      sync.RWMutex
	peers     map[uint64]*LocalTransport
	consumeCh chan RPC
	addr      uint64
}
```

+ makeRPC：根据addr找到要发送的节点，向该节点的consumeCh写入一个RPC
+ Consume：返回consumeCh
+ SendProofMessages：//
+ Broadcast：向其他每个节点广播
+ SendMessage：向指定节点发送消息
+ Connect：加入到peers里
+ Addr：返回addr

### HoneyBadger
```go
type Config struct {
	// Number of participating nodes.
	N int
	// Number of faulty nodes.
	F int
	// Unique identifier of the node.
	ID uint64
	// Identifiers of the participating nodes.
	Nodes []uint64
	// Maximum number of transactions that will be comitted in one epoch.
	BatchSize int
}
// HoneyBadger represents the top-level protocol of the hbbft consensus.
type HoneyBadger struct {
	// Config holds the configuration of the engine. This may not change
	// after engine initialization.
	Config
	// The instances of the "Common Subset Algorithm for some epoch.
	acsInstances map[uint64]*ACS
	// The unbound buffer of transactions.
	txBuffer *buffer
	// current epoch.
	epoch uint64

	lock sync.RWMutex
	// Transactions that are commited in the corresponding epochs.
	outputs map[uint64][]Transaction
	// Que of messages that need to be broadcast after processing a message.
	messageQue *messageQue
	// Counter that counts the number of messages sent in one epoch.
	msgCount int
}
```

### main()
1. makeNetwork(lenNodes)
2. 遍历每个节点，调用run，调用每个节点的hb.Start，将每个节点的hb.Messages写入messages中
3. 遍历relayCh，然后遍历每个节点，调用每个节点的addTransactions
4. 死循环：从messages里拿到消息，取到该消息要送到的节点，调用该节点的hb.HandleMessage，遍历hb.Messages，写入messages
