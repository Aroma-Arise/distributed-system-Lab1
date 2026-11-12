package main

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// have a lock, because multiple goroutines will access the topics map concurrently
type Broker struct {
	mu           sync.Mutex
	nextId       int
	topics       map[string]map[int]*Subscriber
	lastSequence map[string]int // keep track of the last index in this topic
}

type Subscriber struct {
	message chan Message
	polling bool
	done    chan struct{}
}

type Message struct {
	Sequence int    // index number of the message
	Body     []byte // message content
}

func newBroker() *Broker {
	// create a new Broker instance with an empty topics map
	return &Broker{
		topics:       make(map[string]map[int]*Subscriber),
		lastSequence: make(map[string]int),
	}
}

func (b *Broker) subscribe(topic string) int {
	// add a lock to protect data
	b.mu.Lock()
	defer b.mu.Unlock()

	b.nextId++
	id := b.nextId

	if b.topics[topic] == nil {
		// create a new map for the topic if it doesn't exist
		b.topics[topic] = make(map[int]*Subscriber)
	}

	b.topics[topic][id] = &Subscriber{
		message: make(chan Message, 10), // create a buffered channel for the subscriber
		done:    make(chan struct{}),
	}

	return id
}

func (b *Broker) publish(topic string, body []byte) int {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.lastSequence[topic]++
	message := Message{
		Sequence: b.lastSequence[topic],
		Body:     body,
	}

	// how many subscribers have received the message
	delivered := 0
	for _, subscriber := range b.topics[topic] {
		select {
		case subscriber.message <- message:
			delivered++
		default:
			// if the subscriber's channel is full, skip it
		}
	}

	return delivered
}

func handleSubscribe(conn net.Conn, req *http.Request, broker *Broker) {
	if req.Method != http.MethodPost {
		writeResponse(conn, http.StatusNotImplemented, "text/plain",
			[]byte("Method not implemented\n"))
		return
	}

	// get topic from URL
	topic := req.URL.Query().Get("topic")
	if topic == "" {
		writeResponse(conn, http.StatusBadRequest, "text/plain",
			[]byte("Missing topic\n"))
		return
	}

	// create a new subscriber for the topic and get its ID
	id := broker.subscribe(topic)
	body := []byte(fmt.Sprintf("{\"id\":%d}\n", id))
	writeResponse(conn, http.StatusCreated, "application/json", body)
}

func handlePublish(conn net.Conn, req *http.Request, broker *Broker) {
	if req.Method != http.MethodPost {
		writeResponse(conn, http.StatusNotImplemented, "text/plain",
			[]byte("Method not implemented\n"))
		return
	}

	topic := req.URL.Query().Get("topic")

	if topic == "" {
		writeResponse(conn, http.StatusBadRequest, "text/plain",
			[]byte("Missing topic\n"))
		return
	}

	body, err := io.ReadAll(req.Body)
	if err != nil {
		writeResponse(conn, http.StatusBadRequest, "text/plain",
			[]byte("Could not read request body\n"))
		return
	}

	delivered := broker.publish(topic, body)
	response := []byte(fmt.Sprintf("{\"delivered\":%d}\n", delivered))
	writeResponse(conn, http.StatusOK, "application/json", response)
}

func handleEvents(conn net.Conn, req *http.Request, broker *Broker, slots chan struct{}) {
	if req.Method != http.MethodGet {
		writeResponse(conn, http.StatusNotImplemented, "text/plain",
			[]byte("Method not implemented\n"))
		return
	}

	query := req.URL.Query()
	topic := query.Get("topic")
	id, err := strconv.Atoi(query.Get("id"))

	if topic == "" || err != nil || id <= 0 {
		writeResponse(conn, http.StatusBadRequest, "text/plain",
			[]byte("Missing topic or invalid id\n"))
		return
	}

	broker.mu.Lock()
	// get the subscriber for the topic and id
	subscriber := broker.topics[topic][id]
	if subscriber == nil {
		broker.mu.Unlock()
		writeResponse(conn, http.StatusNotFound, "text/plain",
			[]byte("Subscriber not found\n"))
		return
	}

	// if the subscriber is already polling, return a conflict response
	if subscriber.polling {
		broker.mu.Unlock()
		writeResponse(conn, http.StatusConflict, "text/plain",
			[]byte("Subscriber is already polling\n"))
		return
	}
	subscriber.polling = true
	broker.mu.Unlock()

	defer func() {
		broker.mu.Lock()
		subscriber.polling = false
		broker.mu.Unlock()
	}()

	select {
	case message := <-subscriber.message: // get the first message
		writeResponse(conn, http.StatusOK, "application/octet-stream", message.Body)
		return
	default:
		// long polling
	}

	// if there is no message, wait for a message or timeout
	<-slots

	timer := time.NewTimer(10 * time.Second)
	defer timer.Stop()

	var message Message
	received := false

	// will wait for a message or timeout, whichever comes first
	// if no channel is ready, it will block until one of the channels is ready
	select {
	case message = <-subscriber.message:
		received = true
	case <-timer.C:
		// timeout, no message received
	case <-subscriber.done:
	}

	slots <- struct{}{}

	select {
	case <-subscriber.done:
		writeResponse(conn, http.StatusNotFound, "text/plain",
			[]byte("Subscriber not found\n"))
		return
	default:
		// 订阅仍有效，继续处理收到消息或超时的情况。
	}

	if received {
		writeResponse(conn, http.StatusOK, "application/octet-stream", message.Body)
		return
	}

	fmt.Fprint(conn, "HTTP/1.0 204 No Content\r\nConnection: close\r\n\r\n")
}

func (b *Broker) unsubscribe(topic string, id int) bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	subscribers := b.topics[topic]
	subscriber := subscribers[id]
	if subscriber == nil {
		return false
	}

	delete(subscribers, id)
	close(subscriber.done)
	return true
}
