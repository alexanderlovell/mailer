let bluebird = require("bluebird");
let openpgp  = require("openpgp");
let mimelib  = require("mimelib");
let _        = require("lodash");
let crypto   = require("crypto");
let outbound = require("./outbound");
let randomString = require("./util.randomstring").randomString;

let rawSingleTemplate = _.template(`From: <%= from %>
To: <%= to.join(", ") %><% if (cc.length != 0) { %>
Cc: <%= cc.join(", ") %><% } %>
Subject: <%= subject %><% if (reply_to) { %>
Reply-To: <%= reply_to %><% } %>
MIME-Version: 1.0
Content-Type: <%= content_type %>
Content-Transfer-Encoding: quoted-printable

<%= body %>
`);
let rawMultiTemplate = _.template(`From: <%= from %>
To: <%= to.join(", ") %><% if (cc.length != 0) { %>
Cc: <%= cc.join(", ") %><% } %>
MIME-Version: 1.0
Content-Type: multipart/mixed; boundary="<%= boundary1 %>"
Subject: <%= subject %><% if (reply_to) { %>
Reply-To: <%= reply_to %><% } %>

--<%= boundary1 %>
Content-Type: <%= content_type %>
Content-Transfer-Encoding: quoted-printable

<%= body %>
<% _.forEach(files, function(file) { %>
--<%= boundary1 %>
Content-Type: <%= file.encoding %>
Content-Transfer-Encoding: base64
Content-Disposition: attachment; filename="<% file.name %>"

<%= file.body %>
<% } %>
--<%= boundary1 %>--
`);
let pgpTemplate = _.template(`From: <%= from %>
To: <%= to.join(", ") %><% if (cc.length != 0) { %>
Cc: <%= cc.join(", ") %><% } %>
MIME-Version: 1.0
Content-Type: <%= content_type %>
Subject: <%= subject %>

<%= body %>
`);
let singleManifestTemplate = _.template(`From: <%= from %>
To: <%= to.join(", ") %><% if (cc.length != 0) { %>
Cc: <%= cc.join(", ") %><% } %>
Subject: <%= subject %>
MIME-Version: 1.0
Content-Type: multipart/mixed; boundary="<%= boundary1 %>"

--<%= boundary1 %>
Content-Type: multipart/alternative; boundary="<%= boundary2 %>"

--<%= boundary2 %>
Content-Type: application/pgp-encrypted

<%= body %>
--<%= boundary2 %>
Content-Type: text/html; charset="UTF-8"

<!DOCTYPE html>
<html>
<body>
<p>This is an encrypted email, <a href="https://view.lavaboom.com/#<%= id %>">
open it here if you email client doesn't support PGP manifests
</a></p>
</body>
</html>
--<%= boundary2 %>
Content-Type: text/plain; charset="UTF-8"

This is an encrypted email, open it here if your email client
doesn't support PGP manifests:

https://view.lavaboom.com/#<%= id %>
--<%= boundary2 %>--
--<%= boundary1 %>
Content-Type: application/x-pgp-manifest+json
Content-Disposition: attachment; filename="manifest.pgp"

<%= manifest %>
--<%= boundary1 %>--
`);
let multiManifestTemplate = _.template(`From: <%= from %>
To: <%= to.join(", ") %><% if (cc.length != 0) { %>
Cc: <%= cc.join(", ") %><% } %>
Subject: <%= subject %><% if (reply_to) { %>
Reply-To: <%= reply_to %><% } %>
MIME-Version: 1.0
Content-Type: multipart/mixed; boundary="<%= boundary1 %>"

--<%= boundary1 %>
Content-Type: multipart/alternative; boundary="<%= boundary2 %>"

--<%= boundary2 %>
Content-Type: application/pgp-encrypted

<%= body %>
--<%= boundary2 %>
Content-Type: text/html; charset="UTF-8"

<!DOCTYPE html>
<html>
<body>
<p>This is an encrypted email, <a href="https://view.lavaboom.com/#<%= id %>">
open it here if you email client doesn't support PGP manifests
</a></p>
</body>
</html>
--<%= boundary2 %>
Content-Type: text/plain; charset="UTF-8"

This is an encrypted email, open it here if your email client
doesn't support PGP manifests:

https://view.lavaboom.com/#<%= id %>
--<%= boundary2 %>--<% _.forEach(files, function(file) { %>
--<%= boundary1 %>
Content-Type: application/octet-stream
Content-Disposition: attachment; filename="<% file.name %>"

<%= file.body %>
<% } %>
--<%= boundary1 %>
Content-Type: application/x-pgp-manifest+json
Content-Disposition: attachment; filename="manifest.pgp"

<%= manifest %>
--<%= boundary1 %>--
`);

