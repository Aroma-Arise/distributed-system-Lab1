# Go HTTP Lab 1 Demo Tests

These scripts require Python 3.8 or later and a running Lab 1 server. They use only the Python standard library. Each script sets up its own test files or subscriptions as needed and reports individual checks. A failed check prints the reason and exits with a nonzero status.

The default server address is `127.0.0.1:8081`, which matches the Docker port mapping below. If you run the Go server directly with `go run . 8080`, pass `--port 8080` to each test script.

## Set up and run the tests

1. Extract the archive and place the entire `lab1-demo-tests/` directory in the project root, alongside `Dockerfile` and `go.mod`.
2. In the project root, copy the GET fixtures into `www/` before building the Docker image. The GET test reads these files directly, so you can run it independently of the POST tests.

   ```bash
   mkdir -p www/lab1-demo-fixtures
   cp lab1-demo-tests/fixtures/* www/lab1-demo-fixtures/
   docker build -t http-lab .
   docker run --rm --name http-lab-demo -p 127.0.0.1:8081:8080 http-lab
   ```

3. Leave the server running and open a second terminal in the project root. Run each script you want to demonstrate.

   ```bash
   python3 lab1-demo-tests/01_get_file_types.py
   python3 lab1-demo-tests/02_post_10_concurrent.py
   ```

   If the server is running directly on port 8080, use this form:

   ```bash
   python3 lab1-demo-tests/01_get_file_types.py --port 8080
   ```

## Test cases

| Script | Expected behavior |
| --- | --- |
| `01_get_file_types.py` | GET returns 200 for HTML, TXT, CSS, GIF, JPEG, and JPG. The script checks each MIME type and compares the response with the original file byte for byte. |
| `02_post_10_concurrent.py` | Ten clients send POST requests concurrently. A subsequent GET verifies the contents of each uploaded file. |
| `03_http_errors.py` | PUT returns 501. Unsupported file extensions and malformed request headers return 400. A missing file with a supported extension returns 404. |
| `04_subscribe.py` | Two subscriptions return 201 and distinct positive integer IDs. A missing topic returns 400, and GET on `/subscribe` returns 501. |
| `05_publish_and_events.py` | Both subscribers on a topic receive messages in order. Topics stay isolated, and new subscribers receive only publications made after they subscribe. Both subscribers see the same order for messages from eight concurrent publishers. |
| `06_events_poll_success.py` | GET waits on an empty queue. A subsequent POST wakes it, and GET returns 200 with the original message bytes. |
| `07_events_poll_timeout.py` | With no publication, the pending GET returns 204 after about 10 seconds. The response body is empty. |
| `08_events_errors.py` | Missing topics, missing IDs, and nonnumeric or nonpositive IDs return 400. A valid ID on an unknown topic returns 404. POST on `/events` returns 501. |
| `09_unsubscribe.py` | Unsubscribe wakes the waiting GET. Both that GET and subsequent requests using the removed ID return 404. The remaining subscriber still receives messages. |
| `10_post_atomic.py` | Two POST requests upload to the same path. At three checkpoints, GET returns the complete original file, then the complete version A, then the complete version B. |
| `11_concurrency_limit.py` | GET succeeds while nine uploads are pending. With ten uploads pending, the eleventh request waits until one slot becomes free. |
| `12_longpoll_slots.py` | The broker accepts a publication while ten long polls wait. All ten subscribers receive the message. |
| `13_queue_full.py` | When one subscriber's 16-message queue fills up, the next publication still reaches another subscriber. The original 16 messages remain in order. |

To run all scripts in order and stop at the first failure, run:

```bash
for test_file in lab1-demo-tests/[0-9][0-9]_*.py; do
  python3 "$test_file" || break
done
```

The scripts coordinate concurrent requests and long polls automatically. One terminal runs the server and a second runs the tests. To free the server terminal, start the container in the background with `docker run -d --rm --name http-lab-demo -p 127.0.0.1:8081:8080 http-lab`. Stop it after the demo with `docker stop http-lab-demo`.

## Demonstrate long polling manually with curl

Keep the server running and use two client terminals so the audience can see the GET waiting while you publish a message. In either client terminal, subscribe to a topic:

```bash
curl -i -X POST 'http://127.0.0.1:8081/subscribe?topic=live-demo'
```

Replace `1` in the following commands with the ID from the JSON response. In client terminal A, start a long poll:

```bash
curl -i 'http://127.0.0.1:8081/events?topic=live-demo&id=1'
```

Within 10 seconds, publish from client terminal B:

```bash
curl -i --data-binary 'Hello from the publisher' \
  'http://127.0.0.1:8081/publish?topic=live-demo'
```

Terminal A should show 200 and the message. To demonstrate the timeout, repeat the GET and wait for 204. To demonstrate cancellation, start another GET in terminal A and run this command in terminal B while the GET is waiting:

```bash
curl -i -X POST 'http://127.0.0.1:8081/unsubscribe?topic=live-demo&id=1'
```

The waiting GET should return 404.

## Test assumptions

- `/events` returns the raw bytes sent to `/publish`. The concurrent publication test compares the order in which subscribers receive messages.
- Test `07` allows 9.5 to 15 seconds for the nominal 10-second timeout. Test `13` uses the broker's queue capacity of 16. If you change either server setting, update the corresponding test.
- Test uploads use unique names starting with `lab1-`. With the temporary container above, Docker removes the uploaded test files when it removes the container. If you run the Go server directly, uploaded test files remain in `www/`.
