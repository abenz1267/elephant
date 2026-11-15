ARG GO_VERSION=1.27.1
FROM golang:${GO_VERSION}-bookworm AS build

WORKDIR /src
ENV CGO_ENABLED=1

COPY go.mod go.sum ./
RUN go mod download

COPY . .
# Go plugins and the executable must use the same toolchain and build flags.
RUN mkdir -p /out/providers && \
    go build -mod=readonly -buildvcs=false -trimpath -ldflags="-s -w" \
        -o /out/elephant ./cmd/elephant && \
    for provider in ./internal/providers/*; do \
        [ -d "$provider" ] || continue; \
        go build -mod=readonly -buildvcs=false -trimpath -ldflags="-s -w" \
            -buildmode=plugin -o "/out/providers/$(basename "$provider").so" "$provider" || exit 1; \
    done

FROM debian:bookworm-slim AS runtime

# Additional provider-specific commands can be installed in a derived image.
RUN apt-get update && \
    apt-get install -y --no-install-recommends \
        ca-certificates fd-find imagemagick qalc wl-clipboard xdg-utils && \
    rm -rf /var/lib/apt/lists/* && \
    ln -s /usr/bin/fdfind /usr/local/bin/fd && \
    groupadd --gid 1000 elephant && \
    useradd --uid 1000 --gid elephant --create-home elephant && \
    mkdir -p /home/elephant/.config/elephant /home/elephant/.cache/elephant \
        /home/elephant/.local/share/elephant /run/elephant && \
    chown -R elephant:elephant /home/elephant /run/elephant && \
    chmod 0700 /run/elephant

COPY --from=build /out/elephant /usr/local/bin/elephant
COPY --from=build /out/providers/ /usr/local/lib/elephant/providers/

ENV HOME=/home/elephant \
    XDG_CONFIG_HOME=/home/elephant/.config \
    XDG_CACHE_HOME=/home/elephant/.cache \
    XDG_DATA_HOME=/home/elephant/.local/share \
    XDG_RUNTIME_DIR=/run/elephant \
    ELEPHANT_PROVIDER_DIR=/usr/local/lib/elephant/providers

USER elephant
WORKDIR /home/elephant
ENTRYPOINT ["elephant"]
