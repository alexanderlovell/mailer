var crypto = require("crypto");

var charset = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghiklmnopqrstuvwxyz";
exports.randomString = function(length) {
	var buffer = crypto.randomBytes(length);
	var result = "";
	var clength = charset.length - 1;

	for (var i = 0; i < length; i++) {
		result += charset[Math.floor(buffer.readUInt8(i)/255*clength+0.5)]
	}

	return result
}