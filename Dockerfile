FROM golang:1.27 AS build

WORKDIR /src
COPY . .
RUN CGO_ENABLED=0 go build -o /http_server .

FROM scratch

WORKDIR /app
COPY --from=build /http_server ./http_server
COPY --from=build /src/www/ ./www/

EXPOSE 8080
ENTRYPOINT ["/app/http_server"]
CMD ["8080"]