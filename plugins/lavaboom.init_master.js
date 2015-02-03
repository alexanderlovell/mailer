var r = require('rethinkdb');
var nats = require('nats');

exports.hook_init_master = function(next, server) {
	var that = this;

	// Retrieve environment variables
	var settings = {
		// RethinkDB settings
		rethinkdb_address: "127.0.0.1",
		rethinkdb_port: 28015,
		rethinkdb_key: "",
		rethinkdb_db: "dev",
		// NATS queue settings
		nats_address: "nats://127.0.0.1:4222",
		// Accepted hostnames list
		hostnames: {
			"lavaboom.io":  true,
			"lavaboom.com": true,
			"lavaboom.co":  true
		}
	};

	// Fetch environment variables
	// TODO: Use a 3rd party module
	if (process.env.RETHINKDB_PORT_28015_TCP_ADDR) {
		settings.rethinkdb_address = process.env.RETHINKDB_PORT_28015_TCP_ADDR;
		settings.rethinkdb_port = 28015;
	}

	if (process.env.RETHINKDB_PORT) {
		settings.rethinkdb_port = process.env.RETHINKDB_PORT;
	}

	if (process.env.RETHINKDB_KEY) {
		settings.rethinkdb_key = process.env.RETHINKDB_KEY;
	}

	if (process.env.RETHINKDB_DB) {
		settings.rethinkdb_DB = process.env.RETHINKDB_DB;
	}

	if (process.env.NATS_PORT_4222_TCP_ADDR) {
		settings.nats_address = "nats://" + process.env.NATS_PORT_4222_TCP_ADDR + ":4222";
	}

	if (process.env.HOSTNAMES) {
		var hostnames = process.env.HOSTNAMES.split(",");
		settings.hostnames = {};
		for (var host in hostnames) {
			settings.hostnames[hostnames[host]] = true;
		}
	}

	that.logdebug(settings);

	// Push settings into notes
	server.notes.settings = settings;

	that.logdebug("Resolved all settings");

	// Connect to rethinkdb
	r.connect({
		host:    settings.rethinkdb_address,
		port:    settings.rethinkdb_port,
		authKey: settings.rethinkdb_key,
		db:      settings.rethinkdb_db
	}, function(err, conn) {
		// We cannot connect to the database, something happened.
		if (err) {
			that.logdebug("Could not connect to the database");
			that.logdebug("    " + err.message);
			process.exit(1);
		}

		// Push the connection into notes
		server.notes.rethinkdb = conn;

		// Notify that we're connected
		that.logdebug("Connected to RethinkDB");

		// Connect to NATS
		var queue = nats.connect({
			url: settings.nats_address
		});

		// Something might've gone wrong. I'm not sure how'd that work.
		if (!queue) {
			that.logdebug("Connecting to NATS failed");
			process.exit(1);
		}

		// Push the queue into notes
		server.notes.nats = queue;

		// Notify the user
		that.logdebug("Connected to NATS");

		// Execute the next handler
		return next();
	});
};