.PHONY: test cover

# Vet and test every package. Synthetic fixtures are generated in-process;
# a clean checkout does not need any external checkpoint.
test:
	go vet ./...
	go test -race -count=1 ./...

cover:
	go test -race -count=1 -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out
