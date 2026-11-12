import time
from common import (
    broker_path, check, contents, opened_request, publish, read_response,
    run, status, subscribe, unique, unsubscribe, waiting,
)


def test(port):
    topic = unique("wake")
    subscriber_id = subscribe(port, topic)
    path = broker_path("events", topic, subscriber_id)
    body = b"Published while events was waiting"
    with opened_request(port, "GET", path) as pending:
        check(waiting(pending), "Empty /events request is waiting")
        started = time.monotonic()
        publish(port, topic, body, timeout=3)
        result = read_response(pending)
        elapsed = time.monotonic() - started

    status(result, 200, "Waiting /events returns a message")
    contents(result, body, "Response body matches the published message")
    check(elapsed < 5, f"Polling returned after publish in {elapsed:.2f}s")
    unsubscribe(port, topic, subscriber_id)


if __name__ == "__main__":
    run("Long poll succeeds before the 10-second timeout", test)
