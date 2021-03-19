package bunny

import (
	"errors"
	"runtime"
	"sync"
	"testing"
	"time"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/streadway/amqp"

	"github.com/batchcorp/rabbit/bunny/fakes"
)

/*-----------------------
| Connection Pool Tests |
-----------------------*/

func Test_newPool(t *testing.T) {
	Convey("newPool", t, func() {
		fakeDialer := &fakeDialer{}
		fakeDialer.DialReturns(&connectionWrapper{&amqp.Connection{}}, nil)

		cd := &ConnectionDetails{
			URLs:                     []string{"foobar"},
			dialer:                   fakeDialer,
			maxChannelsPerConnection: maxChannelsPerConnection,
		}

		Convey("creates a new connection and returns the pool", func() {
			before := runtime.NumGoroutine()
			pool, err := newPool(cd)
			after := runtime.NumGoroutine()

			So(err, ShouldBeNil)
			So(len(pool.conns), ShouldEqual, 1)
			So(pool.connPoolMux, ShouldNotBeNil)
			So(pool.details, ShouldEqual, cd)
			// Rebalancing should have kicked off
			//  not the best way to test this but will do for now
			// TODO: maybe capture logs and test for debug logging
			//  would also need to make the interval configurable
			So(after, ShouldBeGreaterThan, before)
		})

		Convey("errors if new connection fails", func() {
			cd.URLs = nil
			_, err := newPool(cd)
			So(err, ShouldNotBeNil)
			So(err.Error(), ShouldContainSubstring, "no AMQP URLs were supplied")
		})
	})
}

func Test_connPool_declareInitialTopology(t *testing.T) {
	Convey("declareInitialTopology", t, func() {
		fakeDialer := &fakeDialer{}
		fakeDialer.DialReturns(&connectionWrapper{&amqp.Connection{}}, nil)

		cd := &ConnectionDetails{
			URLs:                     []string{"foobar"},
			dialer:                   fakeDialer,
			maxChannelsPerConnection: maxChannelsPerConnection,
		}

		pool, err := newPool(cd)
		So(err, ShouldBeNil)

		var conn *connection
		// just grab the first one
		for _, conn = range pool.conns {
			break
		}

		// mock the connection
		fakeConnection := &FakeAmqpConnection{}
		fakeChannel := &fakes.FakeAmqpChannel{}
		fakeConnection.ChannelReturns(fakeChannel, nil)
		conn.amqpConn = fakeConnection

		Convey("gets next available connection and declares the topology", func() {
			called := false
			setup := func(*amqp.Channel) error { called = true; return nil }

			err := pool.declareInitialTopology(setup)
			So(err, ShouldBeNil)
			So(called, ShouldBeTrue)
		})

		Convey("errors if called twice", func() {
			err := pool.declareInitialTopology(func(*amqp.Channel) error { return nil })
			So(err, ShouldBeNil)

			err = pool.declareInitialTopology(func(*amqp.Channel) error { return nil })
			So(err, ShouldNotBeNil)
			So(err.Error(), ShouldContainSubstring, "more than once")
		})

		Convey("errors if connection can not be obtained", func() {
			pool.conns = map[string]*connection{}
			err := pool.declareInitialTopology(func(*amqp.Channel) error { return nil })
			So(err, ShouldNotBeNil)
			So(err.Error(), ShouldContainSubstring, "failed to obtain connection")
		})

		Convey("errors if fail to declare topology", func() {
			err := pool.declareInitialTopology(func(*amqp.Channel) error { return errors.New("kaboom") })
			So(err, ShouldNotBeNil)
			So(err.Error(), ShouldContainSubstring, "kaboom")
		})
	})
}

func Test_connPool_newConnection(t *testing.T) {
	Convey("newConnection", t, func() {
		fakeDialer := &fakeDialer{}
		fakeDialer.DialReturns(&connectionWrapper{&amqp.Connection{}}, nil)

		cd := &ConnectionDetails{
			URLs:                     []string{"foobar"},
			dialer:                   fakeDialer,
			maxChannelsPerConnection: maxChannelsPerConnection,
		}

		pool, err := newPool(cd)
		So(err, ShouldBeNil)

		Convey("new connection is created and returned", func() {
			conn, err := pool.newConnection()
			So(err, ShouldBeNil)
			So(conn, ShouldNotBeNil)
			So(conn.details, ShouldEqual, pool.details)
			So(conn.topologyDef, ShouldEqual, pool.topologyDef)
			So(conn.connMux, ShouldNotBeNil)
			So(conn.consumers, ShouldNotBeNil)
			So(conn.consumerMux, ShouldNotBeNil)
			So(conn.rmCallback, ShouldEqual, pool.deleteConnection)

			// appends to pool
			So(pool.conns, ShouldContainKey, conn.id)
			So(pool.conns[conn.id], ShouldEqual, conn)
			So(pool.currentID, ShouldEqual, conn.id)
		})

		Convey("errors connect fails", func() {
			fakeDialer.DialReturns(nil, errors.New("failed dial"))

			_, err := pool.newConnection()
			So(err, ShouldNotBeNil)
			So(err.Error(), ShouldContainSubstring, "failed dial")
		})
	})
}

