# R4 docker double-node smoke: build the three binaries in a golang image,
# then run node-a → hub → node-b federation entirely inside the container.
FROM golang:1.25-bookworm
WORKDIR /src
COPY . .
RUN go build -o /usr/local/bin/mnemon-harness ./harness/cmd/mnemon-harness \
 && go build -o /usr/local/bin/mnemon-hub ./harness/cmd/mnemon-hub
COPY harness/scripts/r4/docker/incontainer.sh /usr/local/bin/incontainer.sh
RUN chmod +x /usr/local/bin/incontainer.sh
CMD ["/usr/local/bin/incontainer.sh"]
