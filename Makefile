.PHONY: dev-web dev-api build test fmt tidy clean frontend backend ensure-frontend package-linux-amd64

DATA_DIR ?= ./data
LISTEN_ADDR ?= :8080
DIST_DIR ?= dist
RELEASE_NAME ?= filechuan-linux-amd64

dev-web:
	cd web && npm run dev

dev-api:
	DATA_DIR=$(DATA_DIR) LISTEN_ADDR=$(LISTEN_ADDR) go run .

frontend:
	cd web && npm ci && npm run build
	rm -rf webembed/dist
	mkdir -p webembed/dist
	cp -R web/dist/. webembed/dist/

ensure-frontend:
	@test -f webembed/dist/index.html || (echo "missing webembed/dist; run make frontend" && exit 1)
	@grep -q 'id="root"' webembed/dist/index.html || (echo "invalid frontend embed" && exit 1)
	@if grep -q '前端尚未构建' webembed/dist/index.html 2>/dev/null; then echo "placeholder frontend detected; run make frontend"; exit 1; fi

backend: ensure-frontend
	go build -o bin/filechuan .

build: frontend backend

# 生产包：嵌入前端的 Linux amd64 单二进制 + 配置示例，输出 tar.gz
package-linux-amd64: frontend ensure-frontend
	rm -rf $(DIST_DIR)/$(RELEASE_NAME) $(DIST_DIR)/$(RELEASE_NAME).tar.gz
	mkdir -p $(DIST_DIR)/$(RELEASE_NAME)
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags='-s -w' \
		-o $(DIST_DIR)/$(RELEASE_NAME)/filechuan .
	cp .env.example $(DIST_DIR)/$(RELEASE_NAME)/
	cd $(DIST_DIR) && tar -czf $(RELEASE_NAME).tar.gz $(RELEASE_NAME)
	@echo "packed: $(DIST_DIR)/$(RELEASE_NAME).tar.gz"

test:
	go test ./...
	cd web && npm test -- --run

fmt:
	gofmt -w $$(find . -name '*.go' -not -path './web/*')

tidy:
	go mod tidy

clean:
	rm -rf bin dist web/dist data/temp/*
