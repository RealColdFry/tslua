// Jest globalTeardown: stops the tslua socket server.
const fs = require("fs");

module.exports = async function () {
  if (globalThis.__tslua_server) {
    globalThis.__tslua_server.kill();
    globalThis.__tslua_server = undefined;
  }
  if (globalThis.__tslua_socket) {
    try {
      fs.unlinkSync(globalThis.__tslua_socket);
    } catch {}
  }
};
