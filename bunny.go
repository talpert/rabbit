package rabbit

import (
	"crypto/tls"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/streadway/amqp"
)

type MainThing struct {
	connDetails *ConnectionDetails
	connected   bool

	conn        *amqp.Connection
	notifyClose chan *amqp.Error

	consumers   []*consumerChan
	consumerMux *sync.RWMutex

	// TODO producers

	// todo maybe a global reconnect lock?
	//  then we could avoid having to go and lock each one

	log Logger
}

type ConnectionDetails struct {
	// Required; format "amqp://user:pass@host:port"
	URLs []string

	// How long to wait before we retry connecting to a server (after disconnect)
	RetryReconnectSec int

	// Use TLS
	UseTLS bool

	// Skip cert verification (only applies if UseTLS is true)
	SkipVerifyTLS bool
}

func NewMainThing(details ConnectionDetails) *MainThing {
	return &MainThing{
		connDetails: &details,
		consumerMux: &sync.RWMutex{},
		log:         &NoOpLogger{},
	}
}

func (m *MainThing) Connect() error {
	if m.connected {
		return errors.New("connect may only be called once")
	}

	return m.connect()
}

// internal version of connect is reusable for reconnects
func (m *MainThing) connect() error {
	var conn *amqp.Connection
	var err error

	for i, url := range m.connDetails.URLs {
		if m.connDetails.UseTLS {
			tlsConfig := &tls.Config{}

			if m.connDetails.SkipVerifyTLS {
				tlsConfig.InsecureSkipVerify = true
			}

			conn, err = amqp.DialTLS(url, tlsConfig)
		} else {
			conn, err = amqp.Dial(url)
		}

		if err == nil {
			break
		}

		m.log.Errorf("failed to connect to URL %d: %v", i, err)
	}
	if err != nil {
		return fmt.Errorf("failed to connect to all URLs. Last error: %v", err)
	}

	m.connected = true
	m.conn = conn

	// always create a new channel since the old one is closed
	m.notifyClose = m.conn.NotifyClose(make(chan *amqp.Error, 0))

	return nil
}

func (m *MainThing) watchNotifyClose() {
	// infinitely watch for close notification, reconnect, repeat
	for {
		closeErr := <-m.notifyClose
		m.log.Debugf("received message on notify close channel: '%+v' (reconnecting)", closeErr)

		m.reconnect()

		m.log.Debug("watchNotifyClose has completed successfully")
	}
}

// helper function for testability
func (m *MainThing) reconnect() {
	// Acquire mutex to pause all consumers/producers while we reconnect AND prevent
	// access to the channel map
	// TODO can we still drop messages here

	var attempts int

	beginTime := time.Now()

	for {
		m.log.Warnf("Attempting to reconnect. All processing has been blocked for %v", time.Since(beginTime))

		attempts++
		if err := m.connect(); err != nil {
			m.log.Warnf("failed attempt %d to reconnect: %s; retrying in %d", attempts, err, m.connDetails.RetryReconnectSec)
			time.Sleep(time.Duration(m.connDetails.RetryReconnectSec) * time.Second)
			continue
		}
		m.log.Debugf("successfully reconnected after %d attempts in %v", attempts, time.Since(beginTime))
		break
	}

	return
}

// helper function for testability and use of defers
func (m *MainThing) restartConsumers() {
	m.consumerMux.Lock()
	defer m.consumerMux.Unlock()

	for _, consumer := range m.consumers {
		if err := consumer.restart(m.conn); err != nil {
			// TODO: not sure about how we want to handle these
		}
	}

	return
}

func (m *MainThing) SeLogger(logger Logger) {
	m.log = logger
}

// Used to declare topology of rabbit via use of amqp functions
type ChannelSetupFunc func(ch *amqp.Channel) error

// Function that is run against evey item delivered from rabbit
//  Author of this function is responsible for calling Delivery.Ack()
type ConsumeFunc func(msg amqp.Delivery) error

type consumerChan struct {
	amqpChan      *amqp.Channel
	chanSetupFunc ChannelSetupFunc
	consumers     []*consumerDefinition
	mux           *sync.RWMutex
}

type consumerDefinition struct {
	consumeFunc  ConsumeFunc
	opts         *ConsumeOptions
	deliveryChan <-chan amqp.Delivery
	errorChan    chan<- error
	deliveryMux  *sync.RWMutex
}

type ConsumeOptions struct {
	QueueName   string
	consumerTag string
	AutoAck     bool
	Exclusive   bool
	NoWait      bool
}

// exported version of consume for the user to kick of consumption
func (c *consumerChan) Consume(consumeFunc ConsumeFunc, opts ConsumeOptions, errs chan<- error) error {
	def := &consumerDefinition{
		consumeFunc: consumeFunc,
		opts:        &opts,
		errorChan:   errs,
		deliveryMux: &sync.RWMutex{},
	}

	if err := c.consume(def); err != nil {
		return err
	}

	// do this after consuming begins because if we can not get this lock, all
	//  other operations on the connection will be locked and we could be in total deadlock
	c.mux.Lock()
	defer c.mux.Unlock()
	c.consumers = append(c.consumers, def)

	return nil
}

// internal consume that is reusable for restarts
func (c *consumerChan) consume(def *consumerDefinition) error {
	// generate a consumer tag to ensure uniqueness
	id, err := uuid.NewRandom()
	if err != nil {
		return fmt.Errorf("failed to generate consumer tag: %v", err)
	}

	def.opts.consumerTag = id.String()

	deliveries, err := c.amqpChan.Consume(
		def.opts.QueueName,
		def.opts.consumerTag,
		def.opts.AutoAck,
		def.opts.Exclusive,
		false, // noLocal is not supported by Rabbit
		def.opts.NoWait,
		nil,
	)
	if err != nil {
		return fmt.Errorf("failed to begin consuming from channel: %v", err)
	}

	// TODO: take code from Rabbit lib
	go func() {
		for {
			item := <-deliveries
			if err := def.consumeFunc(item); err != nil {
				def.errorChan <- err
			}
		}
	}()

	// must lock here because in the restart case, something may be using it
	def.deliveryMux.Lock()
	defer def.deliveryMux.Unlock()
	def.deliveryChan = deliveries

	return nil
}

// helper wrapper for Consume in the restart case
func (c *consumerChan) restart(conn *amqp.Connection) error {
	if err := establishConsumerChan(c, conn); err != nil {
		return err
	}

	for _, consumer := range c.consumers {
		if err := c.consume(consumer); err != nil {
			// TODO: do we aggregate and return any errors or just error immediately?
		}
	}

	return nil
}

func (m *MainThing) NewConsumerChannel(setupFunc ChannelSetupFunc) (*consumerChan, error) {
	// create our representation of the chan to store
	cc := &consumerChan{
		chanSetupFunc: setupFunc,
		mux:           &sync.RWMutex{},
	}

	if err := establishConsumerChan(cc, m.conn); err != nil {
		return nil, err
	}

	// append it safely
	m.consumerMux.Lock()
	defer m.consumerMux.Unlock()
	m.consumers = append(m.consumers, cc)

	return cc, nil
}

// A helper to establish the consumer channel. A shared implementation used in connect and reconnect
func establishConsumerChan(cc *consumerChan, conn *amqp.Connection) error {
	// establish a channel
	ch, err := conn.Channel()
	if err != nil {
		return fmt.Errorf("failed to initialize channel: %v", err)
	}

	// This should be safe because we would not be establishing a chan if
	//  it is in active use
	cc.amqpChan = ch

	// run user provided topology setup
	if err := cc.chanSetupFunc(ch); err != nil {
		return err
	}

	return nil
}
