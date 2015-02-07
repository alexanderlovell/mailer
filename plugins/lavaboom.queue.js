let bluebird = require("bluebird");
let openpgp = require("openpgp");
let r = require("rethinkdbdash");

let randomString = require("./util.randomstring").randomString;
let stripPrefixes = require("./util.stripprefixes").stripPrefixes;

exports.hook_queue = function(next, connection) {
	let self = this;

	self.logdebug("Initializing a coroutine");

	bluebird.coroutine(function *() {
		try {
			// Get the RethinkDB pool
			let r = connection.server.notes.rethinkdb;

			// Debug information
			self.logdebug(connection.transaction.body);

			// Parse the key (we are 100% sure it's valid)
			let key = openpgp.key.readArmored(connection.notes.key.key);

			// Get the Content-Type
			let contentType = connection.transaction.body.header.get("content-type");
			
			// Function scope lets
			let bodyType;
			let bodyText;
			let attachments = [];

			if (contentType.indexOf("multipart") === -1) {
				// Single-part email means that Haraka parsed it into bodytext
				bodyText = connection.transaction.body.bodytext;

				// Determine if it's HTML
				if (contentType.indexOf("text/html") !== -1) {
					bodyType = "html";
				} else {
					bodyType = "text";
				}
			} else {
				// Multipart - welcome to the infinite possiblities of SMTP
				let parts = contentType.split(";")[0].split("/")

				switch (parts[0]) {
					case "digest":
						// multipart/digest
						break;
					case "message":
						// multipart/message
						break;
					case "alternative":
						// multipart/alternative
						break;
					case "related":
						// multipart/related
						break;
					case "report":
						// multipart/report
						break;
					case "signed":
						// multipart-signed
						break;
					case "encrypted":
						// multipart/encrypted
						break;
					case "form-data":
						// multipart/form-data
						break;
					case "byterange":
						// multipart/byterange
						break;
					default:
						// multipart/mixed
						break;
				}
			}

			self.logdebug("Parsed the email");

			// Encrypt the message
			let encryptedBody = yield openpgp.encryptMessage(key.keys, bodyText);

			self.logdebug("Encrypted the body");

			// Encrypt the attachments
			let encryptedAttachments = [];
			for (let i = 0; i < attachments.length; i++) {
				let attachment = encryptedAttachments[i];
				
				let encryptedBody = yield openpgp.encryptMessage(key.keys, attachment.body);

				encryptedAttachments.push({
					body: encryptedBody,
					name: attachment.name,
					type: attachment.type
				});
			}

			// Fetch the inbox of the user receiving
			let inbox = yield r.table("labels").filter({
				"owner":   connection.notes.user.id,
				"name":    "Inbox",
				"builtin": true,
			}).run()

			if (inbox.length != 1) {
				self.logerror("User has no inbox: " + connection.notes.user.name);
				return next(DENY, "Unable to queue the email");
			}

			self.logdebug("Fetched inbox: " + inbox[0].id);

			// Get the subject
			let subject = connection.transaction.body.header.get("subject").trim();
			let threadSubject = stripPrefixes(subject);

			// Generate the email ID early enough
			let emailID = randomString(20);

			// Prepare from, to and cc
			let from = connection.transaction.body.header.get("from").split(",");
			for (let i = 0; i < from.length; i++) {
				from[i] = from[i].trim();
			}

			let to = connection.transaction.body.header.get("to").split(",");
			for (let i = 0; i < to.length; i++) {
				to[i] = to[i].trim();

				if (to[i] == "<>") {
					to[i] = connection.notes.to;
				}
			}

			let cc = connection.transaction.body.header.get("cc").split(",");
			for (let i = 0; i < cc.length; i++) {
				cc[i] = cc[i].trim();
			}

			if (cc[0] === "") {
				cc = [];
			}

			// Trim the headers
			let headers = connection.transaction.body.header.lines();
			for (let i = 0; i < headers.length; i++) {
				headers[i] = headers[i].trim();
			}

			// Prepare thread scope
			let thread;

			// Try to find the thread by name
			let threadList = yield r.table("threads").getAll(threadSubject, {index: "name"}).run();
			if (threadList.length === 0) {
				thread = {
					id:            randomString(20),
					date_created:  r.now(),
					date_modified: r.now(),
					name:          threadSubject,
					owner:         connection.notes.user.id,
					emails:        [emailID],
					labels:        [inbox[0].id],
					members:       from.concat(to).concat(cc),
					is_read:       false,
				};
				yield r.table("threads").insert(thread).run();
			} else {
				thread = threadList[0];
				yield r.table("threads").get(thread.id).update({date_modified: r.now()}).run()
			}

			yield r.table("emails").insert({
				id:            emailID,
				date_created:  r.now(),
				date_modified: r.now(),
				name:          subject,
				owner:         connection.notes.user.id,
				kind:          "received",
				from:          from,
				to:            to,
				cc:            cc,
				headers:       headers,
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
			})

			return next(OK);
		} catch (error) {
			self.logerror("Unable to queue an email: " + error);
			return next(DENY, "Unable to queue the email");
		}
	})();
}