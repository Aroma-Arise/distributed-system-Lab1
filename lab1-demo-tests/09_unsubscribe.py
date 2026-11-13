from common import (
    broker_path, check, get_message, opened_request, publish, read_response,
    request, run, status, subscribe, unique, unsubscribe, waiting,
)


def test(port):
    topic = unique("unsubscribe")
    removed_id = subscribe(port, topic)
    remaining_id = subscribe(port, topic)
    events = broker_path("events", topic, removed_id)

    with opened_request(port, "GET", events, timeout=5) as pending:
        check(waiting(pending), "Subscriber has a pending /events request")
        unsubscribe(port, topic, removed_id)
        status(read_response(pending), 404, "Unsubscribe wakes the waiting /events with 404")

    status(request(port, "GET", events), 404, "New /events with removed ID returns 404")
    status(request(port, "POST", broker_path("unsubscribe", topic, removed_id), b""),
           404, "Repeating unsubscribe returns 404")
    body = b"For the remaining subscriber"
    publish(port, topic, body)
    get_message(port, topic, remaining_id, body)
    unsubscribe(port, topic, remaining_id)


if __name__ == "__main__":
    run("Unsubscribe removes the ID and wakes a waiting request", test)