exports.hook_init_master = function(next, server) {
	let self = this;

	// Get the RethinkDB pool
	let r = server.notes.rethinkdb;

	// NATS is already loaded at this point
	server.notes.nats.subscribe('send', {'queue':'send'}, function(msg) {
		bluebird.coroutine(function *() {
			try {
				// Parse the message
				let id = JSON.parse(msg);

				// Get the email from the database
				let email = yield r.table("emails").get(id).run();

				// Fetch the files
				let files = [];
				for (let id of email.files) {
					let file = yield r.table("files").get(id).run();
					files.push(file);
				}

				// Declare a contents variable
				let contents;

				// Determine the kind
				if (email.kind === "raw") {
					// Generate the email
					if (files.length == 0) {
						contents = rawSingleTemplate({
							"from":     email.from,
							"to":       email.to,
							"cc":       email.cc,
							"reply_to": email.reply_to,
							"subject":  mimelib.encodeMimeWord(email.subject),
							"body":     mimelib.encodeQuotedPrintable(email.body),
						});
					} else {
						contents = rawMultiTemplate({
							"from":         email.from,
							"to":           email.to,
							"cc":           email.cc,
							"reply_to":     email.reply_to,
							"subject":      mimelib.encodeMimeWord(email.subject),
							"boundary1":    randomString(16),
							"content_type": email.content_type,
							"body":         mimelib.encodeQuotedPrintable(email.body),
							"files":        files,
						});
					}

					// Fetch owner's account
					let accountResult = yield r.table("accounts").getAll(username, {index: "name"}).run();

					if (result.length != 1) {
						throw "No account";
					}

					// Get owner's key
					let key;
					if (accountResult[0].public_key) {
						key = yield r.table("keys").get(accountResult[0].public_key).run();
					} else {
						let keys = yield r.table("keys").getAll(accountResult[0].id, {index: "owner"}).run();
						if (keys.length == 0) {
							throw "No user keys";
						}
						key = keys[0];
					}
					let publicKey = openpgp.key.readArmored(key.key);

					// Prepare a new manifest
					let manifest = {
						"version": "1.0.0",
						"headers": {
							"from":    email.from,
							"to":      email.to.join(", "),
							"subject": email.subject,
						}
						"subject": email.subject,
						"parts":   []
					};

					if (email.cc.length > 0) {
						manifest.headers.cc = email.cc.join(", ");
					}

					// Encrypt the body and hash the body
					let body = yield openpgp.encryptMessage(publicKey.keys, email.body);
					let hash = crypto.createHash('sha256');
					hash.update(email.body);

					// Push the body into the manifest
					manifest.parts.push({
						"id":           "body",
						"hash":         hash.digest("hex"),
						"content-type": email.content_type,
					});

					// Encrypt the attachments
					for (let file of files) {
						// Encrypt the attachment
						let ciphertext = yield openpgp.encryptMessage(publicKey.keys, file.body);

						// Calculate the checksum
						let hash = crypto.createHash('sha256');
						hash.update(file.body);

						// Generate an ID
						let id = randomString(16);

						// Push the attachment into the manifest
						manifest.parts.push({
							"id":           id,
							"hash":         hash.digest("hex"),
							"filename":     file.name,
							"content-type": file.encoding,
						});

						// Replace the file
						yield r.table("files").get(file.id).replace({
							"id":            file.id,
							"date_created":  file.date_created,
							"date_modified": r.now(),
							"name":          id + ".pgp",
						});
					}

					// Encrypt the manifest
					let encryptedManifest = yield openpgp.encryptMessage(publicKeys.keys, JSON.stringify(manifest));

					yield r.table("emails").get(email.id).replace({
						"id":       email.id,
						"kind":     "manifest",
						"from":     email.from,
						"to":       email.to,
						"cc":       email.cc,
						"bcc":      email.bcc,
						"pgp_fingerprints": [key.id],
						"files":    email.files,
						"manifest": encryptedManifest,
						"body":     body,
						"subject":  "Encrypted message (" + email.id + ")",
						"thread":   email.thread,
						"status":   "processed",
					});
				} else if (email.kind === "pgpmime") {

				} else if (email.kind === "manifest") {

				}

				for (let addr of email.to) {
					outbound.send_email(email.from, addr, contents, function(code, message) {
						self.logdebug(code);
						self.logdebug(message);
					});
				}

				for (let addr of email.cc) {
					outbound.send_email(email.from, addr, contents, function(code, message) {
						self.logdebug(code);
						self.logdebug(message);
					});
				}

				// TODO: add bcc
			} catch (error) {
				self.logerror("Unable to send an email: " + error);
			}
		});
	});

	next();
}

exports.hook_queue_outbound = function(next) { this.logdebug("pls1"); next(CONT); }
exports.hook_send_email = function(next, hmail) { this.logdebug(hmail); this.logdebug("pls2"); next(OK); }