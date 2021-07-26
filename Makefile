clean:
	rm -rf tplink2mqtt-mongodb-broker.*
lint:
	golangci-lint run ./internal/... ./cmd/... ./pkg/...
build:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o tplink2mqtt-mongodb-broker.linux_amd64 ./cmd/tplink2mqtt-mongodb-broker
	CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -o tplink2mqtt-mongodb-broker.darwin_amd64 ./cmd/tplink2mqtt-mongodb-broker
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -o tplink2mqtt-mongodb-broker.windows_amd64.exe ./cmd/tplink2mqtt-mongodb-broker

docker:
	docker build -f ./cmd/tplink2mqtt-mongodb-broker/Dockerfile -t shauncampbell/tplink2mqtt-mongodb-broker:local .