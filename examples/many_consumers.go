package main

import (
	"fmt"
	"log"
	"math/rand"
	"sync"
	"time"

	"github.com/batchcorp/rabbit/bunny"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
	"github.com/streadway/amqp"
)

const rabbitURI = "amqp://guest:guest@localhost:5672"

func init() {
	logrus.SetLevel(logrus.DebugLevel)
	rand.Seed(time.Now().Unix())
}

func main() {
	bunn := bunny.NewBunny(bunny.ConnectionDetails{
		URLs:              []string{rabbitURI},
		RetryReconnectSec: 5,
		UseTLS:            false,
	})

	bunny.SeLogger(logrus.StandardLogger().WithField("bunny", true))

	if err := bunn.Connect(); err != nil {
		log.Fatal(err)
	}

	if err := bunn.DeclareTopology(myTopology); err != nil {
		log.Fatal(err)
	}

	consumerOne, err := bunn.NewConsumerChannel(setupQueue(firstQueue, firstKey))
	if err != nil {
		log.Fatal(err)
	}

	consumerTwo, err := bunn.NewConsumerChannel(setupQueue(secondQueue, secondKey))
	if err != nil {
		log.Fatal(err)
	}

	errs := make(chan error)
	go func() {
		for {
			logrus.Errorf("got error: %v", <-errs)
		}
	}()

	if _, err := consumerOne.Consume(workFunc(firstQueue), bunny.ConsumeOptions{
		QueueName: firstQueue,
		AutoAck:   true,
		Exclusive: false,
	}, errs); err != nil {
		log.Fatal(err)
	}

	if _, err := consumerTwo.Consume(workFunc(secondQueue), bunny.ConsumeOptions{
		QueueName: secondQueue,
		AutoAck:   true,
		Exclusive: false,
	}, errs); err != nil {
		log.Fatal(err)
	}

	// start publishing
	go publishMessages()

	// sleep for 15 sec. Kill the conn while this is sleeping
	time.Sleep(time.Second * 20)

	consumerThree, err := bunn.NewConsumerChannel(setupQueue(thirdQueue, thirdKey))
	if err != nil {
		log.Fatal(err)
	}

	if _, err := consumerThree.Consume(workFunc(thirdQueue), bunny.ConsumeOptions{
		QueueName: thirdQueue,
		AutoAck:   true,
		Exclusive: false,
	}, errs); err != nil {
		log.Fatal(err)
	}

	time.Sleep(time.Second * 20)
	logrus.Info("cancelled second consumer")

	if err := consumerTwo.Cancel(false); err != nil {
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
	thirdQueue   = "third"
	thirdKey     = "key-third"
)

var routingKeys = []string{
	firstKey,
	secondKey,
	thirdKey,
}

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

	return nil
}

func setupQueue(queueName, routingKey string) bunny.SetupFunc {
	return func(ch *amqp.Channel) error {
		q, err := ch.QueueDeclare(
			queueName, // name
			true,      // durable
			true,      // autoDelete
			true,      // exclusive
			false,     // noWait
			nil,       // args
		)
		if err != nil {
			return fmt.Errorf("failed to declare first queue: %v", err)
		}

		if err := ch.QueueBind(
			q.Name,       // name
			routingKey,   // key
			testExchange, // exchange
			false,        // noWait
			nil,          // args
		); err != nil {
			return fmt.Errorf("failed to bind queue to exchange %s: %v", testExchange, err)
		}

		return nil
	}
}

func workFunc(queue string) bunny.ConsumeFunc {
	return func(item *amqp.Delivery) error {
		logrus.Infof("got new delivery on queue: %s, body: %s", queue, item.Body)
		return nil
	}
}

func publishMessages() {
	for {
		conn, ch, err := connect(rabbitURI)
		if err != nil {
			// don't publish, just wait and try again
			time.Sleep(time.Second * 5)
			continue
		}

		if err := publish(ch, testExchange, routingKeys[rand.Intn(len(routingKeys))],
			fmt.Sprintf("message %s", uuid.New().String())); err != nil {

			logrus.Errorf("failed to publish: %v", err)
		}

		if err := conn.Close(); err != nil {
			logrus.Errorf("failed to close publish conn: %v", err)
		}

		// publish every few sec
		time.Sleep(time.Second * 3)
	}
}

func connect(uri string) (*amqp.Connection, *amqp.Channel, error) {
	conn, err := amqp.Dial(uri)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to dial event bus: %v", err)
	}

	ch, err := conn.Channel()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to establish channel: %v", err)
	}

	return conn, ch, nil
}

func publish(ch *amqp.Channel, exchange, routingKey, message string) error {
	if err := ch.Publish(
		exchange,   // exchange
		routingKey, // routing key
		false,      // mandatory
		false,      // immediate
		amqp.Publishing{
			ContentType: "text/plain",
			Body:        []byte(message),
		},
	); err != nil {
		return err
	}

	return nil
}
