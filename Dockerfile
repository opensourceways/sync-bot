FROM openeuler/go:1.24.1-oe2403lts AS BUILDER
RUN dnf -y install git gcc

ARG USER
ARG PASS
RUN echo "machine github.com login $USER password $PASS" > ~/.netrc

# build binary
WORKDIR /opt/source
COPY . .
RUN go env -w GO111MODULE=on && \
    go env -w CGO_ENABLED=1 && \
    go build -a -o sync-bot -buildmode=pie -ldflags "-s -linkmode 'external' -extldflags '-Wl,-z,now'" .

# copy binary config and utils
FROM openeuler/openeuler:24.03-lts
RUN dnf -y update && \
    dnf in -y shadow git && \
    groupadd -g 1000 robot && \
    useradd -u 1000 -g robot -s /bin/bash -m robot && \
    dnf clean all

USER robot

COPY --chown=robot --from=BUILDER /opt/source/sync-bot /opt/app/sync-bot

ENTRYPOINT ["/opt/app/sync-bot"]
