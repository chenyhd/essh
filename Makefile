.PHONY: build clean

build:
	go build -o essh .

clean:
	rm -f essh
