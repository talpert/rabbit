package bunny

import (
	"crypto/tls"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/streadway/amqp"
)

const (
	maxChannelsPerConnection = 1
	rebalanceInterval        = time.Minute * 5
)

type connPool struct {
	conns map[string]*connection
	// id of the connection in the pool which should be used
	//  for creating new channels. getNext() will use this
	//  Maybe this could be a pointer to the conn but using
	//  id for now since you would have to lock anyway
	currentID string
	// locked during conn creation process to avoid other threads
	//  from creating more connections
	connPoolMux *sync.Mutex

	// these are used when creating new connections
	details     *ConnectionDetails
	topologyDef SetupFunc
}

type connection struct {
	id          string
	amqpConn    *amqp.Connection
	details     *ConnectionDetails
	topologyDef SetupFunc
	// locked when connection is busy and it should not be used
	connMux *sync.Mutex

	consumers   map[string]*consumer
	consumerMux *sync.RWMutex

	// TODO producers

	notifyClose chan *amqp.Error

	rmCallback func(string)
}

func newPool(details *ConnectionDetails) (*connPool, error) {
	cp := &connPool{
		conns:       map[string]*connection{},
		connPoolMux: &sync.Mutex{},
		details:     details,
	}

	// lock the connection mutex for the entirety of this process to avoid
	//  more than one creation from happening at once
	cp.connPoolMux.Lock()
	defer cp.connPoolMux.Unlock()
	_, err := cp.newConnection()
	if err != nil {
		return nil, fmt.Errorf("failed to connect to rabbitmq: %v", err)
	}

	// kick off rebalancing every 5 minutes
	go cp.startRebalancing(rebalanceInterval)

	return cp, nil
}

// a one time declaration of overall rabbit topology to start
func (c *connPool) declareInitialTopology(setup SetupFunc) error {
	if c.topologyDef != nil {
		return errors.New("can not declare main topology more than once")
	}

	c.topologyDef = setup

	conn, err := c.getNext()
	if err != nil {
		return fmt.Errorf("error declaring initial topology: %v", err)
	}

	conn.topologyDef = setup

	if err := conn.declareTopology(); err != nil {
		return err
	}

	return nil
}

// a helper that is reusable for restarts
func (c *connection) declareTopology() error {
	ch, err := c.amqpConn.Channel()
	if err != nil {
		return fmt.Errorf("failed to create channel to define topology: %v", err)
	}

	if err := c.topologyDef(ch); err != nil {
		return err
	}

	if err := ch.Close(); err != nil {
		// log this but allow execution to continue
		log.Errorf("failed to close channel for topology declaration: %v", err)
	}

	return nil
}

// always lock the mutex before calling this method
func (c *connPool) newConnection() (*connection, error) {
	log.Debug("Creating new connection...")
	id, err := uuid.NewRandom()
	if err != nil {
		return nil, fmt.Errorf("failed to generate ID for connection: %v", err)
	}

	conn := &connection{
		id:          id.String(),
		details:     c.details,
		topologyDef: c.topologyDef,
		connMux:     &sync.Mutex{},

		consumers:   map[string]*consumer{},
		consumerMux: &sync.RWMutex{},

		rmCallback: c.deleteConnection,
	}

	if err := conn.connect(); err != nil {
		return nil, err
	}

	// this is safe because this whole method is wrapped in a lock
	c.conns[conn.id] = conn
	c.currentID = conn.id

	return conn, nil
}

