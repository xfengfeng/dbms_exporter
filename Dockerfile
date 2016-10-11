# FROM scratch - doable if we're not using ODBC
# Using golang simply because we already use it for Dockerfile-buildexporter,
# so this is less downloading.
FROM golang:1.7.1-wheezy
RUN apt-get update && apt-get -y install libodbc1 odbcinst libsybdb5 tdsodbc
COPY freetds.conf /usr/local/etc/
COPY odbcinst.ini .
RUN odbcinst -i -d -f ./odbcinst.ini
COPY dbms_exporter /
EXPOSE 9113

ENTRYPOINT [ "/dbms_exporter" ]