func Test_connPool_getNext(t *testing.T) {
	Convey("getNext()", t, func() {
		fakeDialer := &fakeDialer{}
		fakeDialer.DialReturns(&connectionWrapper{&amqp.Connection{}}, nil)

		cd := &ConnectionDetails{
			URLs:                     []string{"foobar"},
			dialer:                   fakeDialer,
			maxChannelsPerConnection: maxChannelsPerConnection,
		}

		pool, err := newPool(cd)
		So(err, ShouldBeNil)

		var connID string
		// just grab the first one
		for connID, _ = range pool.conns {
			break
		}

		Convey("returns the connection if there is only one", func() {
			conn, err := pool.getNext()
			So(err, ShouldBeNil)
			So(conn.id, ShouldEqual, connID)
		})

		Convey("returns the latest one if there is more than one", func() {
			// add a new connection
			newConn, err := pool.newConnection()
			So(err, ShouldBeNil)

			conn, err := pool.getNext()
			So(err, ShouldBeNil)
			So(conn.id, ShouldEqual, newConn.id)
		})

		Convey("errors if there are no connections", func() {
			// wipe it out
			pool.conns = map[string]*connection{}

			_, err := pool.getNext()
			So(err, ShouldNotBeNil)
			So(err.Error(), ShouldContainSubstring, "no active connections")
		})

		Convey("locks the mutex", func() {
			fakeMux := &fakes.FakeLocker{}
			pool.connPoolMux = fakeMux

			conn, err := pool.getNext()
			So(err, ShouldBeNil)
			So(conn, ShouldNotBeNil)
			So(fakeMux.LockCallCount(), ShouldEqual, 1)
			So(fakeMux.UnlockCallCount(), ShouldEqual, 1)
		})

		Convey("creates a new connection if consumer is at max capacity", func() {
			pool.details.maxChannelsPerConnection = 1
			// put a dummy consumer in there
			pool.conns[connID].consumers["foo"] = &consumer{}

			conn, err := pool.getNext()
			So(err, ShouldBeNil)
			So(conn, ShouldNotEqual, pool.conns[connID])
			So(len(pool.conns), ShouldEqual, 2)
		})

		Convey("errors if new connection fails", func() {
			pool.details.maxChannelsPerConnection = 1
			// put a dummy consumer in there
			pool.conns[connID].consumers["foo"] = &consumer{}
			fakeDialer.DialReturns(nil, errors.New("failed connection"))

			_, err := pool.getNext()
			So(err, ShouldNotBeNil)
			So(err.Error(), ShouldContainSubstring, "failed connection")
		})
	})
}

