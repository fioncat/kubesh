all:
	CGO_ENABLED=0 go build -o out/kubesh main.go
