package cqueue

import "context"

type FixedQueue struct {
	queue    chan any
	lockChan chan struct{}
	// queue for watchers that will wait for next elements (if queue is empty at DequeueOrWaitForNextElement execution )
	waitChan chan chan any
}

func NewFixedQueue(capacity int) *FixedQueue {
	queue := &FixedQueue{
		queue:    make(chan any, capacity),
		lockChan: make(chan struct{}, 1),
		waitChan: make(chan chan any, waitChanCapacity),
	}

	return queue
}

// Enqueue enqueues an element. Returns error if queue is locked or it is at full capacity.
func (q *FixedQueue) Enqueue(value any) error {
	if q.IsLocked() {
		return NewQueueError(LockedQueue, "The queue is locked")
	}

	// check if there is a listener waiting for the next element (this element)
	select {
	case listener := <-q.waitChan:
		// verify whether it is possible to notify the listener (it could be the listener is no longer
		// available because the context expired: WaitDequeueWithCtx)
		select {
		// sends the element through the listener's channel instead of enqueueing it
		case listener <- value:
		default:
			// push the element into the queue instead of sending it through the listener's channel (which is not available at this moment)
			return q.enqueueIntoQueue(value)
		}

	default:
		// enqueue the element into the queue
		return q.enqueueIntoQueue(value)
	}

	return nil
}

// enqueueIntoQueue enqueues the given item directly into the regular queue
func (q *FixedQueue) enqueueIntoQueue(value any) error {
	select {
	case q.queue <- value:
	default:
		return NewQueueError(ExceedCapacity, "FixedQueue queue is at full capacity")
	}

	return nil
}

// Dequeue dequeues an element. Returns error if: queue is locked, queue is empty or internal channel is closed.
func (q *FixedQueue) Dequeue() (any, error) {
	if q.IsLocked() {
		return nil, NewQueueError(LockedQueue, "The queue is locked")
	}

	select {
	case value, ok := <-q.queue:
		if ok {
			return value, nil
		}
		return nil, NewQueueError(InternalChannelClosed, "internal channel is closed")
	default:
		return nil, NewQueueError(EmptyQueue, "empty queue")
	}
}

// DequeueOrWaitForNextElement dequeues an element (if exist) or waits until the next element gets enqueued and returns it.
// Multiple calls to DequeueOrWaitForNextElement() would enqueue multiple "listeners" for future enqueued elements.
func (q *FixedQueue) DequeueOrWaitForNextElement() (any, error) {
	return q.DequeueOrWaitForNextElementContext(context.Background())
}

// DequeueOrWaitForNextElementContext dequeues an element (if exist) or waits until the next element gets enqueued and returns it.
// Multiple calls to DequeueOrWaitForNextElementContext() would enqueue multiple "listeners" for future enqueued elements.
// When the passed context expires this function exits and returns the context' error
func (q *FixedQueue) DequeueOrWaitForNextElementContext(ctx context.Context) (any, error) {
	if q.IsLocked() {
		return nil, NewQueueError(LockedQueue, "The queue is locked")
	}

	select {
	case value, ok := <-q.queue:
		if ok {
			return value, nil
		}
		return nil, NewQueueError(InternalChannelClosed, "internal channel is closed")
	case <-ctx.Done():
		return nil, ctx.Err()
	// queue is empty, add a listener to wait until next enqueued element is ready
	default:
		// channel to wait for next enqueued element
		waitChan := make(chan any)

		select {
		// enqueue a watcher into the watchForNextElementChannel to wait for the next element
		case q.waitChan <- waitChan:
			// return the next enqueued element, if any
			select {
			case item := <-waitChan:
				return item, nil
			case <-ctx.Done():
				return nil, ctx.Err()
			// try again to get the element from the regular queue (in case waitChan doesn't provide any item)
			case value, ok := <-q.queue:
				if ok {
					return value, nil
				}
				return nil, NewQueueError(InternalChannelClosed, "internal channel is closed")
			}
		default:
			// too many watchers (waitForNextElementChanCapacity) enqueued waiting for next elements
			return nil, NewQueueError(EmptyQueue, "empty queue and can't wait for next element")
		}

		//return nil, NewQueueError(QueueErrorCodeEmptyQueue, "empty queue")
	}
}

// GetLen returns queue's length (total enqueued elements)
func (q *FixedQueue) GetLen() int {
	q.Lock()
	defer q.Unlock()

	return len(q.queue)
}

// GetCap returns the queue's capacity
func (q *FixedQueue) GetCap() int {
	q.Lock()
	defer q.Unlock()

	return cap(q.queue)
}

func (q *FixedQueue) Lock() {
	// non-blocking fill the channel
	select {
	case q.lockChan <- struct{}{}:
	default:
	}
}

func (q *FixedQueue) Unlock() {
	// non-blocking flush the channel
	select {
	case <-q.lockChan:
	default:
	}
}

func (q *FixedQueue) IsLocked() bool {
	return len(q.lockChan) >= 1
}
