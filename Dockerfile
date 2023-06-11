FROM golang:1.20 AS build

WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY *.go ./
RUN GOOS=linux CGO_ENABLED=0 go build -o jisho-api ./main.go

FROM alpine
COPY --from=build /build/jisho-api /jisho-api
ENTRYPOINT ["/jisho-api"]
