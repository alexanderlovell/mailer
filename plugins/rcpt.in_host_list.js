// TODO still needs work, seems like haraka is ignoring this file
// TODO use hash to drive O(n) search -> O(1)

exports.hook_rcpt = function(next, connection, params) {
    var plugin = this;
    var rcpt = params[0];
    if (!rcpt.host) {
        return next(DENY, "No hostname found");
    }

    var hostname = rcpt.host.toLowerCase();
    plugin.loginfo("HOSTNAMES ARE: " + server.notes.host_list);
    server.notes.host_list.forEach(function(host) {
        if (host === hostname) {
            return next();
        }
    });
    return next(DENY, "Domain not in hosts list");
}
