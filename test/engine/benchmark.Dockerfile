FROM scratch
COPY benchmark.test /benchmark.test
ENTRYPOINT ["/benchmark.test"]
