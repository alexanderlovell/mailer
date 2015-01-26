// TODO still needs work, seems like haraka is ignoring this file
// TODO use hash to drive O(n) search -> O(1)

exports.hook_rcpt = function(next, connection, params) {
	var plugin = this;

	var rcpt = params[0];
	if (!rcpt.host) {
		return next(DENY, "rcpt doesn't specify a host");
	}

	var hostname = rcpt.host.toLowerCase();

	var buf = '';
	for (var host in server.notes.hosts) {
		buf = buf + host + ' ';
	}

	// plugin.loginfo("Hosts:" + buf);
	if (host in server.notes.hosts) {
		return next();
	}
	return next(DENY, "We don't recieve email for " + host);
}
