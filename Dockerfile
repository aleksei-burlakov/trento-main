FROM node:16 AS node-build
WORKDIR /build
ADD . /build
RUN make web-assets

FROM golang:1.16 as go-build
COPY --from=node-build /build /build
WORKDIR /build
RUN make build

#FROM python:3.7-slim AS trento-runner
FROM ubuntu:latest AS trento-runner
RUN apt-get update
RUN apt-get install -y python3 python3-pip && ln -s /usr/bin/python3 /usr/bin/python 
RUN apt-get install -y python3.8-venv golang git vim
RUN /usr/bin/python --version
#RUN /usr/bin/python -m venv /venv \
RUN pip install 'ansible~=4.6.0' 'ara~=1.5.7' 'rpm~=0.0.2' \
    && apt-get update && apt-get install -y --no-install-recommends \
      ssh \
    && apt-get purge -y --auto-remove -o APT::AutoRemove::RecommendsImportant=false \
    && rm -rf /var/lib/apt/lists/*

#ENV PATH="/venv/bin:$PATH"
#ENV PYTHONPATH=/venv/lib/python3.7/site-packages
ENV PATH="/usr/bin:$PATH"
ENV PYTHONPATH=/usr/lib/python3.8/site-packages

# Add Tini
ENV TINI_VERSION v0.19.0
ADD https://github.com/krallin/tini/releases/download/${TINI_VERSION}/tini /tini
RUN chmod +x /tini

COPY --from=go-build /build/trento /app/trento
LABEL org.opencontainers.image.source="https://github.com/trento-project/trento"
#ENTRYPOINT ["/tini", "--", "/app/trento"]
ENTRYPOINT ["/app/trento"]

#FROM gcr.io/distroless/base:debug AS trento-web
#COPY --from=go-build /build/trento /app/trento
#LABEL org.opencontainers.image.source="https://github.com/trento-project/trento"
#EXPOSE 8080/tcp
#ENTRYPOINT ["/app/trento"]
