.PHONY: run test vet fmt tidy

run:
	go run ./cmd/market-lens

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -w .

tidy:
	go mod tidy
