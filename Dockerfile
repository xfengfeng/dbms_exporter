ARG drivers="postgres freetds"
ARG ldflags="-extldflags=-static"

FROM ncabatoff/dbms_exporter_builder:1.1.5
WORKDIR /build
COPY . .
ENV GOFLAGS="-mod=vendor"
RUN make DRIVERS="$drivers" LDFLAGS="$ldflags"

FROM debian:stable-slim
RUN apt-get update
RUN apt-get -y install libodbc1 odbcinst libsybdb5 tdsodbc
COPY freetds.conf /usr/local/etc/
COPY odbcinst.ini .
RUN odbcinst -i -d -f ./odbcinst.ini

COPY --from=0 /build/dbms_exporter /
EXPOSE 9113

ENTRYPOINT [ "/dbms_exporter" ]
