from common import broker_path, request, run, status, subscribe, unique, unsubscribe


def test(port):
    topic = unique("invalid")
    subscriber_id = subscribe(port, topic)
    cases = (
        (broker_path("events", subscriber_id=subscriber_id), 400, "Missing topic"),
        (broker_path("events", topic), 400, "Missing ID"),
        (broker_path("events", topic, "abc"), 400, "Non-numeric ID"),
        (broker_path("events", topic, 0), 400, "Non-positive ID"),
        (broker_path("events", unique("unknown"), subscriber_id), 404, "Valid ID with a different topic"),
    )
    for path, expected, description in cases:
        status(request(port, "GET", path), expected, description)
    status(request(port, "POST", broker_path("events", topic, subscriber_id), b""),
           501, "POST is unsupported for /events")
    unsubscribe(port, topic, subscriber_id)


if __name__ == "__main__":
    run("Invalid /events requests", test)
