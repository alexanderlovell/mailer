var r = require('rethinkdb');
var nats = require('nats').connect();

exports.hook_init_master = function(next, server) {
	var plugin = this;
	var env = process.env.ENV || "dev";
	/* Setting up the database */
	// TODO create indexes if they don't exist
	var config = {
		host: process.env.RDB_HOST || 'localhost',
		port: parseInt(process.env.RDB_PORT) || 28015,
		db: process.env.RDB_DB || 'test',
		tables: ['accounts', 'attachments', 'emails'],
	};
	var conn = r.connect({
		host: config.host,
		port: config.port,
	}, function(err, conn) {
		if (err) {
			plugin.logcrit("Coulnd't connect to database. Exiting...");
			process.exit(1);
		}
		r.dbCreate(config.db).run(conn, function(err, result) {
			if (err) {
				plugin.logdebug("[RethinkDB] Database '" + config.db +
					"' already exists.");
			} else {
				plugin.lognotice("[RethinkDB] Database '" + config.db + "' created.");
			}
			config.tables.forEach(function(table) {
				r.db(config.db).tableCreate(table).run(conn,
					function(err, result) {
						if (err) {
							plugin.logdebug("[RethinkDB] Table '" + table +
								"' already exists.");
						} else {
							plugin.lognotice("[RethinkDB] Table '" + table + "' created.");
						}
					});
			});
		})
	});

	/* Get user custom domains */
	// TODO get the custom domains from allaccounts
	// TODO change to map for faster access
	var host_list = ["lavaboom.com", "lavaboom.io", "lavaboom.net"];
	if (env === "dev") {
		host_list.push("haraka.test");
		host_list.push("andreis-air.local");
	}
	// TODO update this regularly, or use redis/something else

	/* Setting up node-nats */
	plugin.logdebug("Sending a message via nats");
	nats.publish('status', 'Mailer started on ' + require("os").hostname());

	/* Saving everything to global storage */
	server.notes.rdb = {
		config: config,
		conn: conn,
	};
	server.notes.host_list = host_list;
	server.notes.nats = nats;
	server.notes.env = env;

	return next();
}
