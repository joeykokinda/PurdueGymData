# Built from a static binary (CGO_ENABLED=0 go build -o corec-tracker-linux .); distroless has CA certs and tzdata, nothing else.
FROM gcr.io/distroless/static-debian12:nonroot
COPY corec-tracker-linux /corec-tracker
ENTRYPOINT ["/corec-tracker"]
