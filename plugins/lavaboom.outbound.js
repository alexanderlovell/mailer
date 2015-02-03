var outbound = require("./outbound");
var r = require("rethinkdb");

exports.hook_init_master = function(next, server) {
	var that = this;

	// NATS is already loaded at this point
	server.notes.nats.subscribe('send', {'queue':'send'}, function(msg) {
		r.table("emails").get(JSON.parse(msg)).run(server.notes.rethinkdb).then(function(email) {
			that.logdebug(email);
			
			var contents = [
				"From: " + email.from[0],
				"To: "   + email.to.join(", "),
				"MIME-Version: 1.0",
				"Content-Type: text/plain; charset=utf-8",
				"Subject: " + email.name,
				"",
				email.body.data,
			].join("\n");

			outbound.send_email(email.from[0], email.to[0], contents, function(code, message) {
				that.logdebug(code);
				that.logdebug(message);
			});
		}).error(function(error) {
			that.logdebug("Unable to fetch sent email from database: " + error);
		});
	});

	next();
}

exports.hook_queue_outbound = function(next) { this.logdebug("pls1"); next(CONT); }
exports.hook_send_email = function(next, hmail) { this.logdebug(hmail); this.logdebug("pls2"); next(OK); }