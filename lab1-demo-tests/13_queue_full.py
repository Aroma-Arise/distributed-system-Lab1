from common import (
    broker_path, check, get_message, opened_request, publish, read_response,
    run, status, subscribe, unique, unsubscribe, waiting,
)

QUEUE_CAPACITY = 16


def test(port):
    topic = unique("queue-full")
    slow = subscribe(port, topic)

    queued = [f"Message {index}".encode() for index in range(QUEUE_CAPACITY)]
    for body in queued:
        publish(port, topic, body)

    fast = subscribe(port, topic)
    overflow = b"Message when the first queue is full"
    publish(port, topic, overflow, timeout=3)
    get_message(port, topic, fast, overflow)

    for body in queued:
        get_message(port, topic, slow, body)

    with opened_request(port, "GET", broker_path("events", topic, slow), timeout=5) as pending:
        check(waiting(pending), "The overflow publication was skipped for the full queue")
        unsubscribe(port, topic, slow)
        status(read_response(pending), 404, "Unsubscribe ends the waiting request")

    unsubscribe(port, topic, fast)


if __name__ == "__main__":
    run("A full subscriber queue does not block other subscribers", test)
