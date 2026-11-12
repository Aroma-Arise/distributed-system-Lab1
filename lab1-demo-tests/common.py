import argparse
import http.client
import json
import select
import socket
import time
import uuid
from contextlib import contextmanager
from urllib.parse import urlencode

HOST = "127.0.0.1"


def unique(prefix):
    return "lab1-" + prefix + "-" + uuid.uuid4().hex[:12]


def check(condition, description, details=""):
    if not condition:
        raise AssertionError(f"{description}. {details}")
    print(f"  PASS  {description}", flush=True)


def status(result, expected, description):
    actual, _, body = result
    options = expected if isinstance(expected, tuple) else (expected,)
    check(actual in options, description,
          f"Expected HTTP {options}, got {actual}. Body: {body[:200]!r}")


def contents(result, expected, description):
    actual = result[2]
    check(actual == expected, description,
          f"Expected {len(expected)} bytes {expected[:60]!r}, "
          f"got {len(actual)} bytes {actual[:60]!r}")


def read_response(conn):
    with conn.getresponse() as response:
        return response.status, dict((k.lower(), v) for k, v in response.getheaders()), response.read()


def request(port, method, path, body=None, timeout=5):
    with opened_request(port, method, path, body=body, timeout=timeout) as conn:
        return read_response(conn)


@contextmanager
def opened_request(port, method, path, body=None, timeout=15):
    conn = http.client.HTTPConnection(HOST, port, timeout=timeout)
    try:
        conn.request(method, path, body=body, headers={"Connection": "close"})
        yield conn
    finally:
        conn.close()


@contextmanager
def partial_post(port, path, first_part, total_size):
    conn = http.client.HTTPConnection(HOST, port, timeout=5)
    try:
        conn.putrequest("POST", path)
        conn.putheader("Content-Length", str(total_size))
        conn.putheader("Connection", "close")
        conn.endheaders(first_part)
        yield conn
    finally:
        conn.close()


def waiting(conn, duration=0.30):
    return not select.select([conn.sock], [], [], duration)[0]


def broker_path(endpoint, topic=None, subscriber_id=None):
    params = {}
    if topic is not None:
        params["topic"] = topic
    if subscriber_id is not None:
        params["id"] = subscriber_id
    return "/" + endpoint + ("?" + urlencode(params) if params else "")


def subscribe(port, topic):
    result = request(port, "POST", broker_path("subscribe", topic), b"")
    status(result, 201, f"Subscribe to {topic}")
    subscriber_id = json.loads(result[2])["id"]
    check(type(subscriber_id) is int and subscriber_id > 0,
          f"Received positive integer subscriber ID {subscriber_id!r}")
    return subscriber_id


def publish(port, topic, body, timeout=5):
    result = request(port, "POST", broker_path("publish", topic), body, timeout)
    status(result, (200, 202, 204), f"Publish to {topic}: {body[:40]!r}")


def unsubscribe(port, topic, subscriber_id):
    result = request(port, "POST", broker_path("unsubscribe", topic, subscriber_id), b"")
    status(result, (200, 204), f"Unsubscribe ID {subscriber_id} from {topic}")


def get_message(port, topic, subscriber_id, body):
    result = request(port, "GET", broker_path("events", topic, subscriber_id))
    status(result, 200, f"GET /events for subscriber {subscriber_id}")
    contents(result, body, f"Subscriber {subscriber_id} received {body[:60]!r}")


def post_file(port, path, body):
    result = request(port, "POST", path, body)
    status(result, (200, 201, 204), f"POST {path}")


def get_file(port, path, body):
    result = request(port, "GET", path)
    status(result, 200, f"GET {path}")
    contents(result, body, f"{path} matches byte for byte")


def run(title, test):
    parser = argparse.ArgumentParser(description=title)
    parser.add_argument("--port", type=int, default=8081, help="local server port (default: 8081)")
    args = parser.parse_args()
    print(f"{title}  [{HOST}:{args.port}]", flush=True)
    started = time.monotonic()
    try:
        test(args.port)
    except (AssertionError, OSError, http.client.HTTPException, ValueError, KeyError) as error:
        parser.exit(1, f"FAIL  {error}\n")
    print(f"PASS  {title} ({time.monotonic() - started:.2f}s)")
