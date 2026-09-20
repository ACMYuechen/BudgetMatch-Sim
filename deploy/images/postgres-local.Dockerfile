# Preserve the existing PostgreSQL 16 Alpine data format/libc; add only pgvector.
# Override POSTGRES_BASE explicitly when upgrading, after backing up the volume.
ARG POSTGRES_BASE=postgres:16-alpine@sha256:16bc17c64a573ef34162af9298258d1aec548232985b33ed7b1eac33ba35c229
FROM ${POSTGRES_BASE} AS vector-build

ARG APK_MIRROR=https://dl-cdn.alpinelinux.org/alpine
RUN sed -i "s#https://dl-cdn.alpinelinux.org/alpine#${APK_MIRROR}#g" /etc/apk/repositories && \
    apk add --no-cache build-base
ADD --checksum=sha256:10bf9938906e5d643bbc4a7eea104b6f57ba4898e5b76b20e60484ea1d5a7f8f \
    https://codeload.github.com/pgvector/pgvector/tar.gz/refs/tags/v0.8.6 /tmp/pgvector.tar.gz
RUN tar -xzf /tmp/pgvector.tar.gz -C /tmp && \
    make -C /tmp/pgvector-0.8.6 -j2 with_llvm=no OPTFLAGS="" && \
    make -C /tmp/pgvector-0.8.6 with_llvm=no DESTDIR=/vector-install install

FROM ${POSTGRES_BASE}
COPY --from=vector-build /vector-install/usr/local/lib/postgresql/ /usr/local/lib/postgresql/
COPY --from=vector-build /vector-install/usr/local/share/postgresql/extension/ /usr/local/share/postgresql/extension/
COPY --from=vector-build /tmp/pgvector-0.8.6/LICENSE /usr/local/share/doc/pgvector/LICENSE
