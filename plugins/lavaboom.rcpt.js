var r = require('rethinkdb');

exports.hook_rcpt = function(next, connection, params) {
	var that = this;

	// Get rcpt from params and check if it contains the hostname
	var rcpt = params[0];
	if (!rcpt.host) {
		return next(DENY, "rcpt doesn't specify a host");
	}

	// Fetch the hostname
	var hostname = rcpt.host.toLowerCase();

	// Check if hostname is in the list
	if (!connection.server.notes.settings.hostnames[hostname]) {
		return next(DENY, "We don't receive emails for " + hostname);
	}

	// Check if we have user in database
	var username = rcpt.user.toLowerCase().replace(".", "");

	// [Try to] fetch account from database
	r.table("accounts")
		.getAll(username, {index: "name"})
		.run(connection.server.notes.rethinkdb)
		.then(function(cursor) {
			// Convert into an array
			return cursor.toArray();
		}).then(function(result) {
			if (result.length != 1) {
				that.logdebug("User not found: " + username);
				return next(DENY, "Unable to resolve this email");
			}

			// Put the account in the notes
			connection.notes.user = result[0];

			// Make Haraka parse the body
			connection.transaction.parse_body = true;

			// Does the user have a public key set?
			if (result[0].public_key) {
				return r.table("keys")
					.get(result[0].public_key)
					.run(connection.server.notes.rethinkdb);
			} else {
				return r.table("keys")
					.getAll(result[0].id, {index: "owner"})
					.run(connection.server.notes.rethinkdb);
			}
		}).then(function(cursor) {
			return cursor.toArray();
		}).then(function(result) {
			if (result.length == 0) {
				that.logdebug("No keys found for user " + username);
				return next(DENY, "User's account is not set up");
			}

			that.logdebug(result);

			connection.notes.key = result[0];

			return next(OK);
		}).error(function(error) {
			that.logdebug("Error occured while fetching a user: " + error);
			return next(DENY, "Unable to resolve this email");
		});
};