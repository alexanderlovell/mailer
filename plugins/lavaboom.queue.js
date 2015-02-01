var openpgp = require("openpgp");
var r = require("rethinkdb");
var randomstring = require("randomstring");

var prefixes = /([\[\(] *)?(RE?S?|FYI|RIF|I|FS|VB|RV|ENC|ODP|PD|YNT|ILT|SV|VS|VL|AW|WG|ΑΠ|ΣΧΕΤ|ΠΡΘ|תגובה|הועבר|主题|转发|FWD?) *([-:;)\]][ :;\])-]*|$)|\]+ *$/

exports.hook_queue = function(next, connection) {
	var that = this;

	// Parse the key (we are 100% sure it's valid)
	var key = openpgp.key.readArmored(connection.notes.key.key);

	that.logdebug(connection.transaction.body);

	// Encrypt the message
	openpgp.encryptMessage(key.keys, connection.transaction.body.bodytext)
		.then(function(message) {
			that.logdebug("Encrypted the email");

			r.table("labels").filter({
				"owner":   connection.notes.user.id,
				"name":    "Inbox",
				"builtin": true,
			}).run(connection.server.notes.rethinkdb).then(function(cursor) {
					// Convert into an array
					return cursor.toArray();
				}).then(function(inbox) {
					if (inbox.length != 1) {
						that.logerror("User has no inbox: " + connection.notes.user.name);
						return next(DENY, "Unable to queue the email");
					}

					that.logdebug("Fetched inbox: " + inbox[0].id);

					var subject = connection.transaction.body.header.get("subject").trim();
					var threadSubject = subject.replace(prefixes, "");

					var emailID = randomstring.generate(20);
					var from = connection.transaction.body.header.get("from").split(",");
					var to = connection.transaction.body.header.get("to").split(",");
					var cc = connection.transaction.body.header.get("cc").split(",");

					if (cc[0] === "") {
						cc = [];
					}

					var sendEmail = function(thread) {
						// message is an encrypted body armor at this point
						r.table("emails").insert({
							id:           emailID,
							date_created: r.now(),
							name:         subject,
							owner:        connection.notes.user.id,
							kind:         "received",
							from:         from,
							to:           to,
							cc:           cc,
							// attachments:  [],
							body: {
								encoding:         "json",
								pgp_fingerprints: [connection.notes.key.id],
								data:             message,
								schema:           "email",
								version_major:    1,
								version_minor:    0,
							},
							thread:       thread,
							status:       "received",
							is_read:      false,
						}).run(connection.server.notes.rethinkdb).then(function() {
							return next(OK);
						}).error(function(error) {
							that.logerror("Unable to create a new email: " + error);
							return next(DENY, "Unable to queue the email");
						});
					}

					var createThread = function() {
						var thread = {
							id:           randomstring.generate(20),
							date_created: r.now(),
							name:         threadSubject,
							owner:        connection.notes.user.id,
							emails:       [emailID],
							labels:       [inbox[0].id],
							members:      from.concat(to).concat(cc),
							is_read:      false,
						};
						r.table("threads").insert(thread).run(connection.server.notes.rethinkdb).then(function() {
							return sendEmail(thread.id);
						}).error(function(error) {
							that.logerror("Unable to create a new thread: " + error);
							return next(DENY, "Unable to queue the email");
						});
					}

					r.table("threads").getAll(threadSubject, {index: "name"}).run(connection.server.notes.rethinkdb).then(function(cursor) {
						return cursor.toArray();
					}).then(function(result) {
						if (result.length > 0) {
							return sendEmail(result[0].id);
						} else {
							return createThread();
						}
					}).error(function(error) {
						return createThread();
					});
				}).error(function(error) {
					that.logerror("Error occured while fetching user's inbox ID: " + error);
					return next(DENY, "Unable to queue the email");
				});
		}).catch(function(error) {
			that.logerror("Errored while queueing an email: " + error);
			return next(DENY, "Unable to queue the email");
		});
}