.PHONY: test build-web

test:
	cd backend && go test ./...

build-web:
	cd frontend && npm run build
