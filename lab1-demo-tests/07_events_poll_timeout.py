import time
from common import broker_path, check, contents, request, run, status, subscribe, unique, unsubscribe


def test(port):
    topic = unique("timeout")
    subscriber_id = subscribe(port, topic)
    started = time.monotonic()
    result = request(port, "GET", broker_path("events", topic, subscriber_id), timeout=16)
    elapsed = time.monotonic() - started
    status(result, 204, "Empty queue returns 204 after waiting")
    contents(result, b"", "204 response body is empty")
    check(9.5 <= elapsed <= 15, f"Elapsed time was {elapsed:.2f}s (expected about 10s)")
    unsubscribe(port, topic, subscriber_id)


if __name__ == "__main__":
    run("Long poll times out with no message", test)
