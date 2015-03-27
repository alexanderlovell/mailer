FROM google/golang

RUN mkdir -p /gopath/src/github.com/lavab/mailer
ADD . /gopath/src/github.com/lavab/mailer
RUN godep go build

CMD []
ENTRYPOINT ["/gopath/bin/mailer"]
