exports.hook_rcpt = function(next, connection, params) {
	var plugin = this;
	var rcpt = params[0];
	if (!rcpt.host) {
		plugin.logcrit("No host name found");
	}
	var hostname = rcpt.host.toLowerCase();
	// return next(DENY, "Domain not in hosts list");
	return next();
}
