package bunny

import (
	"crypto/tls"
	"errors"
	"fmt"
	"sync"
	"time"

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
	// Naive bool to track if connect has been called. This prevents a user from
	//  calling connect more than once but does not guarantee connection status
	connectCalled bool

	amqpConn *amqp.Connection
	connMux  *sync.RWMutex

	notifyClose chan *amqp.Error

	topologyDef SetupFunc

	consumers   map[string]*consumer
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

// Used to declare topology of rabbit via use of amqp functions
type SetupFunc func(ch *amqp.Channel) error

func NewBunny(details ConnectionDetails) *Bunny {
	return &Bunny{
		connDetails: &details,
		connMux:     &sync.RWMutex{},
		consumers:   map[string]*consumer{},
		consumerMux: &sync.RWMutex{},
	}
}

func (b *Bunny) Connect() error {
	if b.connectCalled {
		return errors.New("connect may only be called once")
	}

	log.Info("Establishing initial connection to Rabbit...")

	if err := b.connect(); err != nil {
		return fmt.Errorf("initial connection failed: %v", err)
	}

	log.Info("Connection established")

	return nil
}

// internal version of connect is reusable for reconnects
func (b *Bunny) connect() error {
	var conn *amqp.Connection
	var err error

	log.Debugf("dialing rabbit... %d urls to try", len(b.connDetails.URLs))

	for i, url := range b.connDetails.URLs {
		if b.connDetails.UseTLS {
			log.Debugf("Dialling url %d using TLS... verify certificate: %v", i, !b.connDetails.SkipVerifyTLS)
			tlsConfig := &tls.Config{}

			if b.connDetails.SkipVerifyTLS {
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

	b.connectCalled = true
	b.amqpConn = conn

	// Watch for closed connection
	// always create a new channel since the old one is closed
	b.notifyClose = conn.NotifyClose(make(chan *amqp.Error, 0))
	go b.watchNotifyClose()

	return nil
}

// a one time declaration of overall rabbit topology to start
func (b *Bunny) DeclareTopology(setup SetupFunc) error {
	if b.topologyDef != nil {
		return errors.New("can not declare main topology more than once")
	}

	b.topologyDef = setup

	if err := b.declareTopology(); err != nil {
		return err
	}

	return nil
}

// a helper that is reusable for restarts
func (b *Bunny) declareTopology() error {
	ch, err := b.conn().Channel()
	if err != nil {
		return fmt.Errorf("failed to create channel to define topology: %v", err)
	}

	if err := b.topologyDef(ch); err != nil {
		return err
	}

	if err := ch.Close(); err != nil {
		// log this but allow execution to continue
		log.Errorf("failed to close channel for topology declaration: %v", err)
	}

	return nil
}

func (b *Bunny) conn() *amqp.Connection {
	b.connMux.RLock()
	defer b.connMux.RUnlock()
	return b.amqpConn
}

func (b *Bunny) deleteConsumer(id string) {
	b.consumerMux.Lock()
	defer b.consumerMux.Unlock()

	delete(b.consumers, id)
}

/*-----------------
| Reconnect Logic |
-----------------*/

func (b *Bunny) watchNotifyClose() {
	log.Debug("subscribing to connection close notifications")

	watchBeginTime := time.Now()

	// watch for close notification, reconnect, repeat
	closeErr := <-b.notifyClose
	log.Debugf("received message on notify close channel: '%+v' (reconnecting)", closeErr)
	log.Warnf("Detected connection close after %v. Reconnecting...", time.Since(watchBeginTime))

	// this will block until connection is established and restart is complete
	b.restart()
}

func (b *Bunny) restart() {
	// first reconnect
	reconnectBeginTime := time.Now()
	b.reconnect()

	// redeclare topology
	if err := b.declareTopology(); err != nil {
		log.Errorf("could not declare topology on restart: %v", err)
		// TODO: handle this better
		// For now just allow execution to continue because the redeclare should be idempotent
		// If there were things declared as auto-delete, this will be a problem
	}

	if err := b.restartConsumers(); err != nil {
		// TODO: handle this better
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
func (b *Bunny) reconnect() {
	// lock so that other functions can not attempt to use the connection
	b.connMux.Lock()
	defer b.connMux.Unlock()

	// TODO will we still drop messages here?

	var attempts int

	beginTime := time.Now()

	for {
		log.Warnf("Attempting to reconnect. All processing has been blocked for %v", time.Since(beginTime))

		attempts++
		if err := b.connect(); err != nil {
			log.Warnf("failed attempt %d to reconnect: %s; retrying in %d seconds", attempts, err, b.connDetails.RetryReconnectSec)
			time.Sleep(time.Duration(b.connDetails.RetryReconnectSec) * time.Second)
			continue
		}
		log.Debugf("successfully reconnected after %d attempts in %v", attempts, time.Since(beginTime))
		break
	}

	return
}

// helper function for testability and use of defers
func (b *Bunny) restartConsumers() error {
	// TODO: does this need to be locked here?
	//  yes? so that we do not add more while restarting?
	//  maybe we should use a getter with a read lock?
	b.consumerMux.Lock()
	defer b.consumerMux.Unlock()

	for _, consumer := range b.consumers {
		if err := consumer.restart(b.conn()); err != nil {
			// TODO: not sure about how we want to handle these
		}
	}

	return nil
}