func Test_connPool_establishConsumerChan(t *testing.T) {
	Convey("establishConsumerChan", t, func() {
		fakeDialer := &fakeDialer{}
		fakeDialer.DialReturns(&connectionWrapper{&amqp.Connection{}}, nil)

		cd := &ConnectionDetails{
			URLs:                     []string{"foobar"},
			dialer:                   fakeDialer,
			maxChannelsPerConnection: maxChannelsPerConnection,
		}

		pool, err := newPool(cd)
		So(err, ShouldBeNil)

		var conn *connection

		// just grab the first one
		for _, conn = range pool.conns {
			break
		}

		// mock the connection
		fakeConnection := &FakeAmqpConnection{}
		conn.amqpConn = fakeConnection
		// mock the locker
		fakeRWMux := &fakes.FakeRwLocker{}
		conn.consumerMux = fakeRWMux

		consumerID := "foo"
		consumer := &consumer{
			id:            consumerID,
			chanSetupFunc: func(*amqp.Channel) error { return nil },
			consumeFunc:   nil,
			opts:          nil,
			errorChan:     nil,
			deliveryMux:   &sync.RWMutex{},
		}

		Convey("establishes a new channel for consuming", func() {
			err := pool.establishConsumerChan(consumer)
			So(err, ShouldBeNil)

			// consumer was registered
			So(conn.consumers, ShouldContainKey, consumerID)
			So(conn.consumers[consumerID], ShouldEqual, consumer)
			So(fakeRWMux.LockCallCount(), ShouldEqual, 1)
			So(fakeRWMux.UnlockCallCount(), ShouldEqual, 1)
		})

		Convey("errors if get next fails", func() {
			// wipe out connections, get next will fail
			pool.conns = map[string]*connection{}

			err := pool.establishConsumerChan(consumer)
			So(err, ShouldNotBeNil)
			So(err.Error(), ShouldContainSubstring, "cannot establish consumer channel")
		})

		Convey("errors if channel creation fails", func() {
			fakeConnection.ChannelReturns(nil, errors.New("no channel"))

			err := pool.establishConsumerChan(consumer)
			So(err, ShouldNotBeNil)
			So(err.Error(), ShouldContainSubstring, "no channel")
		})

		Convey("errors if channel setup func fails", func() {
			consumer.chanSetupFunc = func(*amqp.Channel) error { return errors.New("boom") }

			err := pool.establishConsumerChan(consumer)
			So(err, ShouldNotBeNil)
			So(err.Error(), ShouldContainSubstring, "boom")
		})
	})
}

func Test_connPool_rebalance(t *testing.T) {
	Convey("rebalance", t, func() {
		// TODO: implement
	})
}

func Test_connPool_deleteConnection(t *testing.T) {
	Convey("deleteConnection", t, func() {
		fakeMux := &fakes.FakeLocker{}
		pool := &connPool{
			connPoolMux: fakeMux,
			conns:       map[string]*connection{},
		}

		Convey("deletes the connection and locks the mutex", func() {
			id := "foo"
			pool.conns[id] = &connection{}

			pool.deleteConnection(id)
			So(len(pool.conns), ShouldEqual, 0)
			So(pool.conns, ShouldNotBeNil)
			So(fakeMux.LockCallCount(), ShouldEqual, 1)
			So(fakeMux.UnlockCallCount(), ShouldEqual, 1)
		})

		Convey("if not present, do nothing", func() {
			pool.deleteConnection("bar")
			So(len(pool.conns), ShouldEqual, 0)
		})
	})
}

/*------------------
| Connection Tests |
------------------*/

func Test_connection_connect(t *testing.T) {
	Convey("connect", t, func() {
		fakeDialer := &fakeDialer{}
		fakeDialer.DialReturns(&connectionWrapper{&amqp.Connection{}}, nil)

		connDetails := &ConnectionDetails{
			URLs:                     []string{"foobar"},
			dialer:                   fakeDialer,
			maxChannelsPerConnection: maxChannelsPerConnection,
		}

		conn := &connection{
			details: connDetails,
		}

		Convey("dials and establishes a connection", func() {
			err := conn.connect()
			So(err, ShouldBeNil)
			So(fakeDialer.DialCallCount(), ShouldEqual, 1)
			So(conn.amqpConn, ShouldNotBeNil)
		})

		Convey("close notifications are watched", func() {
			before := runtime.NumGoroutine()
			err := conn.connect()
			after := runtime.NumGoroutine()
			So(err, ShouldBeNil)
			So(fakeDialer.DialCallCount(), ShouldEqual, 1)
			So(conn.notifyClose, ShouldNotBeNil)
			// watcher goroutine should be running
			// TODO: check logs or something to see that watcher is running
			So(after, ShouldBeGreaterThan, before)
		})

		Convey("if first URL fails, next one is attempted", func() {
			fakeDialer.DialReturnsOnCall(0, nil, errors.New("failed dial"))
			id := 235 // just to identify the returned connection
			fakeDialer.DialReturnsOnCall(1, &connectionWrapper{&amqp.Connection{Major: id}}, nil)
			connDetails.URLs = []string{"foo", "bar"}

			err := conn.connect()
			So(err, ShouldBeNil)
			So(fakeDialer.DialCallCount(), ShouldEqual, 2)
			So(conn.amqpConn, ShouldNotBeNil)
			ac, ok := conn.amqpConn.(*connectionWrapper)
			So(ok, ShouldBeTrue)
			So(ac.Major, ShouldEqual, id)
		})

		Convey("uses TLS if specified", func() {
			fakeDialer.DialTLSReturns(&connectionWrapper{&amqp.Connection{}}, nil)
			connDetails.UseTLS = true

			err := conn.connect()
			So(err, ShouldBeNil)
			So(fakeDialer.DialTLSCallCount(), ShouldEqual, 1)
			So(fakeDialer.DialCallCount(), ShouldEqual, 0) // did not dial without TLS
		})

		Convey("skips TLS verify if specified", func() {
			fakeDialer.DialTLSReturns(&connectionWrapper{&amqp.Connection{}}, nil)
			connDetails.UseTLS = true
			connDetails.SkipVerifyTLS = true

			err := conn.connect()
			So(err, ShouldBeNil)
			So(fakeDialer.DialTLSCallCount(), ShouldEqual, 1)
			_, cfg := fakeDialer.DialTLSArgsForCall(0)
			So(cfg.InsecureSkipVerify, ShouldBeTrue)
		})

		Convey("errors if no URLs given", func() {
			connDetails.URLs = nil

			err := conn.connect()
			So(err, ShouldNotBeNil)
			So(err.Error(), ShouldContainSubstring, "no AMQP URLs were supplied")
		})

		Convey("errors if all URLs fail", func() {
			fakeDialer.DialReturns(nil, errors.New("failed dial"))

			err := conn.connect()
			So(err, ShouldNotBeNil)
			So(err.Error(), ShouldContainSubstring, "failed dial")
		})
	})
}

