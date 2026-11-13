from concurrent.futures import ThreadPoolExecutor
from threading import Barrier
from common import get_file, request, run, status, unique


def test(port):
    prefix = unique("post")
    paths = [f"/{prefix}-{index}.txt" for index in range(10)]
    payloads = [f"Payload from client {index}\n".encode() for index in range(10)]
    start = Barrier(10)

    def upload(path, payload):
        start.wait(timeout=5)
        return request(port, "POST", path, payload)

    with ThreadPoolExecutor(max_workers=10) as pool:
        futures = [pool.submit(upload, path, body) for path, body in zip(paths, payloads)]
        for path, future in zip(paths, futures):
            status(future.result(), (200, 201, 204), f"Concurrent POST {path}")

    for path, body in zip(paths, payloads):
        get_file(port, path, body)


if __name__ == "__main__":
    run("10 concurrent POST requests and readback", test)
