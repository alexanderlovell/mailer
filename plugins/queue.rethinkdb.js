var r = require("rethinkdb");

exports.hook_queue = function(next, connection) {
	var plugin = this;
	var rdb = server.notes.rdb;
	var transaction = connection.transaction;

	plugin.loginfo("Received email! Saving to db.");

	// rcpt.address_exists saves this to connection.notes
	var rcptUsers = ["john's ID"];

	var receivedDate = transaction.header.headers.data || (new Date());
	var from = "" + transaction.mail_from.user + "@" + transaction.mail_from.host;
	var to = [];
	var subjectLine = transaction.header.get("subject").replace("\n", "");
	var body = transaction.body.bodytext || "<empty>";

	plugin.loginfo(transaction.header.toString());

	for (var i in transaction.rcpt_to) {
		to.push(transaction.rcpt_to[i].format());
	}

	for (var i in rcptUsers) {
		r.table('emails').insert({
			id: server.notes.uuid(),
			date_created: receivedDate,
			date_modified: receivedDate,
			name: 'email',
			owner: rcptUsers[i],
			from: from,
			to: to,
			cc: [],
			bcc: [],
			attachments: [], // TODO add list of attachment IDs
			body: body,
			status: "received",
			subject: subjectLine,
			is_read: false,
		}).run(rdb.conn, function(err, result) {
			if (err) {
				plugin.logcrit("Couldn't write email to database.");
				return next(DENY, "Couldn't write to RethinkDB");
			}
			return next();
		});
	}
}
