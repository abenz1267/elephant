PREFIX ?= /usr/local
DESTDIR ?=
BINDIR = $(DESTDIR)$(PREFIX)/bin
LICENSEDIR = $(DESTDIR)$(PREFIX)/share/licenses/elephant

# Build configuration
GO_BUILD_FLAGS = -buildvcs=false -trimpath
BUILD_DIR = cmd/elephant

.PHONY: all build install uninstall clean

all: build

build:
	cd $(BUILD_DIR) && go build $(GO_BUILD_FLAGS) -o elephant

install: build
	install -Dm 755 $(BUILD_DIR)/elephant $(BINDIR)/elephant

uninstall:
	rm -f $(BINDIR)/elephant

clean:
	cd $(BUILD_DIR) && go clean
	rm -f $(BUILD_DIR)/elephant

dev-install: PREFIX = /usr/local
dev-install: install

help:
	@echo "Available targets:"
	@echo "  all       - Build the application (default)"
	@echo "  build     - Build the application"
	@echo "  install   - Install the application"
	@echo "  uninstall - Remove installed files"
	@echo "  clean     - Clean build artifacts"
	@echo "  help      - Show this help"
	@echo ""
	@echo "Variables:"
	@echo "  PREFIX    - Installation prefix (default: /usr/local)"
	@echo "  DESTDIR   - Destination directory for staged installs"
