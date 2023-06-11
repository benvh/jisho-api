all: jisho-api

clean:
	rm -f jisho-api

jisho-api: main.go
	go build -o $@ ./main.go

.PHONY: all clean