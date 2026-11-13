# Go HTTP Lab 1 演示测试

需要 Python 3.8+ 和已启动的 Lab 1 服务器。测试脚本只使用 Python 标准库。每个脚本独立创建它需要的订阅或测试文件，打印逐项 PASS，失败时打印原因并以非零状态退出。测试默认访问 `127.0.0.1:8081`，原生 Go 服务器可以用 `--port 8080`。

## 首次准备

1. 解压后，把整个 `lab1-demo-tests/` 文件夹放在项目根目录，与 `Dockerfile` 和 `go.mod` 同级。
2. 把 GET 测试文件放进服务器的 `www/` 下。GET 测试直接读取预置文件，因此它可以在 POST 出错时独立执行。

   ```bash
   mkdir -p www/lab1-demo-fixtures
   cp lab1-demo-tests/fixtures/* www/lab1-demo-fixtures/
   docker build -t http-lab .
   docker run --rm --name http-lab-demo -p 127.0.0.1:8081:8080 http-lab
   ```

3. 保留容器终端，在另一个终端进入项目根目录。比如运行以下命令：

   ```bash
   python3 lab1-demo-tests/01_get_file_types.py
   python3 lab1-demo-tests/02_post_10_concurrent.py
   ```

   如果本地服务器用 `go run . 8080` 启动，测试命令示例为：

   ```bash
   python3 lab1-demo-tests/01_get_file_types.py --port 8080
   ```

## 单独演示

| 脚本 | 预期现象 |
| --- | --- |
| `01_get_file_types.py` | HTML、TXT、CSS、GIF、JPEG、JPG 都返回 200，验证 MIME 类型和原始字节 |
| `02_post_10_concurrent.py` | 10 个客户端同时发 POST，随后用 GET 验证每个文件的内容 |
| `03_http_errors.py` | PUT 501，GET/POST 不支持的扩展名 400，合法扩展名但文件不存在 404，格式错误的 HTTP header 400 |
| `04_subscribe.py` | 两次订阅均返回 201 和不同的正整数 ID，缺少 topic 返回 400，GET 返回 501 |
| `05_publish_and_events.py` | 同 topic 的两个订阅者都收到消息，按顺序取出，topic 相互隔离，新订阅者收不到订阅前的消息。8 个并发发布者的消息在两个订阅者看来顺序一致 |
| `06_events_poll_success.py` | 先发 GET 并观察等待，再发 POST。等待中的 GET 收到 200 和原始消息正文 |
| `07_events_poll_timeout.py` | 始终无人发布，约 10 秒后返回 204 且正文为空 |
| `08_events_errors.py` | 缺 topic、缺 ID、ID 非数字或非正数返回 400，合法但查不到的订阅返回 404，POST 返回 501 |
| `09_unsubscribe.py` | 取消订阅唤醒等待中的 GET，旧 ID 再请求或再取消时返回 404，其余订阅者仍可收消息 |
| `10_post_atomic.py` | 两个 POST 同时写同名文件，三个检查点的 GET 分别读到完整旧版、完整 A 版、完整 B 版 |
| `11_concurrency_limit.py` | 9 个上传等待时探测 GET 能返回，10 个上传等待时第 11 个请求要等一个名额释放 |
| `12_longpoll_slots.py` | 10 个长轮询等待时，发布请求仍能进入，10 个订阅者都收到消息 |
| `13_queue_full.py` | 用当前实现的 16 条队列容量填满一个订阅者，额外发布仍能送达另一位，满队列里原有的消息依序保留 |

运行单个脚本时，把文件名换成表中的目标文件名。若要一次执行全部：

```bash
for test_file in lab1-demo-tests/[0-9][0-9]_*.py; do
  python3 "$test_file" || break
done
```

`06`、`09`、`10`、`11`、`12` 和 `13` 会同时打开多个连接，由脚本安排发送顺序。所有自动测试都只需要 **一个测试终端**。加上运行服务器的终端，总共两个。也可以用 `docker run -d --rm --name http-lab-demo -p 127.0.0.1:8081:8080 http-lab` 在后台启动，演示完成后用 `docker stop http-lab-demo` 停止。

## 用多个终端手动展示长轮询

只有想现场用 curl 展示「一个客户端等待，另一个客户端发布」时，才需要 **一个服务器终端加两个客户端终端**。先在任一客户端终端订阅：

```bash
curl -i -X POST 'http://127.0.0.1:8081/subscribe?topic=live-demo'
```

把下面的 `1` 改成响应 JSON 中的真实 ID。在客户端 A 运行：

```bash
curl -i 'http://127.0.0.1:8081/events?topic=live-demo&id=1'
```

在客户端 B 的终端，在 10 秒内运行：

```bash
curl -i --data-binary 'Hello from the publisher' \
  'http://127.0.0.1:8081/publish?topic=live-demo'
```

客户端 A 应该收到 200 和消息。再发一次 GET，保持不发布，就能看到 204。要演示取消时唤醒，用下面的命令替换客户端 B 的 publish 命令，同样替换 ID：

```bash
curl -i -X POST 'http://127.0.0.1:8081/unsubscribe?topic=live-demo&id=1'
```

## 测试约定

- 脚本按当前设计检查 `/events` 的正文是否等于发布的原始字节。并发发布测试比较实际交付顺序，不要求序号出现在 HTTP 响应里。
- `07` 给 10 秒轮询留出 9.5–15 秒的测量窗口。`13` 使用当前 broker 的队列容量 16。如果项目改了这些值，对应调整脚本。
- 测试上传以 `lab1-` 开头的唯一文件名。建议用上面的临时容器做演示，停止带 `--rm` 的容器后，测试上传随容器移除。在原生 Go 服务器上运行时，这些上传文件会留在 `www/`。
