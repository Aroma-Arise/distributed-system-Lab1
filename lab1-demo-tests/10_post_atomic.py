from common import check, get_file, partial_post, post_file, read_response, run, status, unique, waiting


def test(port):
    path = f"/{unique('atomic')}.txt"
    old = b"OLD VERSION"
    first = b"A" * 65536
    second = b"B" * 65536
    middle = len(first) // 2
    post_file(port, path, old)

    with partial_post(port, path, first[:middle], len(first)) as upload_a:
        with partial_post(port, path, second[:middle], len(second)) as upload_b:
            check(waiting(upload_a) and waiting(upload_b), "Both uploads are awaiting their remaining bytes")
            get_file(port, path, old)

            upload_a.send(first[middle:])
            status(read_response(upload_a), (200, 201, 204), "Upload A finished")
            get_file(port, path, first)

            upload_b.send(second[middle:])
            status(read_response(upload_b), (200, 201, 204), "Upload B finished")
            get_file(port, path, second)


if __name__ == "__main__":
    run("GET sees complete versions during concurrent writes to the same file", test)
