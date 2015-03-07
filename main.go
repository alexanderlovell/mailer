package main

import (
	"os"

	"github.com/lavab/flag"
	"github.com/lavab/mailer/handler"
	"github.com/lavab/mailer/outbound"
	"github.com/lavab/mailer/shared"
	"github.com/lavab/smtpd"
)

var (
	// Flags used to enable functionality in the flag package
	configFlag   = flag.String("config", "", "Config file to load")
	etcdAddress  = flag.String("etcd_address", "", "etcd peer addresses split by commas")
	etcdCAFile   = flag.String("etcd_ca_file", "", "etcd path to server cert's ca")
	etcdCertFile = flag.String("etcd_cert_file", "", "etcd path to client cert file")
	etcdKeyFile  = flag.String("etcd_key_file", "", "etcd path to client key file")
	etcdPath     = flag.String("etcd_path", "mailer/", "Path of the keys")

	// General settings
	bindAddress      = flag.String("bind", ":25", "Address used to bind")
	welcomeMessage   = flag.String("welcome", "Lavaboom Mailer ready.", "Welcome message displayed upon connecting to the server")
	hostname         = flag.String("hostname", "localhost", "Server hostname")
	logFormatterType = flag.String("log", "text", "Log formatter type. Either \"json\" or \"text\"")
	forceColors      = flag.Bool("force_colors", false, "Force colored prompt?")

	// RethinkDB connection settings
	rethinkdbAddress = flag.String("rethinkdb_address", func() string {
		address := os.Getenv("RETHINKDB_PORT_28015_TCP_ADDR")
		if address == "" {
			address = "127.0.0.1"
		}
		return address + ":28015"
	}(), "Address of the RethinkDB database")
	rethinkdbKey      = flag.String("rethinkdb_key", os.Getenv("RETHINKDB_AUTHKEY"), "Authentication key of the RethinkDB database")
	rethinkdbDatabase = flag.String("rethinkdb_db", func() string {
		database := os.Getenv("RETHINKDB_DB")
		if database == "" {
			database = "dev"
		}
		return database
	}(), "Database name on the RethinkDB server")

	// nsqd and nsqlookupd addresses
	nsqdAddress = flag.String("nsqd_address", func() string {
		address := os.Getenv("NSQD_PORT_4150_TCP_ADDR")
		if address == "" {
			address = "127.0.0.1"
		}
		return address + ":4150"
	}(), "Address of the nsqd server")
	lookupdAddress = flag.String("lookupd_address", func() string {
		address := os.Getenv("NSQLOOKUPD_PORT_4161_TCP_ADDR")
		if address == "" {
			address = "127.0.0.1"
		}
		return address + ":4161"
	}(), "Address of the lookupd server")

	// smtp relay and spamd addresses
	smtpAddress  = flag.String("smtp_address", "127.0.0.1:2525", "Address of the SMTP server used for message relaying")
	spamdAddress = flag.String("spamd_address", "127.0.0.1:783", "Address of the spamd server used for antispam")

	// dkim selector, domain and key
	dkimKey      = flag.String("dkim_key", "", "Path of the DKIM private file")
	dkimSelector = flag.String("dkim_selector", "default", "DKIM selector")
)

func main() {
	flag.Parse()

	config := &shared.Flags{
		EtcdAddress:      *etcdAddress,
		EtcdCAFile:       *etcdCAFile,
		EtcdCertFile:     *etcdCertFile,
		EtcdKeyFile:      *etcdKeyFile,
		EtcdPath:         *etcdPath,
		BindAddress:      *bindAddress,
		WelcomeMessage:   *welcomeMessage,
		Hostname:         *hostname,
		LogFormatterType: *logFormatterType,
		ForceColors:      *forceColors,
		RethinkAddress:   *rethinkdbAddress,
		RethinkKey:       *rethinkdbKey,
		RethinkDatabase:  *rethinkdbDatabase,
		NSQDAddress:      *nsqdAddress,
		LookupdAddress:   *lookupdAddress,
		SMTPAddress:      *smtpAddress,
		SpamdAddress:     *spamdAddress,
		DKIMKey:          *dkimKey,
		DKIMSelector:     *dkimSelector,
	}

	h := handler.PrepareHandler(config)

	server := &smtpd.Server{
		WelcomeMessage: *welcomeMessage,
		Handler:        h,
	}

	outbound.StartQueue(config)

	server.ListenAndServe(*bindAddress)
}
