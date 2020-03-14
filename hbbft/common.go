package hbbft

// hb
type HBMessage struct {
	Epoch   uint64
	Payload interface{}
}

// rbc
type BroadcastMessage struct {
	Payload interface{}
}

// bba
type AgreementMessage struct {
	Epoch   int
	Message interface{}
}
