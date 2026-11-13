import socket
from common import HOST, check, request, run, status, unique


def test(port):
    name = unique("missing")
    status(request(port, "PUT", f"/{name}.txt"), 501, "Unsupported PUT")
    status(request(port, "GET", f"/{name}.exe"), 400, "GET unsupported file type")
    status(request(port, "POST", f"/{name}.exe", b"demo"), 400, "POST unsupported file type")
    status(request(port, "GET", f"/{name}.txt"), 404, "GET missing file with a supported extension")

    with socket.create_connection((HOST, port), timeout=5) as conn:
        conn.sendall(b"GET /test.txt HTTP/1.1\r\nHost: localhost\r\nBroken Header\r\n\r\n")
        with conn.makefile("rb") as stream:
            line = stream.readline()
    check(line.split()[1:2] == [b"400"], "Malformed HTTP header returns 400", repr(line))


if __name__ == "__main__":
    run("HTTP error responses", test)
