// Saves the email contents to the Lavaboom database

var r = require('rethinkdb');

exports.hook_queue = function(next, connection) {
	var plugin = this;
	var rdb = server.notes.rdb;

	if (!connection.notes.to) {
		// Nothing to do
		return next();
	}
	connection.notes.to.forEach(function(address) {
		deliverTo(address, connection.transaction);
	});
}

function deliverTo(address, transaction) {
	transaction.loginfo("Sending email to " + address.format());
}
