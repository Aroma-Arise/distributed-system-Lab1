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

	// open the www directory (File Descriptor in Linux)
	root, err := os.OpenRoot("www")
	if err != nil {
		fmt.Fprintln(os.Stderr, "Could not open www directory:", err)
		os.Exit(1)
	}
	defer root.Close()

	// socket() -> bind(":8080") -> listen()
	listener, err1 := net.Listen("tcp", ":"+port) // listener: socket waiting for connection
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
		// Wait for and accept the next client connection (loops forever).
		// conn is a net.Conn: an interface for a two-way connection that wraps the socket's file descriptor.
		conn, err2 := listener.Accept() // fails if the listener is closed or the process runs out of file descriptors
		if err2 != nil {
			fmt.Fprintln(os.Stderr, "Error accepting connection: ", err2)
			os.Exit(1)
		}

		// If the channel is full, this will block until a slot is available
		slots <- struct{}{}
		
		// Handle the connection in its own goroutine so the main loop can go straight back to Accept.
		// Without it, one slow client (e.g. a long poll) would block every other connection.
		go func() {
			defer func() { <-slots }()					// release the slot when the handler returns, even on panic
			handleConnection(conn, root, broker, slots) 
		}()																						
	}
}

func handleConnection(conn net.Conn, root *os.Root, broker *Broker, slots chan struct{}) {
	defer conn.Close()

	// Buffer the connection so ReadRequest can read line by line without one syscall per byte.
	// The body stays in this reader and is later read through req.Body.
	reader := bufio.NewReader(conn)			
	
	// Parse the request line and headers (stops at the blank line; the body is not read yet).
	req, err := http.ReadRequest(reader) 	// Fails on a malformed request line or header, an invalid HTTP version

	// deal with error not to parse
	if err != nil {
		writeResponse(conn, http.StatusBadRequest, "text/plain",
			[]byte("Malformed HTTP request\n"))
		return
	}

	// check "get" method and "post" method 
	if req.Method != http.MethodGet && req.Method != http.MethodPost {
		writeResponse(conn, http.StatusNotImplemented, "text/plain",
			[]byte("Method not implemented\n"))
		return
	}

	// Check broker activities ( Each case returns, so broker requests never reach the file path and extension checks below.)
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

	// get filename from URL path (strings.TrimPrefix：strip the leading "/")
	// (No bytes are copied: filename shares req.URL.Path's underlying data, starting one byte later.)
	filename := strings.TrimPrefix(req.URL.Path, "/")
	if !fs.ValidPath(filename) {
		writeResponse(conn, http.StatusBadRequest, "text/plain",
			[]byte("Invalid file path\n"))
		return
	}

	// check file type is supported (path.Ext returns the extension, from the last "." to the end.)
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

// writeResponse writes a complete HTTP/1.0 response (status line, headers, blank line, body) to conn.
func writeResponse(conn net.Conn, status int, contentType string, body []byte) {
	_, err := fmt.Fprintf(conn, 		// Fprintf writes to any io.Writer; here that is the client connection.
		"HTTP/1.0 %d %s\r\n"+	
			"Content-Type: %s\r\n"+
			"Content-Length: %d\r\n"+ 	// tells the client where the body ends
			"Connection: close\r\n"+
			"\r\n"+						// HTTP requires "\r\n" line endings and a blank line between the headers and the body.
			"%s",
		status, http.StatusText(status), contentType, len(body), body, // writes []byte, so binary files (images) stay intact
	)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Could not write response:", err)
	}
}

// It writes to a temp file in the target directory, then renames it over the target
// The target directory must already exist under www/. Any error makes the caller return 400.
func saveUpload(root *os.Root, filename string, body io.ReadCloser) error {
	dir, err := root.OpenRoot(path.Dir(filename)) // 打開存放 filename 的 dir ( up/new.txt -> up )
	if err != nil {
		return err
	}
	defer dir.Close()

	// create a temporary file to write the uploaded content
	tempName := ".upload_" + rand.Text()
	tempFile, err := dir.OpenFile(
		tempName,
		os.O_WRONLY|os.O_CREATE|os.O_EXCL, 	// O_EXCL: fail instead of reusing an existing file
		0o600, 								// owner read/write only
	)

	if err != nil {
		return err
	}
	defer dir.Remove(tempName)

	// copy the uploaded content to the temporary file (stream the body to disk in chunks instead of loading it into memory.)
	_, copyErr := io.Copy(tempFile, body) 
	closeErr := tempFile.Close() 					// close before checking errors so the file is always closed
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}

	return dir.Rename(tempName, path.Base(filename)) // atomic replace
}

// build command:
// go build -o http_server . 	// Compile (.go files in the current directory + imported standard libraries and their extensions + Go runtime)
// ./http_server 8080 			// Note: ./http_server is also considered one of the parameters.

// test command:
// curl -v http://localhost:8080
