from pathlib import Path
from common import check, contents, request, run, status

FIXTURES = Path(__file__).resolve().parent / "fixtures"
TYPES = {
    "sample.html": "text/html",
    "sample.txt": "text/plain",
    "sample.css": "text/css",
    "sample.gif": "image/gif",
    "sample.jpeg": "image/jpeg",
    "sample.jpg": "image/jpeg",
}


def test(port):
    for filename, mime in TYPES.items():
        result = request(port, "GET", "/lab1-demo-fixtures/" + filename)
        status(result, 200, f"GET {filename}")
        actual = result[1].get("content-type", "").split(";", 1)[0].strip().lower()
        check(actual == mime, f"{filename} Content-Type is {mime}", f"Got {actual!r}")
        contents(result, (FIXTURES / filename).read_bytes(), f"{filename} matches byte for byte")


if __name__ == "__main__":
    run("GET supported file types", test)
