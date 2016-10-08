FROM debian:jessie

COPY dbms_exporter /dbms_exporter
COPY queries.yaml /queries.yaml

EXPOSE 9113

ENTRYPOINT [ "/dbms_exporter", "-queryfile", "/queries.yaml" ]
