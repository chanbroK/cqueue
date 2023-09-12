package cqueue

import "context"

type IQueue interface {
	Enqueue(any) error
	Dequeue() (any, error)
	// WaitDequeue 큐에 원소가 있으면 바로 반환
	// 큐에 원소가 없다면, 원소가 들어올때까지 기다렸다가 반환
	WaitDequeue() (any, error)
	// WaitDequeueWithCtx WaitDequeue 와 동일하게 작동하지만, 인자의 context 가 만료되면 context error 반환
	WaitDequeueWithCtx(ctx context.Context) (any, error)
	GetLen() int
	GetCap() int
	Lock()
	Unlock()
	IsLocked() bool
}
