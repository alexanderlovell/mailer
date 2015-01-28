var r = require('rethinkdb');
var nats = require('nats').connect();
var crypto = require('crypto');

// uuid is a hacky way of generating crypto-strong random strings of length 20,
// containing only [a-zA-Z0-9] characters
function uuid() {
	var out = crypto.randomBytes(18).toString('base64').substring(0, 20);
	var rpl = crypto.randomBytes(1).toString('hex');
	return out.replace("+", rpl[0]).replace("/", rpl[1]);
}

exports.hook_init_master = function(next, server) {
	var plugin = this;
	var env = process.env.ENV || "dev";

	/**************************************************************************/
	/* Setting up the database */
	// TODO create indexes if they don't exist
	var config = {
		host: process.env.RDB_HOST || 'localhost',
		port: parseInt(process.env.RDB_PORT) || 28015,
		auth: process.env.RDB_AUTH || "",
		db: process.env.RDB_DB || 'dev',
		tables: ['accounts', 'attachments', 'emails'],
	};
	var conn = r.connect({
		host: config.host,
		port: config.port,
		db: config.db,
		auth: config.auth,
	}, function(err, conn) {
		if (err) {
			plugin.logcrit("Couldn't connect to RethinkDB.");
			process.exit(1);
		}
		r.dbCreate(config.db).run(conn, function(err, result) {
			if (err) {
				plugin.logdebug("[RethinkDB] Database '" +
					config.db +
					"' already exists.");
			} else {
				plugin.lognotice("[RethinkDB] Database '" +
					config.db + "' created.");
			}
			config.tables.forEach(function(table) {
				r.db(config.db).tableCreate(table).run(
					conn,
					function(err, result) {
						if (err) {
							plugin.logdebug(
								"[RethinkDB] Table '" +
								table +
								"' already exists."
							);
						} else {
							plugin.lognotice(
								"[RethinkDB] Table '" +
								table +
								"' created."
							);
						}
					});
			});
		})
	});

	/**************************************************************************/
	/* Setup hosts names */
	// TODO sync hosts with rethinkdb/mx_hosts and put that in a cron job
	plugin.logdebug("[Setup] Setting up host names dictionary");
	var hosts = {
		'lavaboom.com': true,
		'lavaboom.io': true,
		'lavaboom.net': true,
		'lavaboom.org': true,
	}
	if (env === "dev") {
		hosts['haraka.test'] = true;
		hosts['andreis-air.local'] = true;
	}

	/**************************************************************************/
	/* Setting up node-nats */
	plugin.logdebug("[Setup] Connecting to NATS");
	var msg = 'Mailer started on ' + require("os").hostname() + " at " + new Date();
	plugin.logdebug("Sending a message via nats:\n" + msg);
	nats.publish('status', msg);

	/**************************************************************************/
	/* Saving everything to server.notes (global storage) */
	server.notes.rdb = {
		config: config,
		conn: conn,
	};
	server.notes.hosts = hosts;
	server.notes.nats = nats;
	server.notes.env = env;
	server.notes.uuid = uuid;

	return next();
}
