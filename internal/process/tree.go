package process

type treeController interface {
	Graceful() error
	Kill() error
	Close() error
}
