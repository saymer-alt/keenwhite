.PHONY: test vet build validate doctor crossbuild

test:
	go test ./...
vet:
	go vet ./...
build:
	go build -o bin/keenwhite ./cmd/keenwhite
validate:
	go run ./cmd/keenwhite validate -catalog examples/providers.json
doctor:
	go run ./cmd/keenwhite doctor -catalog examples/providers.json
crossbuild:
	mkdir -p dist
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -o dist/keenwhite-linux-arm64 ./cmd/keenwhite
	CGO_ENABLED=0 GOOS=linux GOARCH=mipsle GOMIPS=softfloat go build -trimpath -o dist/keenwhite-linux-mipsle ./cmd/keenwhite
