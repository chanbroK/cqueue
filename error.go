package cqueue

const (
	EmptyQueue            = "queue empty"
	LockedQueue           = "queue locked"
	IndexOutOfRange       = "index out of range"
	ExceedCapacity        = "exceed capacity"
	InternalChannelClosed = "internal channel closed"
)

type QueueError struct {
	code    string
	message string
}

func NewQueueError(code string, message string) *QueueError {
	return &QueueError{
		code:    code,
		message: message,
	}
}

func (qe *QueueError) Error() string {
	return qe.message
}

func (qe *QueueError) Code() string {
	return qe.code
}
