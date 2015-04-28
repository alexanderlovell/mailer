FROM ubuntu:14.04

RUN apt-get update && apt-get install -y postfix && \
	apt-get clean && rm -rf /var/lib/apt/lists/*

COPY main.cf /etc/postfix/main.cf
COPY master.cf /etc/postfix/master.cf

CMD bash