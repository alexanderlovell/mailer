var r = require('rethinkdb');
var nats = require('nats').connect();
var crypto = require('crypto');

function uuid() {
    return crypto.randomBytes(18).toString('base64');
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
        db: process.env.RDB_DB || 'test',
        tables: ['accounts', 'attachments', 'emails'],
    };
    var conn = r.connect({
        host: config.host,
        port: config.port,
    }, function(err, conn) {
        if (err) {
            plugin.logcrit(
                "Coulnd't connect to database. Exiting...");
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
        hosts['harka.text'] = true;
        hosts['andreis-air.local'] = true;
    }

    /**************************************************************************/
    /* Setting up node-nats */
    plugin.logdebug("[Setup] Connecting to NATS");
    var message = 'Mailer started on ' + require("os").hostname() + " at " +
        new Date();
    plugin.logdebug("Sending a message via nats:\n" + message);
    nats.publish('status', message);

    plugin.loginfo("Random bytes: [" + uuid() + "]");

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
