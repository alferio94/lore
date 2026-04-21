# Makefile — lore build helpers
#
# Targets:
#   build-frontend   Build the Angular SPA and copy output into admin_dist/
#   clean            Remove generated frontend assets from admin_dist/ (keeps index.html)
#   dev              Run lore server in dev-auth mode (Go only; start ng serve separately)

FRONTEND_DIR ?= ../lore-front
ADMIN_DIST   := internal/admin/admin_dist
NG_OUT       := $(FRONTEND_DIR)/dist/lore-front/browser

.PHONY: build-frontend clean dev

## build-frontend: compile the Angular SPA and copy output to admin_dist/
build-frontend:
	@if [ ! -d "$(FRONTEND_DIR)" ]; then \
		echo "Error: frontend repo not found at $(FRONTEND_DIR)"; \
		echo "Clone lore-front next to lore, or set FRONTEND_DIR=<path>"; \
		exit 1; \
	fi
	cd $(FRONTEND_DIR) && npm ci && npx ng build --configuration production
	cp -R $(NG_OUT)/. $(ADMIN_DIST)/

## clean: remove generated frontend assets from admin_dist/ (keeps index.html)
clean:
	rm -f $(ADMIN_DIST)/*.js
	rm -f $(ADMIN_DIST)/*.css
	rm -f $(ADMIN_DIST)/*.map
	rm -rf $(ADMIN_DIST)/media
	rm -f $(ADMIN_DIST)/3rdpartylicenses.txt

## dev: run lore with dev-auth enabled (Go only)
dev:
	go run ./cmd/lore serve 7438 --dev-auth
