#!/bin/env sh
## Script to install and setup Spamassassin on Ubuntu, inspired by:
## www.digitalocean.com/community/tutorials/how-to-install-and-setup-spamassassin-on-ubuntu-12-04
export DEBIAN_FRONTEND=noninteractive

apt-get update
apt-get install -y spamassassin spamc
groupadd spamd
useradd -g spamd -s /bin/false -d /var/log/spamassassin spamd
mkdir /var/log/spamassassin
chown spamd:spamd /var/log/spamassassin

cat <<EOF >> /etc/default/spamassassin
ENABLED=1
OPTIONS="--create-prefs --max-children 5 --username spamd -H ${SAHOME} -s ${SAHOME}spamd.log"
PIDFILE="/var/run/spamd.pid"
CRON=1
SAHOME="/var/log/spamassassin/"
EOF

cat <<EOF >> /etc/spamassassin/local.cf
rewrite_header Subject *****SPAM*****
required_score 7.0
use_bayes 1
bayes_auto_learn 1
EOF
