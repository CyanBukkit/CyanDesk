.PHONY: test build admin

test:
	go test ./...

admin:
	cd admin-web && npm install && npm run build

build: admin
	CGO_ENABLED=0 go build -trimpath -o bin/cyandesk ./cmd/cyandesk
