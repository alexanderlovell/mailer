/* jshint esnext: true */

let bluebird = require("bluebird");

exports.hook_rcpt = function(next, connection, params) {
	// Declare self
	let self = this;

	bluebird.coroutine(function *() {
		try {
			// Get the RethinkDB pool
			let r = connection.server.notes.rethinkdb;

			// Get rcpt from params and check if it contains the hostname
			let rcpt = params[0];
			if (!rcpt.host) {
				return next(DENY, "rcpt doesn't specify a host");
			}

			// Put the rcpt email into notes
			connection.notes.to = rcpt.user + "@" + rcpt.host;

			// Fetch the hostname
			let hostname = rcpt.host.toLowerCase();

			// Check if hostname is in the list
			if (!connection.server.notes.settings.hostnames[hostname]) {
				return next(DENY, "We don't receive emails for " + hostname);
			}

			// Check if we have user in database
			let username = rcpt.user.toLowerCase().replace(".", "");

			// [Try to] fetch account from database
			let result = yield r.table("accounts").getAll(username, {index: "name"}).run();

			if (result.length !== 1) {
				self.logdebug("User not found: " + username);
				return next(DENY, "Unable to resolve this email");
			}

			// Put the account in the notes
			connection.notes.user = result[0];

			// Make Haraka parse the body
			connection.transaction.parse_body = true;

			// Does the user have a public key set?
			let keys;
			if (result[0].public_key) {
				keys = yield r.table("keys").get(result[0].public_key).run();
			} else {
				keys = yield r.table("keys").getAll(result[0].id, {index: "owner"}).run();
			}

			if (!keys || keys.length === 0) {
				self.logdebug("No keys found for user " + username);
				return next(DENY, "User's account is not set up");
			}

			connection.notes.key = keys[0];

			return next(OK);
		} catch (error) {
			self.logerror("Unable to queue an email: " + error);
			return next(DENY, "Unable to queue the email");
		}
	})();
};