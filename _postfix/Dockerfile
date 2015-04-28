FROM ubuntu:14.04
MAINTAINER Piotr Zduniak <piotr@zduniak.net>

RUN apt-get update && apt-get install -y postfix && \
	apt-get clean && rm -rf /var/lib/apt/lists/*

COPY main.cf /etc/postfix/main.cf
COPY master.cf /etc/postfix/master.cf

ADD startup.sh /opt/startup.sh
RUN chmod a+x /opt/startup.sh

ENTRYPOINT ["/opt/startup.sh"]
CMD ["-h"]