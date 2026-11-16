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

// Broker holds all pub-sub state. Every request runs in its own goroutine, so all fields below are guarded by mu.
type Broker struct {
	mu           sync.Mutex						// locked by every handler that reads or writes the fields below
	nextId       int							// last subscriber ID handed out; only POST /subscribe increments it
	topics       map[string]map[int]*Subscriber // topic -> (subscriber ID -> subscriber)
	lastSequence map[string]int 				// topic -> sequence number of the last published message
}

// Subscriber is one subscription to one topic.
type Subscriber struct {
	message chan Message 	// bounded queue (buffered channel, capacity 10); publish skips it when full
	polling bool			// true while a GET /events is waiting, to reject a second concurrent poll (409)
	done    chan struct{}	// closed on unsubscribe to wake up a waiting poll
}

// Message is one published message.
type Message struct {
	Sequence int    // index number of the message (這則訊息在該 topic 的編號)
	Body     []byte // message content (request body sent by the publisher)
}

func newBroker() *Broker {
	// create a new Broker instance with an empty topics map
	return &Broker{
		topics:       make(map[string]map[int]*Subscriber),
		lastSequence: make(map[string]int),
	}
}

// subscribe registers a new subscriber on topic and returns its unique ID.
func (b *Broker) subscribe(topic string) int {

	// Hold the lock other broker operations (subscribe, publish, events, unsubscribe) wait until it returns.
	b.mu.Lock()
	defer b.mu.Unlock()

	// One ID per subscription, unique across all topics.
	b.nextId++
	id := b.nextId

	// Create the inner map the first time a topic is used.
	if b.topics[topic] == nil {
		b.topics[topic] = make(map[int]*Subscriber)
	}

	b.topics[topic][id] = &Subscriber{
		message: make(chan Message, 10), 	// create a buffered channel for the subscriber
		done:    make(chan struct{}),		// closed on unsubscribe
	}										// polling starts as false (zero value)

	return id
}

// handleSubscribe handles POST /subscribe?topic=X and responds 201 with {"id":N}.
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

// publish new message to every current subscriber of topic and returns how many subscribers received it.
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

	// Map iteration order is random, but each subscriber has its own queue ( in-order receives messages).
	for _, subscriber := range b.topics[topic] { 
		// Non-blocking send: publish never waits on a slow subscriber.
		select {
		case subscriber.message <- message:
			delivered++
		default:
		// if the subscriber's channel is full, skip it
		}
	}

	return delivered
}

// handlePublish handles POST /publish?topic=X and responds 200 with {"delivered":N}.
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

	// Read the whole body (up to Content-Length) before taking the lock, so a slow sender never blocks other broker operations.
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

// unsubscribe removes subscriber id from topic and reports whether it existed.
func (b *Broker) unsubscribe(topic string, id int) bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	// Indexing a nil map is safe in Go, so an unknown topic also ends up here with subscriber == nil.
	subscribers := b.topics[topic]
	subscriber := subscribers[id]
	if subscriber == nil {
		return false
	}

	// Remove id from this topic's subscriber map; the Subscriber itself is not destroyed.
	delete(subscribers, id)

	// Wake up a GET /events that may be waiting on this subscriber; it then responds 404.
	close(subscriber.done)
	return true
}

// handleUnsubscribe handles POST /unsubscribe?topic=X&id=N.
func handleUnsubscribe(conn net.Conn, req *http.Request, broker *Broker) {
	if req.Method != http.MethodPost {
		writeResponse(conn, http.StatusNotImplemented, "text/plain",
			[]byte("Method not implemented\n"))
		return
	}

	query := req.URL.Query()
	topic := query.Get("topic")
	id, err := strconv.Atoi(query.Get("id"))

	// IDs start at 1, so 0 and negative values are invalid (400), not unknown (404).
	if topic == "" || err != nil || id <= 0 {
		writeResponse(conn, http.StatusBadRequest, "text/plain",
			[]byte("A topic and a positive subscriber ID are required\n"))
		return
	}

	if !broker.unsubscribe(topic, id) {
		writeResponse(conn, http.StatusNotFound, "text/plain",
			[]byte("Subscriber not found\n"))
		return
	}

	writeResponse(conn, http.StatusOK, "text/plain", []byte("Unsubscribed\n"))
}

// handleEvents handles GET /events?topic=X&id=N with long polling:
// 200 with the next message, 204 if none arrives within 10 s, 404 if unsubscribed.
func handleEvents(conn net.Conn, req *http.Request, broker *Broker, slots chan struct{}) {
	if req.Method != http.MethodGet {
		writeResponse(conn, http.StatusNotImplemented, "text/plain",
			[]byte("Method not implemented\n"))
		return
	}

	// Query() parses the part after "?": /events?topic=news&id=3 -> {"topic": ["news"], "id": ["3"]}
	query := req.URL.Query()
	topic := query.Get("topic")					
	id, err := strconv.Atoi(query.Get("id")) 	// string -> int

	if topic == "" || err != nil || id <= 0 {
		writeResponse(conn, http.StatusBadRequest, "text/plain",
			[]byte("Missing topic or invalid id\n"))
		return
	}

	// Lock while reading topics: another goroutine may be subscribing or unsubscribing.
	broker.mu.Lock() 

	// Reading a nil map is safe, so an unknown topic also gives subscriber == nil.
	subscriber := broker.topics[topic][id]	
	if subscriber == nil {
		broker.mu.Unlock()
		writeResponse(conn, http.StatusNotFound, "text/plain",
			[]byte("Subscriber not found\n"))
		return
	}

	// Allow only one poll per subscriber at a time, e.g. two terminals polling the same id.
	if subscriber.polling {
		broker.mu.Unlock()
		writeResponse(conn, http.StatusConflict, "text/plain",
			[]byte("Subscriber is already polling\n"))
		return
	}
	subscriber.polling = true
	broker.mu.Unlock()

	// Reset polling on every return path (200, 204, 404).
	defer func() {
		broker.mu.Lock()
		subscriber.polling = false
		broker.mu.Unlock()
	}()
	
	select {
	// Fast path: a message is already queued, so return it immediately.
	case message := <-subscriber.message: // get the first message
		writeResponse(conn, http.StatusOK, "application/octet-stream", message.Body) // octet-stream = arbitrary bytes, since a message body can be anything.
		return
	default:
		// Queue empty: fall through to long polling.
	}

	// Give back the connection slot while waiting; otherwise 10 idle polls would block the whole server
	<-slots

	timer := time.NewTimer(10 * time.Second)
	defer timer.Stop()

	var message Message
	received := false

	// Block until the first of: a new message, the timeout, or unsubscribe.
	select {
	case message = <-subscriber.message:
		received = true
	case <-timer.C:  		// timer.C receives a value after 10 s
	case <-subscriber.done: // closed by unsubscribe
	}

	// Take a slot back before doing work again (may block if the server is busy).
	slots <- struct{}{}

	// Check again: the subscriber may have been unsubscribed while we waited for the slot.
	// Scenario: publish（or timeout）→ waiting for slot → unsubscribe
	select {
	case <-subscriber.done:
		writeResponse(conn, http.StatusNotFound, "text/plain", []byte("Subscriber not found\n"))
		return
	default:
		// Still subscribed.
	}

	if received {
		writeResponse(conn, http.StatusOK, "application/octet-stream", message.Body)
		return
	}

	// Timeout: 204 has no body, so it is written directly without Content-Type.
	fmt.Fprint(conn, "HTTP/1.0 204 No Content\r\nConnection: close\r\n\r\n")
}