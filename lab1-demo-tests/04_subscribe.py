from common import broker_path, check, request, run, status, subscribe, unique, unsubscribe


def test(port):
    topic = unique("subscribe")
    status(request(port, "POST", "/subscribe", b""), 400, "Missing topic")
    status(request(port, "GET", broker_path("subscribe", topic)), 501, "GET is unsupported for /subscribe")
    first_id = subscribe(port, topic)
    second_id = subscribe(port, topic)
    check(first_id != second_id, "The two subscriptions received different IDs")
    unsubscribe(port, topic, first_id)
    unsubscribe(port, topic, second_id)


if __name__ == "__main__":
    run("Subscribe and validate subscriber IDs", test)
