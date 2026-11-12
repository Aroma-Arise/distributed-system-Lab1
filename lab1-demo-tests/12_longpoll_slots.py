from contextlib import ExitStack
from common import (
    broker_path, check, contents, opened_request, publish, read_response,
    run, status, subscribe, unique, unsubscribe, waiting,
)


def test(port):
    topic = unique("poll-slots")
    ids = [subscribe(port, topic) for _ in range(10)]
    body = b"Wake all ten subscribers"

    with ExitStack() as stack:
        pending = [
            stack.enter_context(opened_request(port, "GET", broker_path("events", topic, subscriber_id)))
            for subscriber_id in ids
        ]
        check(all(waiting(conn, duration=0.08) for conn in pending), "All 10 long polls are waiting")
        publish(port, topic, body, timeout=3)
        for subscriber_id, conn in zip(ids, pending):
            result = read_response(conn)
            status(result, 200, f"Waiting subscriber {subscriber_id} gets a response")
            contents(result, body, f"Waiting subscriber {subscriber_id} gets the publication")

    for subscriber_id in ids:
        unsubscribe(port, topic, subscriber_id)


if __name__ == "__main__":
    run("Publish can enter while 10 long polls wait", test)
