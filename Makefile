.PHONY: check openapi

check:
	@echo "Checking code with golangci-lint..."
	golangci-lint run
	@echo "Checking code with go vet..."
	go vet ./...
	@echo "Checking code with go fmt..."
	@go fmt ./...
	@echo "Checking code with go mod tidy..."
	@go mod tidy
	@echo "Done."

openapi:
	@echo "Generating OpenAPI spec..."
	@swag fmt
	@swag init -d cmd --v3.1
	@echo "Done."