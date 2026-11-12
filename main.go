package main

import (
	"bufio"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"path"
	"strconv"
	"strings"
)

var contentTypes = map[string]string{
	".html": "text/html",
	".txt":  "text/plain",
	".gif":  "image/gif",
	".jpeg": "image/jpeg",
	".jpg":  "image/jpeg",
	".css":  "text/css",
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "Usage: http_server <port>")
		os.Exit(1)
	}
	port := os.Args[1]

	// open the www directory
	root, err := os.OpenRoot("www")
	if err != nil {
		fmt.Fprintln(os.Stderr, "Could not open www directory:", err)
		os.Exit(1)
	}
	defer root.Close()

	listener, err1 := net.Listen("tcp", ":"+port)
	if err1 != nil {
		fmt.Fprintln(os.Stderr, "Error starting server: ", err1)
		os.Exit(1)
	}

	defer listener.Close()

	fmt.Println("Starting server listening on port: ", port)

	// create a channel to limit the number of concurrent connections, maximum 10
	slots := make(chan struct{}, 10)

	broker := newBroker()

	for {
		// listen to evert connection, endless loop
		conn, err2 := listener.Accept()
		if err2 != nil {
			fmt.Fprintln(os.Stderr, "Error accepting connection: ", err2)
			os.Exit(1)
		}

		// if the channel is full, this will block until a slot is available
		slots <- struct{}{}

		go func() {
			defer func() { <-slots }()
			handleConnection(conn, root, broker, slots)
		}()
	}
}

func handleConnection(conn net.Conn, root *os.Root, broker *Broker, slots chan struct{}) {
	defer conn.Close()

	reader := bufio.NewReader(conn)
	req, err := http.ReadRequest(reader)

	// to deal with error not to parse
	if err != nil {
		writeResponse(conn, http.StatusBadRequest, "text/plain",
			[]byte("Malformed HTTP request\n"))
		return
	}

	// get method and post method
	if req.Method != http.MethodGet && req.Method != http.MethodPost {
		writeResponse(conn, http.StatusNotImplemented, "text/plain",
			[]byte("Method not implemented\n"))
		return
	}

	switch req.URL.Path {
	case "/publish":
		handlePublish(conn, req, broker)
		return
	case "/subscribe":
		handleSubscribe(conn, req, broker)
		return
	case "/events":
		handleEvents(conn, req, broker, slots)
		return
	case "/unsubscribe":
		handleUnsubscribe(conn, req, broker)
		return
	}

	// get filename from URL path
	filename := strings.TrimPrefix(req.URL.Path, "/")
	// validate filename
	if !fs.ValidPath(filename) {
		writeResponse(conn, http.StatusBadRequest, "text/plain",
			[]byte("Invalid file path\n"))
		return
	}

	// check file type is supported
	extension := path.Ext(filename)
	contentType, supported := contentTypes[extension]
	if !supported {
		writeResponse(conn, http.StatusBadRequest, "text/plain",
			[]byte("Unsupported file type\n"))
		return
	}

	// handle POST request to save file
	if req.Method == http.MethodPost {
		if err := saveUpload(root, filename, req.Body); err != nil {
			fmt.Fprintln(os.Stderr, "Error saving uploaded file:", err)
			writeResponse(conn, http.StatusBadRequest, "text/plain",
				[]byte("Could not save file\n"))
			return
		}

		writeResponse(conn, http.StatusOK, "text/plain", []byte("File saved\n"))
		return
	}

	// check if file exists and read it
	body, err := root.ReadFile(filename)
	if errors.Is(err, os.ErrNotExist) {
		writeResponse(conn, http.StatusNotFound, "text/plain",
			[]byte("File not found\n"))
		return
	}
	if err != nil {
		writeResponse(conn, http.StatusBadRequest, "text/plain",
			[]byte("could not read file\n"))
		return
	}

	writeResponse(conn, http.StatusOK, contentType, body)
}

func writeResponse(conn net.Conn, status int, contentType string, body []byte) {
	_, err := fmt.Fprintf(conn,
		"HTTP/1.0 %d %s\r\n"+
			"Content-Type: %s\r\n"+
			"Content-Length: %d\r\n"+
			"Connection: close\r\n"+
			"\r\n"+
			"%s",
		status, http.StatusText(status), contentType, len(body), body,
	)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Could not write response:", err)
	}
}

func saveUpload(root *os.Root, filename string, body io.ReadCloser) error {
	dir, err := root.OpenRoot(path.Dir(filename))
	if err != nil {
		return err
	}
	defer dir.Close()

	// create a temporary file to write the uploaded content
	tempName := ".upload_" + rand.Text()
	tempFile, err := dir.OpenFile(
		tempName,
		os.O_WRONLY|os.O_CREATE|os.O_EXCL,
		0o600,
	)

	if err != nil {
		return err
	}
	defer dir.Remove(tempName)

	// copy the uploaded content to the temporary file
	_, copyErr := io.Copy(tempFile, body)
	closeErr := tempFile.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}

	return dir.Rename(tempName, path.Base(filename))
}

func handleUnsubscribe(conn net.Conn, req *http.Request, broker *Broker) {
	if req.Method != http.MethodPost {
		writeResponse(conn, http.StatusNotImplemented, "text/plain",
			[]byte("Method not implemented\n"))
		return
	}

	query := req.URL.Query()
	topic := query.Get("topic")
	id, err := strconv.Atoi(query.Get("id"))
	if topic == "" || err != nil || id <= 0 {
		writeResponse(conn, http.StatusBadRequest, "text/plain",
			[]byte("A topic and a positive subscriber ID are required\n"))
		return
	}

	if !broker.unsubscribe(topic, id) {
		writeResponse(conn, http.StatusNotFound, "text/plain",
			[]byte("Subscriber not found\n"))
		return
	}

	writeResponse(conn, http.StatusOK, "text/plain", []byte("Unsubscribed\n"))
}

// build command:
// go build -o http_server .
// ./http_server 8080

// test command:
// curl -v http://localhost:8080
