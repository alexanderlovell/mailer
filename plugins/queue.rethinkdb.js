var r = require("rethinkdb");

exports.hook_queue = function(next, connection) {
	var plugin = this;
	var rdb = server.notes.rdb;
	var dbName = rdb.config.db;
	plugin.loginfo("Received email! Saving to db.");

	var transaction = connection.transaction;
	var receivedDate = transaction.header.headers.data;
	var subjectLine = transaction.header.headers.subject;

	r.db(dbName).table('emails').insert({
		email: transaction.mail_from,
		body: transaction.data_lines,
		received: receivedDate || (new Date()),
		subject: subjectLine,
	}).run(rdb.conn, function(err, result) {
		if (err) {
			plugin.logwarn("Couldn't write email to database.");
			return next(DENY, "Couldn't connect to Haraka");
		}
		return next();
	});
}
