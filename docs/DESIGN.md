# TDA596 Lab 1 Design Notes

## Contents

- [1. Roles and relationships](#1-roles-and-relationships)
- [2. Connections](#2-connections)
    - [Common lifecycle](#common-lifecycle)
    - [2.1 Flow of each connection](#21-flow-of-each-connection)
    - [2.2 Errors for each connection](#22-errors-for-each-connection)
    - [2.3 Data structures and mechanisms](#23-data-structures-and-mechanisms)

---

## 1. Roles and relationships

### 1.1 The five roles

| Role           | What it is                                      | Where it lives                                                 | In the code                                                               |
| -------------- | ----------------------------------------------- | -------------------------------------------------------------- | ------------------------------------------------------------------------- |
| **Server**     | The running `http_server` program (one process) | In a Docker container (or locally)                             | `main()` in `main.go`                                                     |
| **Client**     | Any program that connects to the server         | Outside the server                                             | curl, browser, Python tests                                               |
| **Broker**     | The pub-sub component **inside** the server     | Inside the server                                              | `Broker` struct + 4 handlers                                              |
| **Publisher**  | A client that sends messages                    | Outside the server                                             | A client calling `POST /publish`                                          |
| **Subscriber** | A client that subscribes and receives messages  | **Both sides**: a client outside, a `Subscriber` record inside | A client calling `/subscribe` and `/events`; a `Subscriber` in the server |

### 1.2 Containment

```
┌────────────────────────── Server (http_server) ──────────────────────────┐
│                                                                          │
│  File server (main.go)              Broker (broker.go)                   │
│  ├ Listener + 10 slots              ├ topics: "news"  → {1, 3}           │
│  ├ Parse, check, route by path      │         "sport" → {2}              │
│  ├ GET / POST files                 ├ One queue (cap 10) per subscriber  │
│  └ www/ (via os.Root)               └ nextId, lastSequence, mu           │
│                                                                          │
└───────────────────────────────────▲──────────────────────────────────────┘
                                    │ every connection: 
                                    │ client → server (TCP), one request each
          ┌─────────────────────────┼─────────────────────────┐
          │                         │                         │
      Publisher                Subscriber              Plain client
   (POST /publish) (/subscribe, /events,/unsubscribe) (GET / POST files)
```

### 1.3 Three key ideas

- **Publishers and subscribers are just clients**: They are the same kind of program; the role depends only on **which endpoint this request calls**.
- **Publishers and subscribers never talk to each other**: Both connect only to the server and do not know about each other.
- **The broker never contacts a subscriber**: Every connection is opened by a client. A message is delivered as the **response to the subscriber's pending `GET /events`**

### 1.4 Identifiers

- **Topic**: identified by its name (a string such as `"news"`). Topics have no `id`.
- **Subscriber**: identified by an integer `id`. Every `POST /subscribe` creates a new subscriber and assigns the next value of `nextId`.

An `id` represents one subscription, not one client. If a client subscribes to two topics, or to the same topic twice, it receives a different id each time. Because HTTP is stateless, the server cannot tell whether two requests come from the same client. The `id` is how a client identifies itself: it must include the `id` in every later `/events` and `/unsubscribe` request.

---

## 2. Connections

### Common lifecycle

**Each connection carries exactly one request** and is closed afterwards (`Connection: close`). 

```
Client connects
  │
  ▼
main goroutine:
  listener.Accept()
  slots <- struct{}{}       take a slot (waits if all 10 are taken)
  go func() { … }()         start a handler goroutine
  back to Accept            does not wait for the handler to finish
  │
  ▼
handler goroutine:
  defer <-slots             return the slot at the end
  defer conn.Close()        close the connection at the end
  http.ReadRequest()        parse error → 400
  method is GET or POST?    no → 501
  route by path:
    /subscribe, /publish, /events, /unsubscribe   → broker handler
    anything else           → file server
  writeResponse()
  end                       → conn.Close() → <-slots
```

**Why broker routing must come before the file checks:** broker paths have no file extension. If the extension check ran first, every broker request would get `400`.


### The six kinds of connections

| #   | Request                          | Role         | Changes server state?          |
| --- | -------------------------------- | ------------ | ------------------------------ |
| A   | `GET /<file>`                    | Plain client | ❌                              |
| B   | `POST /<file>`                   | Plain client | ✅ files                        |
| C   | `POST /subscribe?topic=X`        | Subscriber   | ✅ adds a subscriber            |
| D   | `POST /publish?topic=X`          | Publisher    | ✅ adds messages to queues      |
| E   | `GET /events?topic=X&id=N`       | Subscriber   | ✅ takes a message from a queue |
| F   | `POST /unsubscribe?topic=X&id=N` | Subscriber   | ✅ removes a subscriber         |

### 2.1 Flow of each connection

#### A. `GET /<file>`: read a file

```
filename = path without the leading /
→ fs.ValidPath(filename)          is the path safe?
→ contentTypes[path.Ext(...)]     is the extension supported?
→ root.ReadFile(filename)         read the whole file
→ 200 + matching Content-Type
```

#### B. `POST /<file>`: store a file

```
(same path and extension checks)
→ saveUpload:
    root.OpenRoot(target directory)
    create temp file .upload_<random>
    io.Copy(temp file, body)       stream: read a chunk, write a chunk
    Close
    Rename(temp file → target name) atomic replace
→ 200 File saved
```

#### C. `POST /subscribe?topic=X`

```
check method and topic
→ mu.Lock
   nextId++ → new id
   create topics[X] if it does not exist
   topics[X][id] = new Subscriber{ queue(10), polling: false, done }
→ mu.Unlock
→ 201 {"id": N}
```

#### D. `POST /publish?topic=X`

```
check method and topic
→ io.ReadAll(body)
→ mu.Lock
   lastSequence[X]++ → sequence number of this message (Number of `publishes`)
   for each subscriber in topics[X]: (Inside queue: Message)
       queue not full → enqueue
       queue full     → skip (never wait)
→ mu.Unlock
→ 200 {"delivered": number actually enqueued}
```

#### E. `GET /events?topic=X&id=N`

```
check method, topic and id
→ mu.Lock
   find the subscriber (not found: 404)
   already polling? (yes: 409)
   polling = true
→ mu.Unlock
→ message already queued? (non-blocking)
     yes → 200 + message (fast path)
     no  ↓
→ <-slots                         return the slot
→ select, wait for:
     ⓐ a message       → received = true
     ⓑ 10 seconds pass
     ⓒ done is closed  (unsubscribed)
→ slots <- …                      take a slot again
→ done closed?  → 404
→ received?     → 200 + message
→ otherwise     → 204 No Content
→ at the end (defer): polling = false
```
- **Long polling.** A subscriber asks for messages with `GET /events`. 
	- If none is queued, the server holds the request open until a message arrives or 10 seconds pass, instead of answering "nothing" right away. 
	- The `polling` flag marks that one `/events` is waiting, so a second one gets `409`.
- **Why unlock before waiting.** `mu` is shared by the whole broker. Holding it while waiting
  would block every `publish`, so the message being waited for could never arrive.
- **`<-slots` / `slots <- struct{}{}`.** Return/take one of the server's 10 slots. A waiting
  poll does no work, so it gives its slot back and takes again before writing the response.
- **`done` is closed.** `unsubscribe` calls `close(done)`, which immediately wakes a waiting poll
  so it returns `404` without waiting for the timeout. `done` is checked once more before
  responding, in case the unsubscribe arrived together with a message.
- **`204 No Content`.** Reached only when the subscription is still valid, and no message arrived, i.e. the 10-second timeout. The client should send `GET /events` again.

#### F. `POST /unsubscribe?topic=X&id=N`

```
check method, topic and id
→ mu.Lock
   find the subscriber (not found: 404)
   delete it from topics[X]
   close(done)                    wake up a waiting /events
→ mu.Unlock
→ 200 Unsubscribed
```

#### A complete pub-sub scenario

```
time ──────────────────────────────────────────────────────────────────────────▶

Subscriber  ① POST /subscribe ──▶ 201 {"id":3}
            ② GET /events id=3 ─────── waiting (holds no slot) …… ──▶ 200 "hello"
                                                      ▲
Publisher                        ③ POST /publish "hello" ─▶ 200 {"delivered":1}
	                                │
Broker                              └── enqueue to subscriber 3 → wakes ②'s select

Subscriber  ④ GET /events id=3 ── 10 s, nothing published ──▶ 204
            ⑤ GET /events id=3 ── waiting … ──▶ 404
                                     ▲
Subscriber  ⑥ POST /unsubscribe id=3 ┘ ──▶ 200 (close(done) wakes ⑤)
```

---

### 2.2 Errors for each connection

#### Common to every connection

| Situation                                           | Result                                                                                         |
| --------------------------------------------------- | ---------------------------------------------------------------------------------------------- |
| Not valid HTTP (e.g. `GARBAGE`)                     | **400** Malformed HTTP request                                                                 |
| Method other than GET / POST (e.g. `PUT`, `DELETE`) | **501** Method not implemented                                                                 |
| All 10 slots taken                                  | **Not an error**: the connection waits in the backlog until a slot is free; nothing is dropped |
| Client connects but never sends data                | Holds a slot; **no timeout** (weakness)                                                        |
| Client disconnects before the response              | `writeResponse` fails; the error is only logged on the server                                  |
| `Accept()` fails (e.g. out of file descriptors)     | **The whole server exits** (`os.Exit`)                                                         |

#### Files

| Situation                                                                      | GET                       | POST                                                          |
| ------------------------------------------------------------------------------ | ------------------------- | ------------------------------------------------------------- |
| Invalid path (`..`, empty, `//`, trailing `/`)                                 | 400 Invalid file path     | 400 Invalid file path                                         |
| Unsupported extension (`.png`, none, `.TXT`)                                   | 400 Unsupported file type | 400 Unsupported file type                                     |
| File does not exist                                                            | **404** File not found    | —                                                             |
| Cannot read (it is a directory, no permission)                                 | 400 could not read file   | —                                                             |
| Target directory missing, client disconnects mid-upload, target is a directory | —                         | 400 Could not save file                                       |
| Many clients POST the same file at once                                        | —                         | **Not an error**: last writer wins; every version is complete |

#### Broker

| Situation                                          | subscribe | publish                    | events                  | unsubscribe |
| -------------------------------------------------- | --------- | -------------------------- | ----------------------- | ----------- |
| Wrong method                                       | 501       | 501                        | 501                     | 501         |
| Missing topic                                      | 400       | 400                        | 400                     | 400         |
| `id` missing, not a number, or ≤ 0                 | —         | —                          | 400                     | 400         |
| Unknown subscriber, or topic and `id` do not match | —         | —                          | **404**                 | **404**     |
| Reading the body fails                             | —         | 400                        | —                       | —           |
| A subscriber's queue is full                       | —         | **Not an error**: skipped; | —                       | —           |
| Another `/events` is already waiting for this id   | —         | —                          | **409**                 | —           |
| No message within 10 seconds                       | —         | —                          | **204** (normal result) | —           |
| Unsubscribed while waiting                         | —         | —                          | **404**                 | —           |

Two orderings of unsubscribe and `/events`:

| Ordering                               | Where it is caught                      | Result                                                     |
| -------------------------------------- | --------------------------------------- | ---------------------------------------------------------- |
| Unsubscribe first, then `/events`      | The initial `subscriber == nil` check   | 404 immediately                                            |
| `/events` is waiting, then unsubscribe | The waiting `select` (`done` is closed) | Woken up, 404 immediately; no need to wait for the timeout |

### 2.3 Data structures and mechanisms

#### Concurrency control

|Mechanism|Data structure|Problem it solves|
|---|---|---|
|One goroutine per connection|`go func() { … }()`|A slow client does not block others; main hands off and returns to `Accept` immediately|
|At most 10 handlers working|`slots := make(chan struct{}, 10)`|Prevents overload; extra requests wait (counting semaphore)|
|Long polls return their slot|`<-slots` … `slots <- struct{}{}`|Ten waiting `/events` cannot lock out `publish` and cause mutual waiting|
|Always release, always close|`defer <-slots`, `defer conn.Close()`|No leaks, whichever `return` the handler leaves from|

#### File safety and atomicity

| Mechanism             | Data structure                        | Problem it solves                                                                                                     |
| --------------------- | ------------------------------------- | --------------------------------------------------------------------------------------------------------------------- |
| Path format check     | `fs.ValidPath`                        | Rejects `../` and other suspicious paths (first line of defense, string-only)                                         |
| Confinement to `www`  | `os.Root`                             | Cannot escape even through a symlink (second line of defense)                                                         |
| Extension allow-list  | `contentTypes map[string]string`      | Decides both **whether a file may be served** and its **Content-Type**                                                |
| Random temp file name | `".upload_" + rand.Text()` + `O_EXCL` | Concurrent uploads never write to the same file; the temp extension is not on the allow-list, so it cannot be fetched |
| Atomic replace        | `dir.Rename`                          | Readers only ever see a complete old or new version                                                                   |
| Cleanup on failure    | `defer dir.Remove(tempName)`          | No half-written temp files are left behind                                                                            |

#### Broker state

| Mechanism                 | Data structure                                                                    | Problem it solves                                                                                |
| ------------------------- | --------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------ |
| Protect shared data       | `Broker.mu sync.Mutex`                                                            | Concurrent map reads and writes crash the program                                                |
| Unique subscriber id      | `Broker.nextId int`                                                               | HTTP is stateless, so a client needs a ticket to identify itself; `ids` are unique across topics |
| Find subscribers by topic | `topics map[string]map[int]*Subscriber`                                           | `publish` iterates only one topic; `events` / `unsubscribe` find a subscriber with two lookups   |
| Message numbering         | `lastSequence map[string]int`, `Message.Sequence`                                 | Each topic's messages get a sequence number (not yet returned to clients)                        |
| Consistent order          | The mutex covers "assign number + enqueue to every subscriber"; channels are FIFO | With concurrent publishers, every subscriber sees the same order                                 |

#### Per subscriber

| Mechanism            | Data structure                       | Problem it solves                                                                                |
| -------------------- | ------------------------------------ | ------------------------------------------------------------------------------------------------ |
| Bounded queue        | `message chan Message` (capacity 10) | The bounded queue required by the spec; also the pipe between `publish` and `events`             |
| Non-blocking publish | `select { case …: default: }`        | A subscriber that never reads cannot block the publisher or the broker                           |
| No duplicate polls   | `polling bool` + mutex               | Two `/events` competing for one queue would lose messages and break order → 409                  |
| Unsubscribe signal   | `done chan struct{}` + `close()`     | A waiting `/events` learns about the unsubscribe immediately instead of after the timeout        |

