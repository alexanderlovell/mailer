Lavaboom mailer
---------------

Lavaboom's SMTP gateway to the world wide web.

Currently only receives emails. Soon outbound email support, via postfix, will be added.

How it works
------------

`mailer` is a Dockerized app that has 3 components:

-	Haraka + our plugins -> receives emails
-	Postfix in send-only mode -> sends emails
-	Spamassassin daemon

### Receiving emails

1.	check domain is in the `mx_hosts` table
2.	check the email address
	1.	`@lavaboom.tld`? check account name
	2.	`@custom.domain`? check `accounts` table for email address
3.	parse email
	1.	create IDs for attachments
	2.	create document in `emails` table
	3.	upload attachments to `attachments`
4.	post NATS message to topic `received`

### Sending emails

1.	receive NATS message on `send`
2.	read email from database, format it
3.	send it to Postfix and post NATS message `delivered` when finished
