package mqconn

import (
	"os"
	"sync"

	"github.com/streadway/amqp"
)

var (
	mq     *amqp.Connection
	mqLock sync.Mutex

	closeChan chan *amqp.Error
)

// MQ returns RabbitMQ conection
func MQ() (*amqp.Connection, error) {
	mqLock.Lock()
	defer mqLock.Unlock()
	if mq == nil {
		conn, err := amqp.Dial(os.Getenv("AMQP_URL"))
		if err != nil {
			return nil, err
		}
		mq = conn

		closeChan = conn.NotifyClose(make(chan *amqp.Error))
		go func() {
			<-closeChan
			mq = nil
		}()
	}
	return mq, nil
}
