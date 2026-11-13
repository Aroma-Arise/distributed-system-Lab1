from contextlib import ExitStack
from common import (
    check, contents, get_file, opened_request, partial_post, post_file,
    read_response, run, status, unique, waiting,
)


def test(port):
    prefix = unique("limit")
    probe_path = f"/{prefix}-probe.txt"
    expected = b"Probe OK"
    post_file(port, probe_path, expected)

    with ExitStack() as stack:
        for index in range(9):
            stack.enter_context(partial_post(port, f"/{prefix}-{index}.txt", b"A", 2))
        get_file(port, probe_path, expected)

        tenth = stack.enter_context(partial_post(port, f"/{prefix}-9.txt", b"A", 2))
        check(waiting(tenth), "Tenth upload remains open with an incomplete body")
        with opened_request(port, "GET", probe_path, timeout=5) as probe:
            check(waiting(probe, duration=0.50), "An eleventh request waits with 10 slots occupied")
            tenth.send(b"B")
            status(read_response(tenth), (200, 201, 204), "One upload completes and frees a slot")
            result = read_response(probe)
            status(result, 200, "The eleventh request proceeds")
            contents(result, expected, "The waiting request receives the expected file")


if __name__ == "__main__":
    run("Server accepts 10 active requests and makes the eleventh wait", test)
