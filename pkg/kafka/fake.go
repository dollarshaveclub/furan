package kafka

import (
	"fmt"

	"github.com/dollarshaveclub/furan/pkg/generated/furanrpc"
	"github.com/gocql/gocql"
)

type FakeEventBus struct {
	c chan *furanrpc.BuildEvent
}

var _ EventBusProducer = &FakeEventBus{}
var _ EventBusConsumer = &FakeEventBus{}

func NewFakeEventBusProducer(capacity uint) *FakeEventBus {
	return &FakeEventBus{
		c: make(chan *furanrpc.BuildEvent, capacity),
	}
}

func (fb *FakeEventBus) PublishEvent(event *furanrpc.BuildEvent) error {
	select {
	case fb.c <- event:
		return nil
	default:
		return fmt.Errorf("channel full")
	}
}

func (fb *FakeEventBus) SubscribeToTopic(c chan<- *furanrpc.BuildEvent, done <-chan struct{}, id gocql.UUID) error {
	if fb.c == nil {
		return fmt.Errorf("nil channel")
	}
	go func() {
		for {
			select {
			case event, ok := <-fb.c:
				if !ok {
					return
				}
				if event != nil && event.BuildId == id.String() {
					c <- event
				}
			case <-done:
				return
			}
		}
	}()
	return nil
}
