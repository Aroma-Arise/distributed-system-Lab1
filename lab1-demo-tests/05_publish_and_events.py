from concurrent.futures import ThreadPoolExecutor
from threading import Barrier
from common import (
    broker_path, check, get_message, publish, request, run, status,
    subscribe, unique, unsubscribe,
)


def test(port):
    topic = unique("publish")
    other_topic = unique("other")
    publish(port, topic, b"Published before anyone subscribed")
    first_id = subscribe(port, topic)
    second_id = subscribe(port, topic)
    other_id = subscribe(port, other_topic)

    publish(port, other_topic, b"Different topic")
    messages = (b"First message", b"Second message")
    for body in messages:
        publish(port, topic, body)

    for subscriber_id in (first_id, second_id):
        for body in messages:
            get_message(port, topic, subscriber_id, body)
    get_message(port, other_topic, other_id, b"Different topic")

    concurrent_messages = [f"Concurrent publisher {i}".encode() for i in range(8)]
    start = Barrier(len(concurrent_messages))

    def send(body):
        start.wait(timeout=5)
        publish(port, topic, body)

    with ThreadPoolExecutor(max_workers=len(concurrent_messages)) as pool:
        list(pool.map(send, concurrent_messages))

    delivered_orders = []
    for subscriber_id in (first_id, second_id):
        path = broker_path("events", topic, subscriber_id)
        observed = []
        for _ in concurrent_messages:
            result = request(port, "GET", path)
            status(result, 200, f"Read a concurrent publication for {subscriber_id}")
            observed.append(result[2])
        check(sorted(observed) == sorted(concurrent_messages),
              f"Subscriber {subscriber_id} got all concurrent publications")
        delivered_orders.append(observed)

    check(delivered_orders[0] == delivered_orders[1],
          "Both subscribers observed the same order for concurrent publications")
    unsubscribe(port, topic, first_id)
    unsubscribe(port, topic, second_id)
    unsubscribe(port, other_topic, other_id)


if __name__ == "__main__":
    run("Publish fan-out, FIFO delivery, and topic isolation", test)
