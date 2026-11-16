# Multi-stage build: each FROM starts a new stage.
# Only the last stage becomes the final image.

# ---- Stage 1: build ----
# Linux image with the Go 1.27 toolchain
FROM golang:1.27 AS build
# Create /src and run later commands from there
WORKDIR /src
# Copy the repo into /src (First .: build context(Folder when docker build), Second .: Current folder in image)
COPY . .
# Static binary: needs no C libraries at runtime (CGO_ENABLED=1	:It needs to look for `libc.so` in the system during execution.)
RUN CGO_ENABLED=0 go build -o /http_server .

# ---- Stage 2: runtime ----
# Empty image: no shell, no OS tools, only what we copy in
FROM scratch
# The server opens "www" relative to this directory
WORKDIR /app
# COPY --from=<stage> <path in that stage> <path in this stage>
COPY --from=build /http_server ./http_server
COPY --from=build /src/www/ ./www/

# Documentation only; the port is published with -p at run time
EXPOSE 8080
# Program to run when the container starts
ENTRYPOINT ["/app/http_server"]
# Default argument (the port); replaced by any args after the image name
CMD ["8080"]

# ---- Usage ----
# docker build -t http-lab .
# docker run -d --rm --name http-lab-demo -p 127.0.0.1:8081:8080 http-lab
#
# docker run [Docker options] <image> [args for the program, replacing CMD]
#   All Docker options (-p, -d, --rm, --name, ...) must come BEFORE the image name;
#   everything after it is passed to the container.
#
# -p 127.0.0.1:8081:8080 = <host IP>:<host port>:<container port>
#   127.0.0.1 accepts connections from this machine only; open http://127.0.0.1:8081
#
# Run on another port (the server must listen on the same port as the container side of -p):
#   docker run --rm -p 127.0.0.1:8070:8070 http-lab 8070