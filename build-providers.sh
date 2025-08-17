#!/usr/bin/env bash
set -e

echo "Building elephant providers..."

# Create providers directory
mkdir -p ~/.config/elephant/providers

# Available providers
PROVIDERS=(
  "files"
  "desktopapplications" 
  "calc"
  "runner"
  "clipboard"
  "symbols"
  "websearch"
  "menus"
  "providerlist"
)

echo "Building providers: ${PROVIDERS[@]}"

for provider in "${PROVIDERS[@]}"; do
  if [[ -d "./internal/providers/$provider" ]]; then
    echo "Building $provider..."
    go build -buildmode=plugin -o ~/.config/elephant/providers/$provider.so ./internal/providers/$provider
    echo "✓ Built $provider.so"
  else
    echo "⚠ Provider $provider not found, skipping"
  fi
done

echo ""
echo "Providers built successfully!"
echo "Run 'elephant listproviders' to see installed providers"
echo "Run 'elephant --debug' to start the service"