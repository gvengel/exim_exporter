FROM scratch
ENTRYPOINT ["/exim_exporter"]
COPY exim_exporter /