func Test_connection_declareTopology(t *testing.T) {
	Convey("declareTopology", t, func() {
		fakeConnection := &FakeAmqpConnection{}
		fakeChannel := &fakes.FakeAmqpChannel{}
		fakeConnection.ChannelReturns(fakeChannel, nil)

		called := false

		conn := &connection{
			amqpConn:    fakeConnection,
			topologyDef: func(*amqp.Channel) error { called = true; return nil },
		}

		Convey("declares the topology", func() {
			err := conn.declareTopology()
			So(err, ShouldBeNil)
			So(called, ShouldBeTrue)
		})

		Convey("errors if fails to obtain channel", func() {
			fakeConnection.ChannelReturns(nil, errors.New("no channel"))
			err := conn.declareTopology()
			So(err, ShouldNotBeNil)
			So(err.Error(), ShouldContainSubstring, "no channel")
		})

		Convey("errors if fails to declare topology", func() {
			conn.topologyDef = func(*amqp.Channel) error { return errors.New("kaboom") }
			err := conn.declareTopology()
			So(err, ShouldNotBeNil)
			So(err.Error(), ShouldContainSubstring, "kaboom")
		})

		Convey("closes the channel when done", func() {
			// can not test this currently
		})
	})
}

func Test_connection_consumerHelpers(t *testing.T) {
	Convey("helpers", t, func() {
		fakeRWLocker := &fakes.FakeRwLocker{}
		conn := &connection{
			consumerMux: fakeRWLocker,
		}

		Convey("numConsumers returns the number and locks mutex", func() {
			conn.consumers = map[string]restartableConsumer{"foo": &consumer{}, "bar": &consumer{}}
			n := conn.numConsumers()
			So(n, ShouldEqual, 2)
			So(fakeRWLocker.RLockCallCount(), ShouldEqual, 1)
			So(fakeRWLocker.RUnlockCallCount(), ShouldEqual, 1)
		})

		Convey("registerConsumers appends the consumer and locks mutex", func() {
			conn.consumers = map[string]restartableConsumer{}
			consumerID := "foobar"
			cons := &consumer{id: consumerID}

			conn.registerConsumer(cons)
			So(len(conn.consumers), ShouldEqual, 1)
			So(fakeRWLocker.LockCallCount(), ShouldEqual, 1)
			So(fakeRWLocker.UnlockCallCount(), ShouldEqual, 1)
			So(conn.consumers, ShouldContainKey, consumerID)
			So(conn.consumers[consumerID], ShouldEqual, cons)
		})

		Convey("deleteConsumer deletes and locks mutex", func() {
			consumerID := "foo"
			conn.consumers = map[string]restartableConsumer{consumerID: &consumer{}, "bar": &consumer{}}

			conn.deleteConsumer(consumerID)
			So(len(conn.consumers), ShouldEqual, 1)
			So(fakeRWLocker.LockCallCount(), ShouldEqual, 1)
			So(fakeRWLocker.UnlockCallCount(), ShouldEqual, 1)
			So(conn.consumers, ShouldNotContainKey, consumerID)
		})
	})
}

