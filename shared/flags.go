package shared

type Flags struct {
	EtcdAddress  string
	EtcdCAFile   string
	EtcdCertFile string
	EtcdKeyFile  string
	EtcdPath     string

	BindAddress      string
	WelcomeMessage   string
	Hostname         string
	LogFormatterType string
	ForceColors      bool

	RethinkAddress  string
	RethinkKey      string
	RethinkDatabase string

	NSQDAddress    string
	LookupdAddress string

	SMTPAddress  string
	SpamdAddress string

	DKIMKey      string
	DKIMSelector string
}
