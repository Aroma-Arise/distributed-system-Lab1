from common import (
    broker_path, check, get_message, opened_request, publish, read_response,
    run, status, subscribe, unique, unsubscribe, waiting,
)

QUEUE_CAPACITY = 10

## 测试订阅者队列满时，新的发布不会阻塞其他订阅者
# 创建慢订阅者 slow。
# 连续发布十六条消息，但它一直不读取。
# 此时它的队列已经满了。
# 再创建新订阅者 fast，它的队列是空的。
# 发布第十七条消息。
# slow 因为队列已满，跳过这条消息
# 第十七条信息就没有，因为队列满了

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