/*----------
| Restarts |
----------*/

func Test_connection_watchNotifyClose(t *testing.T) {
	Convey("watchNotifyClose", t, func() {
		fakeDialer := &fakeDialer{}
		fakeDialer.DialReturns(&connectionWrapper{&amqp.Connection{}}, nil)

		connDetails := &ConnectionDetails{
			URLs:                     []string{"foobar"},
			dialer:                   fakeDialer,
			maxChannelsPerConnection: maxChannelsPerConnection,
		}

		closeChan := make(chan *amqp.Error)
		fakeRWMux := &fakes.FakeRwLocker{}
		conn := &connection{
			details:     connDetails,
			notifyClose: closeChan,
			consumerMux: fakeRWMux,
			rmCallback:  func(string) {},
		}

		Convey("reacts to a message on notifyClose channel", func() {
			// because this connection has no consumers, it will not restart itself
			// this behavior is desirable for this test as it allows us to confirm
			//  that restart was called, but we do not have to go through (and mock)
			//  the restart process
			conn.consumers = map[string]restartableConsumer{}

			// this is used as a way to see that the callback was called
			rmCalled := false
			conn.rmCallback = func(string) { rmCalled = true }

			finished := false
			go func() {
				conn.watchNotifyClose()
				finished = true // signifies that watchNotifyClose returned
			}()

			closeChan <- &amqp.Error{}

			time.Sleep(time.Millisecond * 5)
			So(rmCalled, ShouldBeTrue) // restart was called
			So(finished, ShouldBeTrue) // watchNotifyClose returned
		})

		Convey("restarts if notifyClose channel is closed before anything is sent", func() {
			finished := false
			go func() {
				conn.watchNotifyClose()
				finished = true // signifies that watchNotifyClose returned
			}()

			close(closeChan)

			time.Sleep(time.Millisecond * 5)
			So(finished, ShouldBeTrue)
		})
	})
}

func Test_connection_restart(t *testing.T) {
	Convey("restart", t, func() {
		fakeDialer := &fakeDialer{}
		fakeConnection := &FakeAmqpConnection{}
		fakeDialer.DialReturns(fakeConnection, nil)
		fakeConnection.ChannelReturns(&amqp.Channel{}, nil)

		connDetails := &ConnectionDetails{
			URLs:                     []string{"foobar"},
			dialer:                   fakeDialer,
			maxChannelsPerConnection: maxChannelsPerConnection,
		}

		closeChan := make(chan *amqp.Error)
		fakeRWMux := &fakes.FakeRwLocker{}
		fakeMux := &fakes.FakeLocker{}
		fakeConsumer := &fakeRestartableConsumer{}
		conn := &connection{
			details:     connDetails,
			notifyClose: closeChan,
			connMux:     fakeMux,
			consumers:   map[string]restartableConsumer{"foo": fakeConsumer},
			consumerMux: fakeRWMux,
			rmCallback:  func(string) {},
		}

		Convey("restarts the connection", func() {
			conn.restart()
			So(fakeDialer.DialCallCount(), ShouldEqual, 1)
			// TODO: grab successful log message here
		})

		Convey("if there are no consumers, it does not restart", func() {
			conn.consumers = map[string]restartableConsumer{}
			rmCalled := false
			conn.rmCallback = func(string) { rmCalled = true }

			conn.restart()
			So(fakeDialer.DialCallCount(), ShouldEqual, 0)
			// removes itself from the pool
			So(rmCalled, ShouldBeTrue)
		})

		Convey("redeclares topology if defined", func() {
			fakeConnection := &FakeAmqpConnection{}
			fakeChannel := &fakes.FakeAmqpChannel{}
			fakeConnection.ChannelReturns(fakeChannel, nil)
			fakeDialer.DialReturns(fakeConnection, nil)
			conn.amqpConn = fakeConnection
			topologyCalled := false
			conn.topologyDef = func(ch *amqp.Channel) error { topologyCalled = true; return nil }

			conn.restart()
			So(topologyCalled, ShouldBeTrue)
		})

		Convey("restarts the consumers", func() {
			restartCalled := false
			fakeConsumer.restartStub = func(ch amqpChannel) error { restartCalled = true; return nil }
			conn.restart()
			// maybe consumer needs to be an interface so we can stick a mocked on in there
			So(fakeRWMux.LockCallCount(), ShouldEqual, 1)
			So(fakeRWMux.UnlockCallCount(), ShouldEqual, 1)
			So(restartCalled, ShouldBeTrue)
		})
	})
}

