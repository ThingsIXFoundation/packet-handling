package broadcast

import "github.com/sirupsen/logrus"

type Broadcaster[T any] struct {
	message     chan T
	subscribe   chan chan<- T
	unsubscribe chan chan<- T
	listeners   map[chan<- T]bool
}

func New[T any](bufsize uint) *Broadcaster[T] {
	return &Broadcaster[T]{
		message:     make(chan T, bufsize),
		subscribe:   make(chan chan<- T),
		unsubscribe: make(chan chan<- T),
		listeners:   make(map[chan<- T]bool),
	}
}

func (bc *Broadcaster[T]) Run() *Broadcaster[T] {
	go func() {
		for {
			select {
			case msg := <-bc.message:
				bc.broadcast(msg)
			case ch, ok := <-bc.subscribe:
				if ok {
					bc.listeners[ch] = true
				} else {
					return
				}
			case ch := <-bc.unsubscribe:
				delete(bc.listeners, ch)
			}
		}
	}()
	return bc
}

func (bc *Broadcaster[T]) Subscribe(ch chan<- T) {
	bc.subscribe <- ch
}

func (bc *Broadcaster[T]) Unsubscribe(ch chan<- T) {
	bc.unsubscribe <- ch
}

func (bc *Broadcaster[T]) broadcast(msg T) {
	for ch := range bc.listeners {
		select {
		case ch <- msg:
			continue
		default:
			logrus.Trace("broadcast, drop message")
		}
	}
}

func (bc *Broadcaster[T]) Broadcast(msg T) {
	bc.message <- msg
}

func (bc *Broadcaster[T]) TryBroadcast(msg T) bool {
	select {
	case bc.message <- msg:
		return true
	default:
		return false
	}
}
