const http = require('node:http');
const fs = require('node:fs');
const server = http.createServer((_request, response) => {
  response.end('<p role="status">Ready</p>');
});
server.listen(Number(process.env.NINELIVES_SMOKE_PORT), '127.0.0.1', () => {
  fs.writeFileSync(`${process.env.NINELIVES_SMOKE_MARKER}.server`, JSON.stringify({serverPID: process.pid}));
});