func Test_connection_reconnect(t *testing.T) {
	Convey("reconnect", t, func() {
		fakeDialer := &fakeDialer{}
		fakeDialer.DialReturns(&connectionWrapper{&amqp.Connection{}}, nil)

		connDetails := &ConnectionDetails{
			URLs:                     []string{"foobar"},
			dialer:                   fakeDialer,
			maxChannelsPerConnection: maxChannelsPerConnection,
		}

		closeChan := make(chan *amqp.Error)
		fakeMux := &fakes.FakeLocker{}
		conn := &connection{
			details:     connDetails,
			notifyClose: closeChan,
			connMux:     fakeMux,
		}

		Convey("reconnects to rabbit if first try is successful", func() {
			conn.reconnect()
			// mutex is locked
			So(fakeMux.LockCallCount(), ShouldEqual, 1)
			So(fakeMux.UnlockCallCount(), ShouldEqual, 1)
			So(fakeDialer.DialCallCount(), ShouldEqual, 1)
		})

		Convey("will retry connection until successful", func() {
			n := 25
			fakeDialer.DialReturns(nil, errors.New("dial failure"))
			fakeDialer.DialReturnsOnCall(n, &connectionWrapper{&amqp.Connection{}}, nil)

			conn.reconnect()
			So(fakeDialer.DialCallCount(), ShouldEqual, n+1)
		})
	})
}

func Test_connection_restartConsumers(t *testing.T) {
	Convey("restartConsumers", t, func() {
		fakeConnection := &FakeAmqpConnection{}
		fakeConnection.ChannelReturns(&amqp.Channel{}, nil)
		closeChan := make(chan *amqp.Error)
		fakeRWMux := &fakes.FakeRwLocker{}
		fakeMux := &fakes.FakeLocker{}
		fakeConsumer := &fakeRestartableConsumer{}
		conn := &connection{
			amqpConn:    fakeConnection,
			notifyClose: closeChan,
			connMux:     fakeMux,
			// a single consumer with status cancelled will allow the function
			//  under test to execute, but it will not actually try to restart and error
			consumers:   map[string]restartableConsumer{"foo": fakeConsumer},
			consumerMux: fakeRWMux,
			rmCallback:  func(string) {},
		}

		Convey("restarts the consumers", func() {
			fakeConsumer2 := &fakeRestartableConsumer{}
			conn.consumers["bar"] = fakeConsumer2

			err := conn.restartConsumers()
			So(err, ShouldBeNil)
			So(fakeRWMux.LockCallCount(), ShouldEqual, 1)
			So(fakeRWMux.UnlockCallCount(), ShouldEqual, 1)
			So(fakeConsumer.RestartCallCount(), ShouldEqual, 1)
			So(fakeConsumer2.RestartCallCount(), ShouldEqual, 1)
		})

		Convey("skips consumers that have been cancelled", func() {
			fakeStatus := statusCancelled
			fakeConsumer.GetStatusReturns(fakeStatus)

			err := conn.restartConsumers()
			So(err, ShouldBeNil)
			So(fakeConsumer.GetStatusCallCount(), ShouldEqual, 1)
			So(fakeConsumer.RestartCallCount(), ShouldEqual, 0)
		})

		Convey("errors if channel creation fails", func() {
			fakeConnection.ChannelReturns(nil, errors.New("no channel for you!"))

			err := conn.restartConsumers()
			So(err, ShouldNotBeNil)
			So(err.Error(), ShouldContainSubstring, "no channel")
		})

		Convey("errors if consumer restart fails", func() {
			fakeChannel := &fakes.FakeAmqpChannel{}
			fakeConnection.ChannelReturns(fakeChannel, nil)
			fakeConsumer.RestartReturns(errors.New("consumer fail"))

			err := conn.restartConsumers()
			So(err, ShouldNotBeNil)
			So(err.Error(), ShouldContainSubstring, "consumer fail")
		})
	})
}

/*----------
| Wrappers |
----------*/

func TestWrappers(t *testing.T) {
	Convey("the structs meet our wrapper interfaces ", t, func() {
		var _ amqpConnection = &connectionWrapper{&amqp.Connection{}}
		var _ amqpChannel = &amqp.Channel{}
		var _ rwLocker = &sync.RWMutex{}
	})
}
