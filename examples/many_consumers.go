package main

import (
	"fmt"
	"log"
	"sync"

	"github.com/batchcorp/rabbit/bunny"
	"github.com/sirupsen/logrus"
	"github.com/streadway/amqp"
)

func init() {
	logrus.SetLevel(logrus.DebugLevel)
}

func main() {
	bunn := bunny.NewBunny(bunny.ConnectionDetails{
		URLs:              []string{"amqp://guest:guest@localhost:5672"},
		RetryReconnectSec: 15,
		UseTLS:            false,
	})

	bunny.SeLogger(logrus.StandardLogger().WithField("bunny", true))

	if err := bunn.Connect(); err != nil {
		log.Fatal(err)
	}

	consumerChan, err := bunn.NewConsumerChannel(myTopology)
	if err != nil {
		log.Fatal(err)
	}

	errs := make(chan error)
	go func() {
		for {
			logrus.Errorf("got error: %v", <-errs)
		}
	}()

	if _, err := consumerChan.Consume(doWork, bunny.ConsumeOptions{
		QueueName: firstQueue,
		AutoAck:   true,
		Exclusive: false,
	}, errs); err != nil {
		log.Fatal(err)
	}

	if _, err := consumerChan.Consume(doWork, bunny.ConsumeOptions{
		QueueName: secondQueue,
		AutoAck:   true,
		Exclusive: false,
	}, errs); err != nil {
		log.Fatal(err)
	}

	wg := &sync.WaitGroup{}
	wg.Add(1)
	wg.Wait()
}

const (
	testExchange = "test-main"
	firstQueue   = "first"
	firstKey     = "key-first"
	secondQueue  = "second"
	secondKey    = "key-second"
)

func myTopology(ch *amqp.Channel) error {
	if err := ch.ExchangeDeclare(
		testExchange,        // name
		amqp.ExchangeDirect, // kind
		true,                // durable
		true,                // autoDelete
		false,               // internal
		false,               // noWait
		nil,                 // args
	); err != nil {
		return fmt.Errorf("failed to declare %s exchange: %v", testExchange, err)
	}

	q, err := ch.QueueDeclare(
		firstQueue, // name
		true,       // durable
		true,       // autoDelete
		true,       // exclusive
		false,      // noWait
		nil,        // args
	)
	if err != nil {
		return fmt.Errorf("failed to declare first queue: %v", err)
	}

	if err := ch.QueueBind(
		q.Name,       // name
		firstKey,     // key
		testExchange, // exchange
		false,        // noWait
		nil,          // args
	); err != nil {
		return fmt.Errorf("failed to bind queue to exchange %s: %v", testExchange, err)
	}

	q2, err := ch.QueueDeclare(
		secondQueue, // name
		true,        // durable
		true,        // autoDelete
		true,        // exclusive
		false,       // noWait
		nil,         // args
	)
	if err != nil {
		return fmt.Errorf("failed to declare first queue: %v", err)
	}

	if err := ch.QueueBind(
		q2.Name,      // name
		secondKey,    // key
		testExchange, // exchange
		false,        // noWait
		nil,          // args
	); err != nil {
		return fmt.Errorf("failed to bind queue to exchange %s: %v", testExchange, err)
	}

	return nil
}

func doWork(item *amqp.Delivery) error {
	logrus.Infof("got item %+v message: %s", item, item.Body)
	return nil
}
