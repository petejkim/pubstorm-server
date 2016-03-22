package main

import (
	"os"
	"os/signal"
	"syscall"
	"time"

	log "github.com/Sirupsen/logrus"
	"github.com/nitrous-io/rise-server/edged/invalidator"
	"github.com/nitrous-io/rise-server/pkg/mqconn"
	"github.com/nitrous-io/rise-server/shared/exchanges"
	"github.com/streadway/amqp"
)

func main() {
	run()
	os.Exit(1)
}

func run() {
	mq, err := mqconn.MQ()
	if err != nil {
		log.Errorln("Failed to connect to mq:", err)
		return
	}
	connErrCh := mq.NotifyClose(make(chan *amqp.Error))

	ch, err := mq.Channel()
	if err != nil {
		log.Errorln("Failed to obtain channel:", err)
		return
	}

	defer func() {
		err = ch.Close()
		if err != nil {
			log.Errorln("Failed to close channel:", err)
		}
	}()

	exchangeName := exchanges.Edges

	if err := ch.ExchangeDeclare(
		exchangeName, // name
		"direct",     // type
		true,         // durable
		false,        // auto-deleted
		false,        // internal
		false,        // no-wait
		nil,          // arguments
	); err != nil {
		log.Errorf("Failed to declare exchange(%s): %v", exchangeName, err)
		return
	}

	routeKey := exchanges.RouteV1Invalidation

	q, err := ch.QueueDeclare(
		"",    // name
		true,  // durable
		true,  // delete when usused
		false, // exclusive. This should be false to make connection persistent
		false, // no-wait
		nil,   // arguments
	)
	if err != nil {
		log.Errorf("Failed to declare queue for exchange(%s) and route(%s): %v", exchangeName, routeKey, err)
		return
	}

	if ch.QueueBind(
		q.Name,       // queue name
		routeKey,     // routing key
		exchangeName, // exchange
		false,
		nil,
	); err != nil {
		log.Errorf("Failed to bind queue(%s) for route(%s) to exchange(%s): %v", q.Name, routeKey, exchangeName, err)
		return
	}

	defer func() {
		if err = ch.QueueUnbind(
			q.Name,       // queue name
			routeKey,     // routing key
			exchangeName, // exchange
			nil,
		); err != nil {
			log.Errorf("Failed to unbind queue(%s) for route(%s) from exchange(%s): %v", q.Name, routeKey, exchangeName, err)
		}
	}()

	msgCh, err := ch.Consume(
		q.Name, // queue
		"",     // consumer
		false,  // auto-ack
		false,  // exclusive
		false,  // no-local
		false,  // no-wait
		nil,    // args
	)

	if err != nil {
		log.Errorf("Failed to start consuming message from queue(%s): %v", q.Name, err)
		return
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)

	log.Infof("Worker started listening to queue(%s)...", q.Name)

	for {
		select {
		case d := <-msgCh:
			err := invalidator.Work(d.Body)

			if err != nil {
				// failure
				log.Warnln("Work failed", err, string(d.Body))

				go func() {
					// nack after a delay to prevent thrashing
					time.Sleep(1 * time.Second)
					if err := d.Nack(false, true); err != nil {
						log.WithFields(log.Fields{"queue": q.Name}).Warnln("Failed to Nack message:", err)
					}
				}()
			} else {
				// success
				if err := d.Ack(false); err != nil {
					log.WithFields(log.Fields{"queue": q.Name}).Warnln("Failed to Ack message:", err)
				}
			}
		case err := <-connErrCh:
			log.Errorln(err)
			return

		case sig := <-sigCh:
			log.Errorln("Caught signal:", sig)
			return
		}
	}
}
