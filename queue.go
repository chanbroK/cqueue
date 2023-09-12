package cqueue

import (
	"context"
	"fmt"
	"sync"
	"time"
)

const (
	waitChanCapacity  = 1000
	waitInvokeGapTime = 10
)

type Queue struct {
	slice          []any
	mutex          sync.RWMutex
	lockMutex      sync.RWMutex
	isLocked       bool
	waitChan       chan chan any
	unlockWaitChan chan struct{} // 메모리 사이즈가 제일 작아서 struct{} 사용
}

func NewQueue() (queue IQueue) {
	queue = &Queue{
		slice:          make([]any, 0),
		waitChan:       make(chan chan any, waitChanCapacity),
		unlockWaitChan: make(chan struct{}, waitChanCapacity),
	}

	return
}

// Enqueue enqueues an element. Returns error if queue is locked.
func (q *Queue) Enqueue(value interface{}) error {
	if q.isLocked {
		return NewQueueError(LockedQueue, "The queue is locked")
	}

	// let consumers (WaitDequeue) know there is a new element
	select {
	case q.unlockWaitChan <- struct{}{}:
	default:
		// message could not be sent
	}

	// check if there is a listener waiting for the next element (this element)
	select {
	case listener := <-q.waitChan:
		// send the element through the listener's channel instead of enqueue it
		select {
		case listener <- value:
		default:
			// enqueue if listener is not ready

			// lock the object to enqueue the element into the slice
			q.mutex.Lock()
			// enqueue the element
			q.slice = append(q.slice, value)
			defer q.mutex.Unlock()
		}

	default:
		// lock the object to enqueue the element into the slice
		q.mutex.Lock()
		// enqueue the element
		q.slice = append(q.slice, value)
		defer q.mutex.Unlock()
	}

	return nil
}

// Dequeue dequeues an element. Returns error if queue is locked or empty.
func (q *Queue) Dequeue() (interface{}, error) {
	if q.isLocked {
		return nil, NewQueueError(LockedQueue, "The queue is locked")
	}

	q.mutex.Lock()
	defer q.mutex.Unlock()

	length := len(q.slice)
	if length == 0 {
		return nil, NewQueueError(EmptyQueue, "empty queue")
	}

	elementToReturn := q.slice[0]
	q.slice = q.slice[1:]

	return elementToReturn, nil
}

// WaitDequeue dequeues an element (if exist) or waits until the next element gets enqueued and returns it.
// Multiple calls to WaitDequeue() would enqueue multiple "listeners" for future enqueued elements.
func (q *Queue) WaitDequeue() (interface{}, error) {
	return q.WaitDequeueWithCtx(context.Background())
}

// WaitDequeueWithCtx dequeues an element (if exist) or waits until the next element gets enqueued and returns it.
// Multiple calls to WaitDequeueWithCtx() would enqueue multiple "listeners" for future enqueued elements.
// When the passed context expires this function exits and returns the context' error
func (q *Queue) WaitDequeueWithCtx(ctx context.Context) (interface{}, error) {
	for {
		if q.isLocked {
			return nil, NewQueueError(LockedQueue, "The queue is locked")
		}

		// get the slice's len
		q.mutex.Lock()
		length := len(q.slice)
		q.mutex.Unlock()

		if length == 0 {
			// channel to wait for next enqueued element
			waitChan := make(chan interface{})

			select {
			// enqueue a watcher into the watchForNextElementChannel to wait for the next element
			case q.waitChan <- waitChan:

				// n
				for {
					// re-checks every i milliseconds (top: 10 times) ... the following verifies if an item was enqueued
					// around the same time WaitDequeueWithCtx was invoked, meaning the waitChan wasn't yet sent over
					// q.waitForNextElementChan
					for i := 0; i < waitInvokeGapTime; i++ {
						select {
						case <-ctx.Done():
							return nil, ctx.Err()
						case dequeuedItem := <-waitChan:
							return dequeuedItem, nil
						case <-time.After(time.Millisecond * time.Duration(i)):
							if dequeuedItem, err := q.Dequeue(); err == nil {
								return dequeuedItem, nil
							}
						}
					}

					// return the next enqueued element, if any
					select {
					// new enqueued element, no need to keep waiting
					case <-q.unlockWaitChan:
						// check if we got a new element just after we got <-q.unlockDequeueOrWaitForNextElementChan
						select {
						case item := <-waitChan:
							return item, nil
						default:
						}
						// go back to: for loop
						continue

					case item := <-waitChan:
						return item, nil
					case <-ctx.Done():
						return nil, ctx.Err()
					}
					// n
				}
			default:
				// too many watchers (waitForNextElementChanCapacity) enqueued waiting for next elements
				return nil, NewQueueError(InternalChannelClosed, "empty queue and can't wait for next element because there are too many WaitDequeue() waiting")
			}
		}

		q.mutex.Lock()

		// verify that at least 1 item resides on the queue
		if len(q.slice) == 0 {
			q.mutex.Unlock()
			continue
		}
		elementToReturn := q.slice[0]
		q.slice = q.slice[1:]

		q.mutex.Unlock()
		return elementToReturn, nil
	}
}

// Get returns an element's value and keeps the element at the queue
func (q *Queue) Get(index int) (interface{}, error) {
	if q.isLocked {
		return nil, NewQueueError(LockedQueue, "The queue is locked")
	}

	q.mutex.RLock()
	defer q.mutex.RUnlock()

	if len(q.slice) <= index {
		return nil, NewQueueError(IndexOutOfRange, fmt.Sprintf("index out of bounds: %v", index))
	}

	return q.slice[index], nil
}

// Remove removes an element from the queue
func (q *Queue) Remove(index int) error {
	if q.isLocked {
		return NewQueueError(LockedQueue, "The queue is locked")
	}

	q.mutex.Lock()
	defer q.mutex.Unlock()

	if len(q.slice) <= index {
		return NewQueueError(IndexOutOfRange, fmt.Sprintf("index out of bounds: %v", index))
	}

	// remove the element
	q.slice = append(q.slice[:index], q.slice[index+1:]...)

	return nil
}

// GetLen returns the number of enqueued elements
func (q *Queue) GetLen() int {
	q.mutex.RLock()
	defer q.mutex.RUnlock()

	return len(q.slice)
}

// GetCap returns the queue's capacity
func (q *Queue) GetCap() int {
	q.mutex.RLock()
	defer q.mutex.RUnlock()

	return cap(q.slice)
}

// Lock // Locks the queue. No enqueue/dequeue operations will be allowed after this point.
func (q *Queue) Lock() {
	q.lockMutex.Lock()
	defer q.lockMutex.Unlock()

	q.isLocked = true
}

// Unlock unlocks the queue
func (q *Queue) Unlock() {
	q.lockMutex.Lock()
	defer q.lockMutex.Unlock()

	q.isLocked = false
}

// IsLocked returns true whether the queue is locked
func (q *Queue) IsLocked() bool {
	q.lockMutex.RLock()
	defer q.lockMutex.RUnlock()

	return q.isLocked
}
