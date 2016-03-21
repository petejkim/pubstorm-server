package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/nitrous-io/rise-server/deployer/deployer"
	"github.com/nitrous-io/rise-server/pkg/mqconn"
	"github.com/nitrous-io/rise-server/shared/queues"
	"github.com/streadway/amqp"

	log "github.com/Sirupsen/logrus"
)

func main() {
	run()
	os.Exit(1)
}

func run() {
	queueName := queues.Deploy

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

	_, err = ch.QueueDeclare(
		queueName,
		true,  // durable
		false, // delete when unused
		false, // exclusive
		false, // noWait
		nil,
	)
	if err != nil {
		log.Errorln(err)
		return
	}

	msgCh, err := ch.Consume(
		queueName, // queue
		"",        // consumer
		false,     // auto-ack
		false,     // exclusive
		false,     // no-local
		false,     // no-wait
		nil,       // args
	)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)

	log.Infoln(fmt.Sprintf(`Worker started listening to queue "%s"...`, queueName))

	for {
		select {
		case d := <-msgCh:
			err = deployer.Work(d.Body)
			if err != nil {
				// failure
				log.Warnln("Work failed", err, string(d.Body))

				if err := d.Nack(false, true); err != nil {
					log.WithFields(log.Fields{"queue": queueName}).Warnln("Failed to Nack message: ", err)
				}
			} else {
				// success
				if err := d.Ack(false); err != nil {
					log.WithFields(log.Fields{"queue": queueName}).Warnln("Failed to Ack message: ", err)
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
