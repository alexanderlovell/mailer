# mailer

<img src="https://mail.lavaboom.com/img/Lavaboom-logo.svg" align="right" width="200px" />

SMTP server that handles inbound emails and an outbound email generator.

Uses `lavab/smtpd`, a fork of `bitbucket.org/chrj/smtpd` for inbound email
handling and Postfix for routing of outbound emails.

## Requirements

 - RethinkDB
 - NSQ
 - Postfix
 - SpamAssassin

## How it works

<img src="http://i.imgur.com/w2HygbX.png">

## Usage

### Inside a Docker container

*This image will be soon uploaded to Docker Hub*

```bash
git clone git@github.com:lavab/mailer.git
cd mailer
docker build -t "lavab/mailer" .

docker run \
    -p 25:25 \
    -e "NSQD_ADDRESS=172.8.0.1:4150" \
    -e "LOOKUPD_ADDRESS=172.8.0.1:4161" \
    -e "SMTP_ADDRESS=172.8.0.1:2525" \
    -e "SPAMD_ADDRESS=172.8.0.1:783" \
    -e "RETHINKDB_ADDRESS=172.8.0.1:28015" \
    --name mailer \
    lavab/mailer
```

### Directly running the service

```bash
go get github.com/lavab/mailer

mailer \
    --nsqd_address=172.8.0.1:4150 \
    --lookupd_address=172.8.0.1:4161 \
    --smtpd_address=172.8.0.1:2525 \
    --spamd_address=172.8.0.1:783 \
    --rethinkdb_address=172.8.0.1:28015
```

## License

This project is licensed under the MIT license. Check `license` for more
information.
