DESTDIR ?=
CONFIGDIR = $(DESTDIR)/etc/xdg/elephant/providers

GO_BUILD_FLAGS = -buildvcs=false -buildmode=plugin -trimpath
PLUGIN_NAME = unicode.so

.PHONY: all build install uninstall clean

all: build

build:
	go build $(GO_BUILD_FLAGS)

install: build
	# Install plugin
	install -Dm 755 $(PLUGIN_NAME) $(CONFIGDIR)/$(PLUGIN_NAME)

uninstall:
	rm -f $(CONFIGDIR)/$(PLUGIN_NAME)

clean:
	go clean
	rm -f $(PLUGIN_NAME)

dev-install: install

help:
	@echo "Available targets:"
	@echo "  all       - Build the plugin (default)"
	@echo "  build     - Build the plugin"
	@echo "  install   - Install the plugin"
	@echo "  uninstall - Remove installed plugin"
	@echo "  clean     - Clean build artifacts"
	@echo "  help      - Show this help"
	@echo ""
	@echo "Variables:"
	@echo "  DESTDIR   - Destination directory for staged installs"
	@echo ""
	@echo "Note: This builds a Go plugin (.so file) for elephant"
