FROM gcr.io/distroless/base
COPY /ecsfgrun /ecsfgrun
ENTRYPOINT ["/ecsfgrun"]
