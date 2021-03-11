package bunny

import (
	"errors"
	"testing"

	"github.com/batchcorp/rabbit/bunny/fakes"
	. "github.com/smartystreets/goconvey/convey"
	"github.com/streadway/amqp"
)

func TestNewBunny(t *testing.T) {
	Convey("NewBunny()", t, func() {
		Convey("Sets the connection details", func() {
			details := ConnectionDetails{
				URLs: []string{"foobar"},
			}

			b := NewBunny(details)
			So(b.connDetails, ShouldNotBeNil)
			So(b.connDetails.URLs, ShouldResemble, details.URLs)
			So(b.connDetails.maxChannelsPerConnection, ShouldEqual, maxChannelsPerConnection)
			So(b.connDetails.dialer, ShouldHaveSameTypeAs, &amqpDialer{})
		})
	})
}

func TestConnect(t *testing.T) {
	Convey("Connect()", t, func() {
		bunn := NewBunny(ConnectionDetails{
			URLs: []string{"foobar"},
		})

		fakeDialer := &fakes.FakeDialer{}
		bunn.connDetails.dialer = fakeDialer

		fakeDialer.DialReturns(&amqp.Connection{}, nil)

		Convey("connects to rabbit", func() {
			err := bunn.Connect()

			So(err, ShouldBeNil)
			So(bunn.connections, ShouldNotBeNil)
			So(bunn.connections.conns, ShouldHaveLength, 1)
		})

		Convey("errors if called twice", func() {
			err := bunn.Connect()
			So(err, ShouldBeNil)

			err = bunn.Connect()
			So(err, ShouldNotBeNil)
			So(err.Error(), ShouldEqual, "connect may only be called once")
			So(bunn.connections, ShouldNotBeNil)
			So(bunn.connections.conns, ShouldHaveLength, 1)
		})

		Convey("errors if connect fails", func() {
			fakeDialer.DialReturns(nil, errors.New("did not connect"))

			err := bunn.Connect()
			So(err, ShouldNotBeNil)
			So(err.Error(), ShouldContainSubstring, "did not connect")
		})
	})
}
