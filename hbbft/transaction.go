package hbbft

type Transaction interface {
	Hash() []byte
}
