package bunny

import (
	"errors"
	"fmt"

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

	connections *connPool

	notifyClose chan *amqp.Error

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
	}
}

func (b *Bunny) Connect() error {
	if b.connectCalled {
		return errors.New("connect may only be called once")
	}

	log.Info("Establishing initial connection to Rabbit...")

	pool, err := newPool(b.connDetails)
	if err != nil {
		return fmt.Errorf("initial connection failed: %v", err)
	}

	b.connections = pool

	log.Info("Connection established")

	return nil
}

// A one time declaration of overall rabbit topology to start
// Very important that this is idempotent. It will get called on restarts
func (b *Bunny) DeclareTopology(setup SetupFunc) error {
	return b.connections.declareInitialTopology(setup)
}
