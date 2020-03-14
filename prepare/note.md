## 概要

+ HoneyBadger（HB）：从应用接收交易请求并且将请求放入缓存排队，HB还会管理通过`epoch`来区别每次共识。每次有一个新的`epoch`开始，HB都会在这次交易请求中带上`batch`，并传给`ACS`
+ Asynchronous Common Set (ACS)：`ACS`用`RBC`和`BBA`来达成共识
+ Reliable Broadcast (RBC)：将单个节点的`batch`分发到网络中的其余节点。`RBC`的结果是网络中所有节点的`batches`。`RBC`保证每个节点得到相同的输出，即使发送方或其他节点尝试向不同节点发送不同的信息。
+ Byzantine Binary Agreement (BBA)：根据来自网络中所有节点的投票组合来确定`batch`是否包含在`batch`集合中。一旦超过2/3的参与节点同意是否将`batch`包含在`batch`集合中`BBA`就完成。