func (c *connection) connect() error {
	var amqpConn *amqp.Connection
	var err error

	log.Debugf("dialing rabbit... %d urls to try", len(c.details.URLs))

	for i, url := range c.details.URLs {
		if c.details.UseTLS {
			log.Debugf("Dialling url %d using TLS... verify certificate: %v", i, !c.details.SkipVerifyTLS)
			tlsConfig := &tls.Config{}

			if c.details.SkipVerifyTLS {
				tlsConfig.InsecureSkipVerify = true
			}

			amqpConn, err = amqp.DialTLS(url, tlsConfig)
		} else {
			log.Debugf("Dialling url %d without TLS...", i)
			amqpConn, err = amqp.Dial(url)
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

	// save the connection
	c.amqpConn = amqpConn

	// Watch for closed connection
	c.notifyClose = c.amqpConn.NotifyClose(make(chan *amqp.Error))
	go c.watchNotifyClose()

	return nil
}

func (c *connPool) getNext() (*connection, error) {
	// check for nil or no connection
	if len(c.conns) < 1 {
		return nil, fmt.Errorf("no active connections found must connect() first")
	}

	// Grab the latest connection for now
	// TODO: do something better here to reuse older connections that have capacity
	// Lock the connection mutex for the entirety of this process to avoid
	//  more than one creation from happening at once
	c.connPoolMux.Lock()
	defer c.connPoolMux.Unlock()
	con, ok := c.conns[c.currentID]

	// check that this connection exists and see that there is capacity on it
	// TODO: implement producers
	if ok && con.numConsumers() < maxChannelsPerConnection {
		return con, nil
	}

	log.Debugf("reached max number of channels per connection: %d, starting new...", maxChannelsPerConnection)

	// safe to create here because we still have the lock
	newCon, err := c.newConnection()
	if err != nil {
		return nil, fmt.Errorf("need new connection but failed to create: %v", err)
	}

	return newCon, nil
}

func (c *connPool) startRebalancing(interval time.Duration) {
	for {
		<-time.Tick(interval)
		c.rebalance()
	}
}

// This will "rebalance" the connections by choosing which connection gets more channels next
// Naive, very basic, and potentially expensive way of doing this but ok for an MVP as long as
// it doesn't run very often
func (c *connPool) rebalance() {
	c.connPoolMux.Lock()
	defer c.connPoolMux.Unlock()

	log.Debugf("Rebalancing connection pool... size: %d", len(c.conns))

	var (
		min    = maxChannelsPerConnection
		chosen string
	)

	// loop through all the connections and find the one with least consumers
	for _, conn := range c.conns {
		n := conn.numConsumers()
		if n < min {
			min = n
			chosen = conn.id
		}
	}

	// if one was chosen, set it
	if chosen != "" {
		c.currentID = chosen
	}
}

func (c *connPool) deleteConnection(id string) {
	log.Debugf("Removing connection %s from pool...", id)

	c.connPoolMux.Lock()
	defer c.connPoolMux.Unlock()

	delete(c.conns, id)
}

/*-----------
| Consumers |
-----------*/

// A helper to establish the consumer channel. A shared implementation used in connect and reconnect
func (c *connPool) establishConsumerChan(consumer *consumer) error {
	conn, err := c.getNext()
	if err != nil {
		return fmt.Errorf("cannot establish consumer channel: %v", err)
	}

	log.Debug("establishing a channel...")

	// establish a channel
	ch, err := conn.amqpConn.Channel()
	if err != nil {
		return fmt.Errorf("failed to initialize channel: %v", err)
	}

	// register the consumer
	conn.registerConsumer(consumer)

	// This should be safe because we would not be establishing a chan if
	//  it is in active use
	consumer.amqpChan = ch
	consumer.rmCallback = conn.deleteConsumer

	log.Debug("running channel topology setup func...")
	// run user provided topology setup
	if err := consumer.chanSetupFunc(ch); err != nil {
		return err
	}

	return nil
}

func (c *connection) numConsumers() int {
	c.consumerMux.RLock()
	defer c.consumerMux.RUnlock()
	return len(c.consumers)
}

func (c *connection) registerConsumer(consumer *consumer) {
	c.consumerMux.Lock()
	defer c.consumerMux.Unlock()

	c.consumers[consumer.id] = consumer
}

func (c *connection) deleteConsumer(id string) {
	log.Debugf("Removing consumer %s from connection...", id)
	c.consumerMux.Lock()
	defer c.consumerMux.Unlock()

	delete(c.consumers, id)
}

/*-----------------
| Reconnect Logic |
-----------------*/

func (c *connection) watchNotifyClose() {
	log.Debug("subscribing to connection close notifications")

	watchBeginTime := time.Now()

	// watch for close notification, reconnect, repeat
	closeErr := <-c.notifyClose
	log.Debugf("received message on notify close channel: '%+v' (reconnecting)", closeErr)
	log.Warnf("Detected connection close after %v. Reconnecting...", time.Since(watchBeginTime))

	// this will block until connection is established and restart is complete
	c.restart()
}

func (c *connection) restart() {
	// first reconnect
	reconnectBeginTime := time.Now()

	if c.numConsumers() < 1 {
		log.Infof("Connection %s has no consumers. Not restarting connection...", c.id)
		// remove it from the pool
		c.rmCallback(c.id)

		return
	}

	c.reconnect()

	// if there is a topology definition, redeclare it
	if c.topologyDef != nil {
		if err := c.declareTopology(); err != nil {
			log.Errorf("could not declare topology on restart: %v", err)
			// TODO: handle this better (see comment below)
			// For now just allow execution to continue because the redeclare should be idempotent
			// If there were things declared as auto-delete, this will be a problem
		}
	}

	if err := c.restartConsumers(); err != nil {
		log.Errorf("Failed to restart consumers! %v", err)
		// TODO: handle this better (see comment below)
	}

	// TODO: maybe if there are errors on restart, we disconnect and try again.
	//  Log lots of errors so someone will notice? Maybe we offer a "fatal chan" so
	//  we can let the user know that shit has hit the fan... Or maybe we give the
	//  user an option what we do. Log, err chan? Panic is a bad choice generally,
	//  but even worse in this case because it's probably running in a goroutine, so
	//  we would just die and no one would know about it.

	log.Infof("Successfully reconnected after %v", time.Since(reconnectBeginTime))
	return
}

// helper function for testability
func (c *connection) reconnect() {
	// lock so that other functions can not attempt to use the connection
	c.connMux.Lock()
	defer c.connMux.Unlock()

	// TODO will we still drop messages here?

	var attempts int

	beginTime := time.Now()

	// TODO: implement exponential backoff
	for {
		log.Warnf("Attempting to reconnect. All processing has been blocked for %v", time.Since(beginTime))

		attempts++
		if err := c.connect(); err != nil {
			log.Warnf("failed attempt %d to reconnect: %s; retrying in %d seconds", attempts, err, c.details.RetryReconnectSec)
			time.Sleep(time.Duration(c.details.RetryReconnectSec) * time.Second)
			continue
		}
		log.Debugf("successfully reconnected after %d attempts in %v", attempts, time.Since(beginTime))
		break
	}

	return
}

// helper function for testability and use of defers
func (c *connection) restartConsumers() error {
	//  Lock this so that we do not add more while restarting
	c.consumerMux.Lock()
	defer c.consumerMux.Unlock()

	// quick and dirty error aggregation
	var errs []string

	// TODO: implement some kind of rate limiting. Maybe something global, not just restarts
	for _, consumer := range c.consumers {
		// Do not restart channels that have been cancelled. This avoids a race condition where
		//  the consumer ended but had not been deleted before a restart was triggered.
		if consumer.getStatus() == statusCancelled {
			log.Debugf("Consumer %s is cancelled so not restarting", consumer.id)
			continue
		}

		// create the exclusive channel that is associated with this consumer
		ch, err := c.amqpConn.Channel()
		if err != nil {
			log.Errorf("Failed to create a new consumer channel: %v", err)
			// TODO: need to figure this out too
			errs = append(errs, err.Error())
		}

		// set new channel
		consumer.amqpChan = ch

		if err := consumer.restart(); err != nil {
			log.Errorf("Failed to restart consumer %s: %v", consumer.id, err)

			// need to close the channel we created for this consumer
			if err := ch.Close(); err != nil {
				// Unfortunately this is the best we can do. Probably a lot of bad things are already happening
				//  if this fails and someone will know.
				log.Errorf("Attempted to close unused channel after failed restart but got error: %v", err)
			}

			// TODO: not sure about how we want to handle these...
			//  restart as many as we can, or fail fast when one errors?
			errs = append(errs, err.Error())
		}
	}

	// aggregate errors for now and do this better in the future
	if len(errs) > 0 {
		return fmt.Errorf("failures during consumer restarts: %v", strings.Join(errs, ", "))
	}

	return nil
}
