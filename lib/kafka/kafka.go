package kafka

import (
	"bytes"
	"fmt"
	"log"
	"time"

	"github.com/Shopify/sarama"
	cluster "github.com/bsm/sarama-cluster"
	"github.com/dollarshaveclub/furan/generated/lib"
	"github.com/dollarshaveclub/furan/lib/metrics"
	"github.com/gocql/gocql"
	"github.com/golang/protobuf/proto"
)

const (
	maxFlushMsgs       = 5
	maxFlushFreqSecs   = 1
	connectTimeoutSecs = 10
	keepaliveSecs      = 5
)

var kafkaVersion = sarama.V1_0_0_0

// EventBusProducer describes an object capable of publishing events somewhere
type EventBusProducer interface {
	PublishEvent(*lib.BuildEvent) error
}

// EventBusConsumer describes an object cabable of subscribing to events somewhere
type EventBusConsumer interface {
	SubscribeToTopic(chan<- *lib.BuildEvent, <-chan struct{}, gocql.UUID) error
}

// EventBusManager describes an object that can publish and subscribe to events somewhere
type EventBusManager interface {
	EventBusProducer
	EventBusConsumer
}

// KafkaManager handles sending event messages to the configured Kafka topic
type KafkaManager struct {
	ap           sarama.AsyncProducer
	topic        string
	brokers      []string
	consumerConf *cluster.Config
	mc           metrics.MetricsCollector
	logger       *log.Logger
}

// NewKafkaManager returns a new Kafka manager object
func NewKafkaManager(brokers []string, topic string, maxsends uint, mc metrics.MetricsCollector, logger *log.Logger) (*KafkaManager, error) {
	pconf := sarama.NewConfig()
	pconf.Version = kafkaVersion

	pconf.Net.MaxOpenRequests = int(maxsends)
	pconf.Net.DialTimeout = connectTimeoutSecs * time.Second
	pconf.Net.ReadTimeout = connectTimeoutSecs * time.Second
	pconf.Net.WriteTimeout = connectTimeoutSecs * time.Second
	pconf.Net.KeepAlive = keepaliveSecs * time.Second

	pconf.Producer.Return.Errors = true
	pconf.Producer.Flush.Messages = maxFlushMsgs
	pconf.Producer.Flush.Frequency = maxFlushFreqSecs * time.Second

	asyncp, err := sarama.NewAsyncProducer(brokers, pconf)
	if err != nil {
		return nil, err
	}

	cconf := cluster.NewConfig()
	cconf.Version = pconf.Version
	cconf.Net = pconf.Net
	cconf.Consumer.Return.Errors = true
	cconf.Consumer.Offsets.Initial = sarama.OffsetOldest

	kp := &KafkaManager{
		ap:           asyncp,
		topic:        topic,
		brokers:      brokers,
		consumerConf: cconf,
		mc:           mc,
		logger:       logger,
	}
	go kp.handlePErrors()
	return kp, nil
}

func (kp *KafkaManager) handlePErrors() {
	var kerr *sarama.ProducerError
	for {
		kerr = <-kp.ap.Errors()
		log.Printf("Kafka producer error: %v", kerr)
		kp.mc.KafkaProducerFailure()
	}
}

// PublishEvent publishes a build event to the configured Kafka topic
func (kp *KafkaManager) PublishEvent(event *lib.BuildEvent) error {
	id, err := gocql.ParseUUID(event.BuildId)
	if err != nil {
		return err
	}
	val, err := proto.Marshal(event)
	if err != nil {
		return fmt.Errorf("error marshaling protobuf: %v", err)
	}
	pmsg := &sarama.ProducerMessage{
		Topic: kp.topic,
		Key:   sarama.ByteEncoder(id.Bytes()), // Key is build ID to preserve event order (all events of a build go to the same partition)
		Value: sarama.ByteEncoder(val),
	}
	select { // don't block if Kafka is unavailable for some reason
	case kp.ap.Input() <- pmsg:
		return nil
	default:
		kp.mc.KafkaProducerFailure()
		return fmt.Errorf("could not publish Kafka message: channel full")
	}
}

// SubscribeToTopic listens to the configured topic, filters by build_id and writes
// the resulting messages to output. When the subscribed build is finished
// output is closed. done is a signal from the caller to abort the stream subscription
func (kp *KafkaManager) SubscribeToTopic(output chan<- *lib.BuildEvent, done <-chan struct{}, buildID gocql.UUID) error {
	// random group ID for each connection
	groupid, err := gocql.RandomUUID()
	if err != nil {
		return err
	}
	con, err := cluster.NewConsumer(kp.brokers, groupid.String(), []string{kp.topic}, kp.consumerConf)
	if err != nil {
		return err
	}
	handleConsumerErrors := func() {
		var err error
		for {
			err = <-con.Errors()
			if err == nil { // chan closed
				return
			}
			kp.mc.KafkaConsumerFailure()
			kp.logger.Printf("Kafka consumer error: %v", err)
		}
	}
	go handleConsumerErrors()
	go func() {
		defer close(output)
		defer con.Close()
		var err error
		var msg *sarama.ConsumerMessage
		var event *lib.BuildEvent
		input := con.Messages()
		for {
			select {
			case <-done:
				kp.logger.Printf("SubscribeToTopic: aborting")
				return
			default:
				break
			}
			msg = <-input
			if msg == nil {
				return
			}
			if bytes.Equal(msg.Key, []byte(buildID[:])) {
				event = &lib.BuildEvent{}
				err = proto.Unmarshal(msg.Value, event)
				if err != nil {
					kp.logger.Printf("%v: error unmarshaling event from Kafka stream: %v", buildID.String(), err)
					continue
				}
				output <- event
				if event.BuildFinished || event.EventError.IsError {
					return
				}
			}
		}
	}()
	return nil
}
