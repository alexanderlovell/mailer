exports.hook_rcpt = function(next, connection, params) {
    var plugin = this;
    var rcpt = params[0];

    plugin.loginfo("TODO check users from rethinkdb");
    return next(OK);
}
