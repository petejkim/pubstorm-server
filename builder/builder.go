package main

import (
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/getsentry/raven-go"
	"github.com/nitrous-io/rise-server/apiserver/common"
	"github.com/nitrous-io/rise-server/builder/builder"
	"github.com/nitrous-io/rise-server/pkg/mqconn"
	"github.com/nitrous-io/rise-server/shared/queues"
	"github.com/streadway/amqp"

	log "github.com/Sirupsen/logrus"
)

func main() {
	raven.SetDSN(common.SentryURL)

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

	err = ch.Qos(
		1,     // prefetch count
		0,     // prefetch size
		false, // global
	)

	if err != nil {
		log.Errorln("Failed to set qos to channel:", err)
		return
	}

	queueName := queues.Build

	q, err := ch.QueueDeclare(
		queueName,
		true,  // durable
		false, // delete when unused
		false, // exclusive
		false, // noWait
		nil,
	)
	if err != nil {
		log.Errorf("Failed to declare queue(%s): %v", queueName, err)
		return
	}

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
			err = builder.Work(d.Body)
			if err != nil {
				// failure
				log.Warnln("Work failed", err, string(d.Body))

				if err != builder.ErrProjectLocked {
					raven.CaptureError(err, map[string]string{
						"app":  "builder",
						"body": string(d.Body),
					})
				}

				if err == builder.ErrRecordNotFound || err == builder.ErrUnarchiveFailed {
					if err := d.Ack(false); err != nil {
						log.WithFields(log.Fields{"queue": queueName}).Warnln("Failed to Ack message:", err)
					}
				} else {
					go func() {
						// nack after a delay to prevent thrashing
						time.Sleep(1 * time.Second)
						if err := d.Nack(false, true); err != nil {
							log.WithFields(log.Fields{"queue": queueName}).Warnln("Failed to Nack message:", err)
						}
					}()
				}
			} else {
				// success
				if err := d.Ack(false); err != nil {
					log.WithFields(log.Fields{"queue": queueName}).Warnln("Failed to Ack message:", err)
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
