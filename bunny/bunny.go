package bunny

import (
	"crypto/tls"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/streadway/amqp"

	"github.com/batchcorp/rabbit"
)

var log rabbit.Logger

func init() {
	log = &rabbit.NoOpLogger{}
}

func SeLogger(logger rabbit.Logger) {
	log = logger
}

type Bunny struct {
	connDetails *ConnectionDetails
	connected   bool

	conn        *amqp.Connection
	notifyClose chan *amqp.Error

	consumers   []*consumerChan
	consumerMux *sync.RWMutex

	// TODO producers

	// todo maybe a global reconnect lock?
	//  then we could avoid having to go and lock each one
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

func NewBunny(details ConnectionDetails) *Bunny {
	return &Bunny{
		connDetails: &details,
		consumerMux: &sync.RWMutex{},
	}
}

func (m *Bunny) Connect() error {
	if m.connected {
		return errors.New("connect may only be called once")
	}

	log.Debug("Establishing initial connection to Rabbit...")

	if err := m.connect(); err != nil {
		return fmt.Errorf("initial connection failed: %v", err)
	}

	return nil
}

// internal version of connect is reusable for reconnects
func (m *Bunny) connect() error {
	var conn *amqp.Connection
	var err error

	log.Debugf("dialing rabbit... %d urls to try", len(m.connDetails.URLs))

	for i, url := range m.connDetails.URLs {
		if m.connDetails.UseTLS {
			log.Debugf("Dialling url %d using TLS... verify certificate: %v", i, !m.connDetails.SkipVerifyTLS)
			tlsConfig := &tls.Config{}

			if m.connDetails.SkipVerifyTLS {
				tlsConfig.InsecureSkipVerify = true
			}

			conn, err = amqp.DialTLS(url, tlsConfig)
		} else {
			log.Debugf("Dialling url %d without TLS...", i)
			conn, err = amqp.Dial(url)
		}

		if err == nil {
			log.Debug("connection successful")
			break
		}

		log.Errorf("failed to connect to URL %d: %v", i, err)
	}
	if err != nil {
		return fmt.Errorf("failed to connect to all URLs. Last error: %v", err)
	}

	m.connected = true
	m.conn = conn

	// Watch for closed connection
	// always create a new channel since the old one is closed
	m.notifyClose = m.conn.NotifyClose(make(chan *amqp.Error, 0))
	go m.watchNotifyClose()

	return nil
}

func (m *Bunny) watchNotifyClose() {
	log.Debug("subscribing to connection close notifications")

	watchBeginTime := time.Now()

	// watch for close notification, reconnect, repeat
	closeErr := <-m.notifyClose
	log.Debugf("received message on notify close channel: '%+v' (reconnecting)", closeErr)
	log.Warnf("Detected connection close after %v. Reconnecting...", time.Since(watchBeginTime))

	reconnectBeginTime := time.Now()
	m.reconnect()

	log.Infof("Successfully reconnected after %v", time.Since(reconnectBeginTime))
}

// helper function for testability
func (m *Bunny) reconnect() {
	// Acquire mutex to pause all consumers/producers while we reconnect AND prevent
	// access to the channel map
	// TODO can we still drop messages here?

	var attempts int

	beginTime := time.Now()

	for {
		log.Warnf("Attempting to reconnect. All processing has been blocked for %v", time.Since(beginTime))

		attempts++
		if err := m.connect(); err != nil {
			log.Warnf("failed attempt %d to reconnect: %s; retrying in %d", attempts, err, m.connDetails.RetryReconnectSec)
			time.Sleep(time.Duration(m.connDetails.RetryReconnectSec) * time.Second)
			continue
		}
		log.Debugf("successfully reconnected after %d attempts in %v", attempts, time.Since(beginTime))
		break
	}

	if err := m.restartConsumers(); err != nil {

	}

	return
}

// helper function for testability and use of defers
func (m *Bunny) restartConsumers() error {
	// TODO: does this need to be locked here?
	m.consumerMux.Lock()
	defer m.consumerMux.Unlock()

	for _, consumer := range m.consumers {
		if err := consumer.restart(m.conn); err != nil {
			// TODO: not sure about how we want to handle these
		}
	}

	return nil
}

// Used to declare topology of rabbit via use of amqp functions
type ChannelSetupFunc func(ch *amqp.Channel) error

// Function that is run against evey item delivered from rabbit
//  Author of this function is responsible for calling Delivery.Ack()
type ConsumeFunc func(msg *amqp.Delivery) error

type consumerChan struct {
	amqpChan      *amqp.Channel
	chanSetupFunc ChannelSetupFunc
	consumers     map[string]*consumerDefinition
	consumersMux  *sync.RWMutex
}

type consumerDefinition struct {
	consumeFunc  ConsumeFunc
	consumerTag  string
	opts         *ConsumeOptions
	deliveryChan <-chan amqp.Delivery
	errorChan    chan<- error
	deliveryMux  *sync.RWMutex

	consumerChan *consumerChan
}

type ConsumeOptions struct {
	QueueName string
	AutoAck   bool
	Exclusive bool
	NoWait    bool
}

type Cancelable interface {
	Cancel(noWait bool) error
}

// exported version of consume for the user to kick of consumption
func (c *consumerChan) Consume(consumeFunc ConsumeFunc, opts ConsumeOptions, errs chan<- error) (Cancelable, error) {
	def := &consumerDefinition{
		consumeFunc: consumeFunc,
		opts:        &opts,
		errorChan:   errs,
		deliveryMux: &sync.RWMutex{},
	}

	if err := c.consume(def); err != nil {
		return nil, err
	}

	// do this after consuming begins because if we can not get this lock, all
	//  other operations on the connection will be locked and we could be in total deadlock
	c.consumersMux.Lock()
	defer c.consumersMux.Unlock()
	c.consumers[def.consumerTag] = def

	return def, nil
}

// internal consume that is reusable for restarts
func (c *consumerChan) consume(def *consumerDefinition) error {

	// save the consumer channel to allow cancelling
	def.consumerChan = c

	// generate a consumer tag to ensure uniqueness
	id, err := uuid.NewRandom()
	if err != nil {
		return fmt.Errorf("failed to generate consumer tag: %v", err)
	}

	def.consumerTag = id.String()

	log.Debugf("setting up to consume from queue %s with consumer tag %s...", def.opts.QueueName, id.String())

	deliveries, err := c.amqpChan.Consume(
		def.opts.QueueName,
		def.consumerTag,
		def.opts.AutoAck,
		def.opts.Exclusive,
		false, // noLocal is not supported by Rabbit
		def.opts.NoWait,
		nil,
	)
	if err != nil {
		return fmt.Errorf("failed to begin consuming from channel: %v consumer tag: %s", err, id.String())
	}

	log.Debugf("kicking off consumer func for consumer tag %s", id.String())

	// must lock here because in the restart case, something may be using it
	def.deliveryMux.Lock()
	def.deliveryChan = deliveries
	def.deliveryMux.Unlock()

	// TODO: take code from Rabbit lib
	go func() {
		for {
			item, ok := <-def.deliveries()
			if !ok {
				log.Infof("got delivery channel close! consumer tag: %s", def.consumerTag)
				return
			}

			if err := def.consumeFunc(&item); err != nil {
				log.Debugf("error during consume: %s", err)
				if def.errorChan != nil {
					def.errorChan <- err
				}
			}
		}
	}()

	log.Debugf("consuming from queue %s with consumer tag ", def.opts.QueueName, id.String())

	return nil
}

func (c *consumerDefinition) Cancel(noWait bool) error {
	if err := c.consumerChan.amqpChan.Cancel(c.consumerTag, noWait); err != nil {
		return err
	}

	// remove itself from the list of consumers
	c.consumerChan.consumersMux.Lock()
	defer c.consumerChan.consumersMux.Unlock()
	delete(c.consumerChan.consumers, c.consumerTag)

	return nil
}

func (c *consumerDefinition) deliveries() <-chan amqp.Delivery {
	c.deliveryMux.RLock()
	defer c.deliveryMux.RUnlock()
	return c.deliveryChan
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

// TODO: add a general topology definition function

func (m *Bunny) NewConsumerChannel(setupFunc ChannelSetupFunc) (*consumerChan, error) {
	// create our representation of the chan to store
	cc := &consumerChan{
		chanSetupFunc: setupFunc,
		consumers:     map[string]*consumerDefinition{},
		consumersMux:  &sync.RWMutex{},
	}

	if err := establishConsumerChan(cc, m.conn); err != nil {
		return nil, err
	}

	// append it safely
	m.consumerMux.Lock()
	defer m.consumerMux.Unlock()
	m.consumers = append(m.consumers, cc)

	log.Debug("new channel created")
	return cc, nil
}

// A helper to establish the consumer channel. A shared implementation used in connect and reconnect
func establishConsumerChan(cc *consumerChan, conn *amqp.Connection) error {
	log.Debug("establishing a channel...")
	// establish a channel
	ch, err := conn.Channel()
	if err != nil {
		return fmt.Errorf("failed to initialize channel: %v", err)
	}

	// This should be safe because we would not be establishing a chan if
	//  it is in active use
	cc.amqpChan = ch

	log.Debug("running channel topology setup func...")
	// run user provided topology setup
	if err := cc.chanSetupFunc(ch); err != nil {
		return err
	}

	return nil
}
