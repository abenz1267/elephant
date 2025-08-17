#!/usr/bin/env bash
set -e

echo "🐘 Installing Elephant with all providers..."

# Ensure directories exist
mkdir -p ~/.local/bin
mkdir -p ~/.config/elephant/providers

echo "📦 Building elephant..."
nix develop -c go build -o ~/.local/bin/elephant cmd/elephant.go

echo "🔌 Building providers..."
nix develop -c ./build-providers.sh

echo "✅ Installation complete!"
echo ""
echo "🚀 Usage:"
echo "  Add ~/.local/bin to your PATH if not already there:"
echo "    export PATH=\"\$HOME/.local/bin:\$PATH\""
echo ""
echo "  Start elephant service:"
echo "    elephant --debug"
echo ""
echo "  Query providers (in another terminal):"
echo "    elephant listproviders"
echo "    elephant query \"files;Documents;5;false\""
echo "    elephant query \"desktopapplications;chrome;3;false\""
echo "    elephant query \"calc;2+2;1;false\""
echo ""
echo "📖 Check the README for more usage examples and API documentation."